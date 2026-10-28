package wowsextractor

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/z46-dev/wows-extractor/internal/gameidx"
)

const manifestPath string = ".wows-extractor/manifest.json"

type Manifest struct {
	FormatVersion int    `json:"formatVersion"`
	Build         string `json:"build"`
	ClientVersion string `json:"clientVersion,omitempty"`
}

type Entry struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}

type Archive struct {
	file     *os.File
	reader   *zip.Reader
	entries  map[string]*zip.File
	paths    []string
	Manifest Manifest
}

var _ fs.FS = (*Archive)(nil)

// Extract copies the latest installed build into a portable ZIP archive.
// Prefixes limit extraction when provided; an empty list includes every indexed file.
func Extract(ctx context.Context, gameDir, output string, prefixes []string, clientVersion string) (archivePath string, err error) {
	var (
		source            gameidx.Source
		packageRoot       *os.Root
		file              *os.File
		writer            *zip.Writer
		entryWriter       io.Writer
		pkgFiles          map[string]*os.File = make(map[string]*os.File)
		pkgFile           *os.File
		fileHeader        *zip.FileHeader
		manifestData      []byte
		written, expected int64
		statErr           error
		closeErr          error
	)

	if source, err = gameidx.Discover(gameDir); err != nil {
		return
	}
	if packageRoot, err = os.OpenRoot(source.PkgDir); err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, packageRoot.Close())
	}()

	if output == "" {
		output = "wows-" + source.Build + ".zip"
	}

	if archivePath, err = filepath.Abs(output); err != nil {
		return
	}

	if _, statErr = os.Stat(archivePath); statErr == nil {
		err = fmt.Errorf("archive already exists: %s", archivePath)
		return
	} else if !errors.Is(statErr, os.ErrNotExist) {
		err = statErr
		return
	}

	for _, prefix := range prefixes {
		if prefix == "" || strings.Contains(prefix, "\\") || path.IsAbs(prefix) || strings.HasPrefix(prefix, "../") {
			err = fmt.Errorf("invalid include prefix %q", prefix)
			return
		}
	}

	if file, err = os.CreateTemp(filepath.Dir(archivePath), ".wows-extractor-*.tmp"); err != nil {
		return
	}

	defer os.Remove(file.Name())
	defer file.Close()
	defer func() {
		for _, pkg := range pkgFiles {
			if closeErr = pkg.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	writer = zip.NewWriter(file)
	for _, entry := range source.Entries {
		if err = ctx.Err(); err != nil {
			return
		}

		if !included(entry.Path, prefixes) {
			continue
		}

		if entry.Path == manifestPath || strings.HasPrefix(entry.Path, ".wows-extractor/") {
			err = fmt.Errorf("reserved archive path %q", entry.Path)
			return
		}

		if pkgFile = pkgFiles[entry.Volume]; pkgFile == nil {
			if pkgFile, err = packageRoot.Open(entry.Volume); err != nil {
				err = fmt.Errorf("open volume %s: %w", entry.Volume, err)
				return
			}

			pkgFiles[entry.Volume] = pkgFile
		}

		if entry.File.Offset > uint64(^uint64(0)>>1) || uint64(entry.File.Size) > uint64(^uint64(0)>>1)-entry.File.Offset {
			err = fmt.Errorf("invalid volume extent for %s", entry.Path)
			return
		}

		fileHeader = &zip.FileHeader{
			Name:               entry.Path,
			CRC32:              entry.File.CRC32,
			CompressedSize64:   uint64(entry.File.Size),
			UncompressedSize64: uint64(entry.File.UnpackedSize),
		}

		if entry.File.Compression == 0 {
			fileHeader.Method = zip.Store
			fileHeader.UncompressedSize64 = uint64(entry.File.Size)
		} else {
			fileHeader.Method = zip.Deflate
		}

		if entryWriter, err = writer.CreateRaw(fileHeader); err != nil {
			return
		}

		expected = int64(entry.File.Size)
		if written, err = io.Copy(entryWriter, io.NewSectionReader(pkgFile, int64(entry.File.Offset), expected)); err != nil {
			return
		}

		if written != expected {
			err = fmt.Errorf("short PKG read for %s: %d of %d bytes", entry.Path, written, expected)
			return
		}
	}

	if err = writeLooseResources(writer, gameDir, source.Build, prefixes); err != nil {
		return
	}

	if manifestData, err = json.Marshal(Manifest{FormatVersion: 2, Build: source.Build, ClientVersion: clientVersion}); err != nil {
		return
	}

	if entryWriter, err = writer.Create(manifestPath); err != nil {
		return
	}

	if _, err = entryWriter.Write(manifestData); err != nil {
		return
	}

	if err = writer.Close(); err != nil {
		return
	}

	if err = file.Sync(); err != nil {
		return
	}

	if err = file.Close(); err != nil {
		return
	}

	if err = os.Rename(file.Name(), archivePath); err != nil {
		return
	}

	return
}

// writeLooseResources includes every unpackaged file under the selected build's res directory.
func writeLooseResources(writer *zip.Writer, gameDir, build string, prefixes []string) (err error) {
	var (
		root     string = filepath.Join(gameDir, "bin", build, "res")
		readRoot *os.Root
		statErr  error
	)

	if _, statErr = os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
		return
	} else if statErr != nil {
		err = statErr
		return
	}
	if readRoot, err = os.OpenRoot(root); err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, readRoot.Close())
	}()
	err = filepath.WalkDir(root, func(sourcePath string, entry fs.DirEntry, readErr error) (walkErr error) {
		var (
			relative, archiveName string
			file                  *os.File
			entryWriter           io.Writer
			closeErr              error
		)

		if readErr != nil {
			walkErr = readErr
			return
		}
		if entry.IsDir() {
			return
		}
		if entry.Type()&os.ModeSymlink != 0 {
			walkErr = fmt.Errorf("symlink in build resources: %s", sourcePath)
			return
		}
		if relative, walkErr = filepath.Rel(root, sourcePath); walkErr != nil {
			return
		}
		archiveName = "bin/res/" + filepath.ToSlash(relative)
		if !included(archiveName, prefixes) {
			return
		}
		if file, walkErr = readRoot.Open(relative); walkErr != nil {
			return
		}
		if entryWriter, walkErr = writer.Create(archiveName); walkErr != nil {
			walkErr = errors.Join(walkErr, file.Close())
			return
		}
		_, walkErr = io.Copy(entryWriter, file)
		if closeErr = file.Close(); walkErr == nil {
			walkErr = closeErr
		}
		return
	})
	return
}

