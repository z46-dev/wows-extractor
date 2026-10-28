package gameidx

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type Entry struct {
	Path   string
	Volume string
	File   File
}

type Source struct {
	Build   string
	PkgDir  string
	Entries []Entry
}

// Discover indexes the latest installed build without changing game files.
func Discover(gameDir string) (source Source, err error) {
	var (
		buildDirs, idxFiles    []os.DirEntry
		buildNumber, candidate int
		data                   []byte
		index                  Index
		resources              map[uint64]Resource = make(map[uint64]Resource)
		files                  map[uint64]File     = make(map[uint64]File)
		orderedIDs             []uint64
		volumes                map[uint64]string = make(map[uint64]string)
		paths                  map[uint64]string = make(map[uint64]string)
		visiting               map[uint64]bool   = make(map[uint64]bool)
		byPath                 map[string]Entry  = make(map[string]Entry)
		info                   os.FileInfo
		indexRoot              *os.Root
		statErr                error
	)

	if buildDirs, err = os.ReadDir(filepath.Join(gameDir, "bin")); err != nil {
		return
	}

	for _, dir := range buildDirs {
		if !dir.IsDir() {
			continue
		}

		if candidate, err = strconv.Atoi(dir.Name()); err != nil {
			err = nil
			continue
		}

		if candidate > buildNumber {
			buildNumber = candidate
		}
	}

	if buildNumber == 0 {
		err = fmt.Errorf("no numeric build directory under %s", filepath.Join(gameDir, "bin"))
		return
	}

	source.Build = strconv.Itoa(buildNumber)
	source.PkgDir = filepath.Join(gameDir, "res_packages")
	if idxFiles, err = os.ReadDir(filepath.Join(gameDir, "bin", source.Build, "idx")); err != nil {
		return
	}
	if indexRoot, err = os.OpenRoot(filepath.Join(gameDir, "bin", source.Build, "idx")); err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, indexRoot.Close())
	}()

	if info, statErr = os.Stat(source.PkgDir); statErr != nil || !info.IsDir() {
		err = fmt.Errorf("res_packages directory is missing: %s", source.PkgDir)
		return
	}

	for _, idxFile := range idxFiles {
		if idxFile.IsDir() || !strings.EqualFold(filepath.Ext(idxFile.Name()), ".idx") {
			continue
		}

		if data, err = indexRoot.ReadFile(idxFile.Name()); err != nil {
			return
		}

		if index, err = Parse(data); err != nil {
			err = fmt.Errorf("%s: %w", idxFile.Name(), err)
			return
		}

		for _, resource := range index.Resources {
			if path.IsAbs(resource.Name) || strings.Contains(resource.Name, "\\") || strings.ContainsRune(resource.Name, 0) {
				err = fmt.Errorf("invalid IDX resource name %q", resource.Name)
				return
			}

			for _, part := range strings.Split(resource.Name, "/") {
				if part == "." || part == ".." {
					err = fmt.Errorf("invalid IDX resource name %q", resource.Name)
					return
				}
			}

			resources[resource.ID] = resource
		}

		for _, file := range index.Files {
			files[file.ResourceID] = file
			orderedIDs = append(orderedIDs, file.ResourceID)
		}

		for _, volume := range index.Volumes {
			if filepath.Base(volume.Name) != volume.Name || strings.ContainsAny(volume.Name, "/\\") || volume.Name == "." || volume.Name == ".." {
				err = fmt.Errorf("invalid volume name %q", volume.Name)
				return
			}

			volumes[volume.ID] = volume.Name
		}
	}

	if len(resources) == 0 {
		err = fmt.Errorf("no IDX resources found for build %s", source.Build)
		return
	}

	for _, id := range orderedIDs {
		var (
			file             File = files[id]
			filePath, volume string
			found            bool
		)

		if filePath, err = resolve(id, resources, paths, visiting); err != nil {
			return
		}

		if volume, found = volumes[file.VolumeID]; !found {
			err = fmt.Errorf("missing volume %d for %s", file.VolumeID, filePath)
			return
		}

		filePath = strings.TrimPrefix(filePath, "/")
		if filePath == "." || filePath == "" || path.IsAbs(filePath) || strings.HasPrefix(filePath, "../") || strings.Contains(filePath, "\\") || path.Clean(filePath) != filePath {
			err = fmt.Errorf("invalid resource path %q", filePath)
			return
		}

		byPath[filePath] = Entry{Path: filePath, Volume: volume, File: file}
	}

	for _, entry := range byPath {
		source.Entries = append(source.Entries, entry)
	}

	slices.SortFunc(source.Entries, func(a, b Entry) (order int) {
		order = strings.Compare(a.Path, b.Path)
		return
	})

	return
}

// resolve reconstructs a resource path from its parent chain.
func resolve(id uint64, resources map[uint64]Resource, paths map[uint64]string, visiting map[uint64]bool) (value string, err error) {
	var (
		resource Resource
		parent   string
		found    bool
	)

	if value, found = paths[id]; found {
		return
	}

	if visiting[id] {
		err = fmt.Errorf("IDX parent cycle at resource %d", id)
		return
	}

	if resource, found = resources[id]; !found {
		err = fmt.Errorf("IDX resource %d is missing", id)
		return
	}

	visiting[id] = true
	defer delete(visiting, id)

	if resource.ParentID != RootParentID {
		if parent, err = resolve(resource.ParentID, resources, paths, visiting); err != nil {
			return
		}
	}

	value = path.Clean(parent + "/" + resource.Name)
	paths[id] = value
	return
}
