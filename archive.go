package wowsextractor

import (
	"archive/zip"
	"bytes"
	"compress/flate"
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

	"github.com/z46-dev/wows-extractor/internal/gameconstants"
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

type Progress struct {
	Completed int64
	Total     int64
}

type looseResource struct {
	relative, name string
	size           int64
}

type copyWriter struct {
	writer    io.Writer
	ctx       context.Context
	completed *int64
	total     int64
	report    func(Progress)
}

type Archive struct {
	file     *os.File
	reader   *zip.Reader
	entries  map[string]*zip.File
	paths    []string
	Manifest Manifest
}

var _ fs.FS = (*Archive)(nil)

// readReplayConstantModules reads selected Python bytecode modules from scripts.zip.
func readReplayConstantModules(scriptPath string) (modules map[string][]byte, err error) {
	var (
		archive *zip.ReadCloser
		file    *zip.File
		reader  io.ReadCloser
		data    []byte
	)

	modules = make(map[string][]byte)
	if archive, err = zip.OpenReader(scriptPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return
	}

	defer func() {
		err = errors.Join(err, archive.Close())
	}()

	for _, file = range archive.File {
		switch file.Name {
		case "scripts/m140717bd.pyc", "scripts/Version.pyc", "scripts/m865ba929.pyc", "scripts/m1966847e.pyc", "scripts/SharedDataConstants.pyc", "scripts/Avatar.pyc":
		default:
			continue
		}

		if reader, err = file.Open(); err != nil {
			return
		}

		if data, err = io.ReadAll(reader); err != nil {
			_ = reader.Close()
			return
		}

		if err = reader.Close(); err != nil {
			return
		}

		modules[file.Name] = data
	}

	return
}

// Extract copies the latest installed build into a portable ZIP archive.
// Prefixes limit extraction when provided; an empty list includes every indexed file.
func Extract(ctx context.Context, gameDir, output string, prefixes []string, clientVersion string) (archivePath string, err error) {
	archivePath, err = ExtractWithProgress(ctx, gameDir, output, prefixes, clientVersion, nil)
	return
}

// ExtractWithProgress reports copied input bytes while building the archive.
func ExtractWithProgress(ctx context.Context, gameDir, output string, prefixes []string, clientVersion string, report func(Progress)) (archivePath string, err error) {
	var (
		source            gameidx.Source
		loose             []looseResource
		constantsFiles    map[string][]byte = make(map[string][]byte)
		scriptConstants   map[string][]byte
		constantsData     []byte
		packageRoot       *os.Root
		file              *os.File
		writer            *zip.Writer
		entryWriter       io.Writer
		pkgFiles          map[string]*os.File = make(map[string]*os.File)
		pkgFile           *os.File
		fileHeader        *zip.FileHeader
		manifestData      []byte
		written, expected int64
		completed, total  int64
		tempPath          string
		committed         bool
		statErr, closeErr error
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

	for _, entry := range source.Entries {
		if !strings.HasPrefix(entry.Path, "gui/data/constants/") || !strings.HasSuffix(entry.Path, ".xml") {
			continue
		}

		if constantsFiles[entry.Path], err = readIndexedResource(packageRoot, entry); err != nil {
			err = fmt.Errorf("read %s: %w", entry.Path, err)
			return
		}
	}

	if len(constantsFiles) > 0 {
		if scriptConstants, err = readReplayConstantModules(filepath.Join(gameDir, "bin", source.Build, "res", "scripts.zip")); err != nil {
			return
		}

		if constantsData, err = gameconstants.GenerateWithScripts(constantsFiles, scriptConstants, source.Build); err != nil {
			return
		}
	}

	for _, entry := range source.Entries {
		if entry.Path == "constants.json" && included(entry.Path, prefixes) && len(constantsData) > 0 {
			err = fmt.Errorf("indexed resource conflicts with generated constants.json")
			return
		}
	}

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

	for _, entry := range source.Entries {
		if included(entry.Path, prefixes) {
			total += int64(entry.File.Size)
		}
	}

	if loose, err = listLooseResources(ctx, gameDir, source.Build, prefixes); err != nil {
		return
	}

	for _, resource := range loose {
		total += resource.size
	}

	total += int64(len(constantsData))
	if report != nil {
		report(Progress{Completed: 0, Total: total})
	}

	if file, err = os.CreateTemp(filepath.Dir(archivePath), ".wows-extractor-*.tmp"); err != nil {
		return
	}

	tempPath = file.Name()
	defer func() {
		if file != nil {
			err = errors.Join(err, file.Close())
		}

		if !committed {
			if closeErr = os.Remove(tempPath); closeErr != nil && !errors.Is(closeErr, os.ErrNotExist) {
				err = errors.Join(err, closeErr)
			}
		}
	}()

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
		if written, err = io.Copy(&copyWriter{writer: entryWriter, ctx: ctx, completed: &completed, total: total, report: report}, io.NewSectionReader(pkgFile, int64(entry.File.Offset), expected)); err != nil {
			return
		}

		if written != expected {
			err = fmt.Errorf("short PKG read for %s: %d of %d bytes", entry.Path, written, expected)
			return
		}
	}

	if err = writeLooseResources(ctx, writer, gameDir, source.Build, loose, &completed, total, report); err != nil {
		return
	}

	if len(constantsData) > 0 {
		if entryWriter, err = writer.Create("constants.json"); err != nil {
			return
		}

		if _, err = io.Copy(&copyWriter{writer: entryWriter, ctx: ctx, completed: &completed, total: total, report: report}, bytes.NewReader(constantsData)); err != nil {
			return
		}
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

	closeErr = file.Close()
	file = nil
	if closeErr != nil {
		err = closeErr
		return
	}

	if err = os.Rename(tempPath, archivePath); err != nil {
		return
	}
	committed = true
	if report != nil {
		report(Progress{Completed: total, Total: total})
	}

	return
}

// readIndexedResource decompresses one indexed file for derived-data generation.
func readIndexedResource(root *os.Root, entry gameidx.Entry) (data []byte, err error) {
	var (
		file         *os.File
		section      *io.SectionReader
		reader       io.Reader
		decompressor io.ReadCloser
		expected     int64
	)

	if entry.File.Offset > uint64(^uint64(0)>>1) || uint64(entry.File.Size) > uint64(^uint64(0)>>1)-entry.File.Offset {
		err = fmt.Errorf("invalid volume extent")
		return
	}

	if file, err = root.Open(entry.Volume); err != nil {
		return
	}

	defer func() {
		err = errors.Join(err, file.Close())
	}()

	section = io.NewSectionReader(file, int64(entry.File.Offset), int64(entry.File.Size))
	reader = section

	if entry.File.Compression != 0 {
		decompressor = flate.NewReader(section)
		defer func() {
			err = errors.Join(err, decompressor.Close())
		}()

		reader = decompressor
		expected = int64(entry.File.UnpackedSize)
	} else {
		expected = int64(entry.File.Size)
	}

	if data, err = io.ReadAll(io.LimitReader(reader, expected+1)); err != nil {
		return
	}

	if int64(len(data)) != expected {
		err = fmt.Errorf("unexpected uncompressed size: got %d, want %d", len(data), expected)
		return
	}

	return
}

// listLooseResources collects selected build files and their byte counts before copying.
func listLooseResources(ctx context.Context, gameDir, build string, prefixes []string) (resources []looseResource, err error) {
	var (
		rootPath string = filepath.Join(gameDir, "bin", build, "res")
		readRoot *os.Root
		statErr  error
	)

	if _, statErr = os.Stat(rootPath); errors.Is(statErr, os.ErrNotExist) {
		return
	} else if statErr != nil {
		err = statErr
		return
	}
	if readRoot, err = os.OpenRoot(rootPath); err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, readRoot.Close())
	}()
	err = filepath.WalkDir(rootPath, func(sourcePath string, entry fs.DirEntry, readErr error) (walkErr error) {
		var (
			relative, archiveName string
			info                  os.FileInfo
		)

		if readErr != nil {
			walkErr = readErr
			return
		}
		if walkErr = ctx.Err(); walkErr != nil {
			return
		}
		if entry.IsDir() {
			return
		}
		if entry.Type()&os.ModeSymlink != 0 {
			walkErr = fmt.Errorf("symlink in build resources: %s", sourcePath)
			return
		}
		if relative, walkErr = filepath.Rel(rootPath, sourcePath); walkErr != nil {
			return
		}
		archiveName = "bin/res/" + filepath.ToSlash(relative)
		if !included(archiveName, prefixes) {
			return
		}
		if info, walkErr = readRoot.Lstat(relative); walkErr != nil {
			return
		}
		if !info.Mode().IsRegular() {
			walkErr = fmt.Errorf("non-regular build resource: %s", sourcePath)
			return
		}
		resources = append(resources, looseResource{relative: relative, name: archiveName, size: info.Size()})

		return
	})
	return
}

