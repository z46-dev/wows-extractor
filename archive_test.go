package wowsextractor_test

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	wowsextractor "github.com/z46-dev/wows-extractor"
	"github.com/z46-dev/wows-extractor/internal/gameidx"
)

// makeIDX builds a small index containing res/hello.txt for either IDX layout.
func makeIDX(version uint32, compressedSize, size uint32, crc uint32) (data []byte) {
	var (
		resourceOffset, fileOffset, volumeOffset, stringOffset uint32
		resourceSize, volumeSize                               uint32
	)

	if version == 0x02000000 {
		resourceOffset, resourceSize, volumeSize = 56, 32, 24
	} else {
		resourceOffset, resourceSize, volumeSize = 40, 24, 16
	}

	fileOffset = resourceOffset + 2*resourceSize
	volumeOffset = fileOffset + 48
	stringOffset = volumeOffset + volumeSize
	data = make([]byte, stringOffset+uint32(len("res\x00hello.txt\x00test.pkg\x00")))
	binary.LittleEndian.PutUint32(data[:4], 0x50465349)
	binary.LittleEndian.PutUint32(data[4:8], version)
	copy(data[stringOffset:], "res\x00hello.txt\x00test.pkg\x00")

	if version == 0x02000000 {
		binary.LittleEndian.PutUint32(data[16:20], 2)
		binary.LittleEndian.PutUint32(data[20:24], 1)
		binary.LittleEndian.PutUint32(data[24:28], 1)
		binary.LittleEndian.PutUint64(data[32:40], uint64(resourceOffset-16))
		binary.LittleEndian.PutUint64(data[40:48], uint64(fileOffset-16))
		binary.LittleEndian.PutUint64(data[48:56], uint64(volumeOffset-16))
		binary.LittleEndian.PutUint64(data[resourceOffset+8:resourceOffset+16], uint64(stringOffset-resourceOffset))
		binary.LittleEndian.PutUint64(data[resourceOffset+16:resourceOffset+24], 1)
		binary.LittleEndian.PutUint64(data[resourceOffset+24:resourceOffset+32], gameidx.RootParentID)
		binary.LittleEndian.PutUint64(data[resourceOffset+resourceSize+8:resourceOffset+resourceSize+16], uint64(stringOffset+4-resourceOffset-resourceSize))
		binary.LittleEndian.PutUint64(data[resourceOffset+resourceSize+16:resourceOffset+resourceSize+24], 2)
		binary.LittleEndian.PutUint64(data[resourceOffset+resourceSize+24:resourceOffset+resourceSize+32], 1)
		binary.LittleEndian.PutUint64(data[fileOffset:fileOffset+8], 2)
		binary.LittleEndian.PutUint64(data[fileOffset+8:fileOffset+16], 7)
		binary.LittleEndian.PutUint64(data[fileOffset+24:fileOffset+32], 1)
		binary.LittleEndian.PutUint32(data[fileOffset+32:fileOffset+36], compressedSize)
		binary.LittleEndian.PutUint32(data[fileOffset+36:fileOffset+40], crc)
		binary.LittleEndian.PutUint32(data[fileOffset+40:fileOffset+44], size)
		binary.LittleEndian.PutUint64(data[volumeOffset+8:volumeOffset+16], uint64(stringOffset+14-volumeOffset))
		binary.LittleEndian.PutUint64(data[volumeOffset+16:volumeOffset+24], 7)
	} else {
		binary.LittleEndian.PutUint32(data[16:20], 2)
		binary.LittleEndian.PutUint32(data[20:24], resourceOffset-16)
		binary.LittleEndian.PutUint32(data[24:28], 1)
		binary.LittleEndian.PutUint32(data[28:32], fileOffset-16)
		binary.LittleEndian.PutUint32(data[32:36], 1)
		binary.LittleEndian.PutUint32(data[36:40], volumeOffset-16)
		binary.LittleEndian.PutUint64(data[resourceOffset:resourceOffset+8], 1)
		binary.LittleEndian.PutUint64(data[resourceOffset+8:resourceOffset+16], gameidx.RootParentID)
		binary.LittleEndian.PutUint32(data[resourceOffset+16:resourceOffset+20], 4)
		binary.LittleEndian.PutUint32(data[resourceOffset+20:resourceOffset+24], stringOffset-resourceOffset-16)
		binary.LittleEndian.PutUint64(data[resourceOffset+resourceSize:resourceOffset+resourceSize+8], 2)
		binary.LittleEndian.PutUint64(data[resourceOffset+resourceSize+8:resourceOffset+resourceSize+16], 1)
		binary.LittleEndian.PutUint32(data[resourceOffset+resourceSize+16:resourceOffset+resourceSize+20], 10)
		binary.LittleEndian.PutUint32(data[resourceOffset+resourceSize+20:resourceOffset+resourceSize+24], stringOffset+4-resourceOffset-resourceSize-16)
		binary.LittleEndian.PutUint32(data[fileOffset+12:fileOffset+16], compressedSize)
		binary.LittleEndian.PutUint32(data[fileOffset+16:fileOffset+20], crc)
		binary.LittleEndian.PutUint32(data[fileOffset+20:fileOffset+24], size)
		binary.LittleEndian.PutUint64(data[fileOffset+24:fileOffset+32], 1)
		binary.LittleEndian.PutUint64(data[fileOffset+32:fileOffset+40], 2)
		binary.LittleEndian.PutUint64(data[fileOffset+40:fileOffset+48], 7)
		binary.LittleEndian.PutUint64(data[volumeOffset:volumeOffset+8], 7)
		binary.LittleEndian.PutUint32(data[volumeOffset+8:volumeOffset+12], 9)
		binary.LittleEndian.PutUint32(data[volumeOffset+12:volumeOffset+16], stringOffset+14-volumeOffset-8)
	}

	return
}

