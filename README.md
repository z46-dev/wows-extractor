# wows-extractor

A Go implementation of wowsunpack, a CLI/API program to extract information from a World of Warships installation.

## Features

- Read the latest installed build's IDX/PKG files without changing the installation.
- Write a portable ZIP archive with indexed virtual paths. Already-compressed PKG data is copied directly into ZIP entries.
- Include every file under the selected build's loose `bin/<build>/res` directory at `bin/res/...` paths in the archive.
- Query path prefixes and read files concurrently through an `fs.FS` API.
- Decode any GameParams region on demand and query its identifiers as JSON.

## CLI

```sh
go run ./cmd/wows-extractor extract "/path/to/World of Warships"
go run ./cmd/wows-extractor query wows-BUILD.zip scripts/entity_defs/
go run ./cmd/wows-extractor query wows-BUILD.zip content/GameParams.data --output GameParams.data
go run ./cmd/wows-extractor query wows-BUILD.zip PASA --region NA
go run ./cmd/wows-extractor query wows-BUILD.zip PASA001 --region NA --output param.json
```

`extract` writes `wows-BUILD.zip` in the current directory unless `--output` is set. It refuses to replace an existing archive. Use repeated `--include` flags to copy only particular virtual paths or directory prefixes. Package resources use paths such as `scripts/entities.xml`; loose build resources use `bin/res/...`. `--client-version` records a known full version in the archive manifest.

You can also install the CLI with `go install github.com/z46-dev/wows-extractor/cmd/wows-extractor@latest` and run it as `wows-extractor`.

## Go API

```go
func inspect() (err error) {
    var (
        archive *wowsextractor.Archive
        matches []wowsextractor.Entry
        params *wowsextractor.GameParams
        value json.RawMessage
    )

    if archive, err = wowsextractor.Load("wows-13357625.zip"); err != nil {
        return
    }

    defer archive.Close()

    if matches, err = archive.Query("scripts/entity_defs/"); err != nil {
        return
    }

    fmt.Println(matches)

    if _, err = fs.ReadFile(archive, "scripts/entities.xml"); err != nil {
        return
    }

    if params, err = archive.LoadGameParams("NA"); err != nil {
        return
    }

    if value, err = params.Get("PASA001"); err != nil {
        return
    }

    fmt.Println(string(value))
    return
}
```

`Archive.Query` and `GameParams.Query` are case-sensitive prefix searches. `Archive` implements `fs.FS` and can be shared across concurrent readers. `LoadGameParams` is an explicit, memory-intensive decode; load a region once and share its immutable result across requests. Close the archive after all readers are finished. ZIP entries and the manifest use portable forward-slash paths.

Replay-specific catalogs and renderer file layout can be adapted in `wows-replay-parser` using these archived files. The current port covers IDX v20/v40, PKG reads, and GameParams conversion. `assets.bin` subfiles and Rust's model and texture exporters are not yet implemented.