// writeLooseResources adds selected unpackaged files under the selected build's res directory.
func writeLooseResources(ctx context.Context, writer *zip.Writer, gameDir, build string, resources []looseResource, completed *int64, total int64, report func(Progress)) (err error) {
	var (
		readRoot    *os.Root
		file        *os.File
		entryWriter io.Writer
		closeErr    error
	)

	if len(resources) == 0 {
		return
	}
	if readRoot, err = os.OpenRoot(filepath.Join(gameDir, "bin", build, "res")); err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, readRoot.Close())
	}()

	for _, resource := range resources {
		if err = ctx.Err(); err != nil {
			return
		}
		if file, err = readRoot.Open(resource.relative); err != nil {
			return
		}
		if entryWriter, err = writer.Create(resource.name); err != nil {
			err = errors.Join(err, file.Close())
			return
		}
		_, err = io.Copy(&copyWriter{writer: entryWriter, ctx: ctx, completed: completed, total: total, report: report}, file)
		closeErr = file.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return
		}
	}

	return
}

// Write forwards copied bytes and reports progress after each successful write.
func (counter *copyWriter) Write(data []byte) (written int, err error) {
	if err = counter.ctx.Err(); err != nil {
		return
	}
	if written, err = counter.writer.Write(data); written > 0 {
		*counter.completed += int64(written)
		if counter.report != nil {
			counter.report(Progress{Completed: *counter.completed, Total: counter.total})
		}
	}

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