// included reports whether a virtual path matches one of the requested prefixes.
func included(name string, prefixes []string) (matched bool) {
	if len(prefixes) == 0 {
		matched = true
		return
	}

	for _, prefix := range prefixes {
		prefix = strings.TrimPrefix(prefix, "/")
		if name == prefix || strings.HasPrefix(name, strings.TrimSuffix(prefix, "/")+"/") {
			matched = true
			return
		}
	}

	return
}

// Load opens an immutable archive for concurrent read requests.
func Load(archivePath string) (archive *Archive, err error) {
	var (
		file     *os.File
		info     os.FileInfo
		reader   *zip.Reader
		manifest io.ReadCloser
		openRoot *os.Root
		data     []byte
	)

	if openRoot, err = os.OpenRoot(filepath.Dir(archivePath)); err != nil {
		return
	}
	if file, err = openRoot.Open(filepath.Base(archivePath)); err != nil {
		err = errors.Join(err, openRoot.Close())
		return
	}
	if err = openRoot.Close(); err != nil {
		err = errors.Join(err, file.Close())
		return
	}

	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
			archive = nil
		}
	}()

	if info, err = file.Stat(); err != nil {
		return
	}

	if reader, err = zip.NewReader(file, info.Size()); err != nil {
		return
	}

	archive = &Archive{file: file, reader: reader, entries: make(map[string]*zip.File, len(reader.File))}
	for _, entry := range reader.File {
		if entry.Name == manifestPath {
			if manifest, err = entry.Open(); err != nil {
				return
			}

			if data, err = io.ReadAll(manifest); err != nil {
				err = errors.Join(err, manifest.Close())
				return
			}

			if err = manifest.Close(); err != nil {
				return
			}

			continue
		}

		if entry.Name != path.Clean(entry.Name) || strings.HasPrefix(entry.Name, "../") || path.IsAbs(entry.Name) {
			err = fmt.Errorf("invalid archive path %q", entry.Name)
			return
		}

		if _, exists := archive.entries[entry.Name]; exists {
			err = fmt.Errorf("duplicate archive path %q", entry.Name)
			return
		}

		archive.entries[entry.Name] = entry
		archive.paths = append(archive.paths, entry.Name)
	}

	if len(data) == 0 {
		err = fmt.Errorf("archive manifest is missing")
		return
	}

	if err = json.Unmarshal(data, &archive.Manifest); err != nil {
		return
	}

	switch archive.Manifest.FormatVersion {
	case 1, 2:
	default:
		err = fmt.Errorf("unsupported archive format %d", archive.Manifest.FormatVersion)
		return
	}

	if archive.Manifest.Build == "" {
		err = fmt.Errorf("archive build is missing")
		return
	}

	sort.Strings(archive.paths)
	return
}

// ReadFile returns the uncompressed contents of one archived virtual path.
func (archive *Archive) ReadFile(name string) (data []byte, err error) {
	var reader fs.File

	if reader, err = archive.Open(name); err != nil {
		return
	}

	defer reader.Close()
	data, err = io.ReadAll(reader)
	return
}

// Query lists files whose virtual paths have the given prefix.
func (archive *Archive) Query(prefix string) (results []Entry, err error) {
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		results = make([]Entry, 0, len(archive.paths))
	} else {
		results = make([]Entry, 0)
	}

	for position := sort.SearchStrings(archive.paths, prefix); position < len(archive.paths); position++ {
		if !strings.HasPrefix(archive.paths[position], prefix) {
			break
		}

		var file *zip.File = archive.entries[archive.paths[position]]
		results = append(results, Entry{Path: file.Name, Size: file.UncompressedSize64})
	}

	return
}

// Open implements fs.FS with independent readers backed by the archive.
func (archive *Archive) Open(name string) (file fs.File, err error) {

	file, err = archive.reader.Open(name)
	return
}

// Close releases the archive file after all concurrent reads are finished.
func (archive *Archive) Close() (err error) {
	err = archive.file.Close()
	return
}
