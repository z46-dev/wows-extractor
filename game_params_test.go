package wowsextractor_test

import (
	"archive/zip"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	wowsextractor "github.com/z46-dev/wows-extractor"
)

// TestLoadGameParams checks on-demand region queries from a portable archive.
func TestLoadGameParams(t *testing.T) {
	var (
		archivePath string = filepath.Join(t.TempDir(), "params.zip")
		file        *os.File
		writer      *zip.Writer
		entryWriter interface{ Write([]byte) (int, error) }
		fixture     []byte
		archive     *wowsextractor.Archive
		params      *wowsextractor.GameParams
		names       []string
		value       []byte
		err         error
	)

	if fixture, err = base64.StdEncoding.DecodeString("gR8QOQB6xcVJpUnFPIUFmRmpiG5C8SBRsC5C2s4akAyOQvy0vMzUgsqSzIA4IrW92QpTMnrANNkLa1kLGzRZCrkSSxJcA9y5i3MSixIDU3MT3ZOZCgM8PYOyAWCKZC2sZCxz8GBgYmCIZC2qYGuceA=="); err != nil {
		t.Fatal(err)
	}

	if file, err = os.Create(archivePath); err != nil {
		t.Fatal(err)
	}

	writer = zip.NewWriter(file)
	if entryWriter, err = writer.Create("content/GameParams.data"); err != nil {
		t.Fatal(err)
	}

	if _, err = entryWriter.Write(fixture); err != nil {
		t.Fatal(err)
	}

	if entryWriter, err = writer.Create(".wows-extractor/manifest.json"); err != nil {
		t.Fatal(err)
	}

	if _, err = entryWriter.Write([]byte(`{"formatVersion":2,"build":"42"}`)); err != nil {
		t.Fatal(err)
	}

	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	if archive, err = wowsextractor.Load(archivePath); err != nil {
		t.Fatal(err)
	}

	defer archive.Close()
	if params, err = archive.LoadGameParams("NA"); err != nil {
		t.Fatal(err)
	}

	if names = params.Query("SH"); len(names) != 1 || names[0] != "SHIP" {
		t.Fatalf("names = %v", names)
	}

	if value, err = params.Get("SHIP"); err != nil || string(value) != `{"id":42,"typeinfo":{"type":"Ship"}}` {
		t.Fatalf("value = %s, %v", value, err)
	}

	value[0] = 'x'
	if value, err = params.Get("SHIP"); err != nil || value[0] != '{' {
		t.Fatalf("stored value changed: %s, %v", value, err)
	}
}