// TestIDXFormats checks both index layouts and rejects truncated tables.
func TestIDXFormats(t *testing.T) {
	for _, version := range []uint32{0x02000000, 0x01010004} {
		var (
			index gameidx.Index
			err   error
		)

		if index, err = gameidx.Parse(makeIDX(version, 7, 5, 123)); err != nil {
			t.Fatal(err)
		}

		if len(index.Resources) != 2 || index.Resources[1].Name != "hello.txt" || len(index.Files) != 1 || index.Files[0].VolumeID != 7 || len(index.Volumes) != 1 || index.Volumes[0].Name != "test.pkg" {
			t.Fatalf("version %08x parsed incorrectly: %+v", version, index)
		}

		if _, err = gameidx.Parse(makeIDX(version, 7, 5, 123)[:50]); err == nil {
			t.Fatalf("version %08x accepted truncated index", version)
		}
	}
}

// TestArchiveRoundTrip checks raw deflate copying and independent concurrent readers.
func TestArchiveRoundTrip(t *testing.T) {
	var (
		gameDir, archivePath string = filepath.Join(t.TempDir(), "game"), filepath.Join(t.TempDir(), "archive.zip")
		payload              []byte = []byte("hello from a PKG")
		compressed           bytes.Buffer
		compressor           *flate.Writer
		archive              *wowsextractor.Archive
		entries              []wowsextractor.Entry
		reader               io.ReadCloser
		data                 []byte
		fsData               []byte
		dirEntries           []fs.DirEntry
		wait                 sync.WaitGroup
		errors               chan error = make(chan error, 16)
		err                  error
	)

	if err = os.MkdirAll(filepath.Join(gameDir, "bin", "42", "idx"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err = os.MkdirAll(filepath.Join(gameDir, "res_packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(gameDir, "bin", "42", "res", "texts", "en"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(gameDir, "bin", "42", "res", "texts", "en", "sample.txt"), []byte("loose resource"), 0o644); err != nil {
		t.Fatal(err)
	}

	if compressor, err = flate.NewWriter(&compressed, flate.DefaultCompression); err != nil {
		t.Fatal(err)
	}

	if _, err = compressor.Write(payload); err != nil {
		t.Fatal(err)
	}

	if err = compressor.Close(); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(filepath.Join(gameDir, "bin", "42", "idx", "main.idx"), makeIDX(0x02000000, uint32(compressed.Len()), uint32(len(payload)), crc32.ChecksumIEEE(payload)), 0o644); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(filepath.Join(gameDir, "res_packages", "test.pkg"), compressed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if archivePath, err = wowsextractor.Extract(context.Background(), gameDir, archivePath, nil, "1.0.0.42"); err != nil {
		t.Fatal(err)
	}

	if archive, err = wowsextractor.Load(archivePath); err != nil {
		t.Fatal(err)
	}

	defer archive.Close()
	if archive.Manifest.Build != "42" || archive.Manifest.ClientVersion != "1.0.0.42" {
		t.Fatalf("manifest = %+v", archive.Manifest)
	}

	if entries, err = archive.Query("res/"); err != nil || len(entries) != 1 || entries[0].Path != "res/hello.txt" {
		t.Fatalf("query = %+v, %v", entries, err)
	}

	if reader, err = archive.Open("res/hello.txt"); err != nil {
		t.Fatal(err)
	}

	if data, err = io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}

	reader.Close()
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload = %q", data)
	}
	if fsData, err = fs.ReadFile(archive, "res/hello.txt"); err != nil || !bytes.Equal(fsData, payload) {
		t.Fatalf("fs.ReadFile = %q, %v", fsData, err)
	}
	if dirEntries, err = fs.ReadDir(archive, "res"); err != nil || len(dirEntries) != 1 || dirEntries[0].Name() != "hello.txt" {
		t.Fatalf("fs.ReadDir = %+v, %v", dirEntries, err)
	}
	if fsData, err = fs.ReadFile(archive, "bin/res/texts/en/sample.txt"); err != nil || string(fsData) != "loose resource" {
		t.Fatalf("loose resource = %q, %v", fsData, err)
	}

	for range cap(errors) {
		wait.Go(func() {
			var (
				file     io.ReadCloser
				contents []byte
				readErr  error
			)

			if file, readErr = archive.Open("res/hello.txt"); readErr == nil {
				contents, readErr = io.ReadAll(file)
				file.Close()
			}

			if readErr == nil && !bytes.Equal(contents, payload) {
				readErr = fmt.Errorf("wrong concurrent payload")
			}

			errors <- readErr
		})
	}

	wait.Wait()
	close(errors)

	for readErr := range errors {
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
}
