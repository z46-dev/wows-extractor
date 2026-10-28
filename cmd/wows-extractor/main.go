package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alexflint/go-arg"
	wowsextractor "github.com/z46-dev/wows-extractor"
)

type extractCommand struct {
	GameDir       string   `arg:"positional,required" help:"World of Warships install directory"`
	Output        string   `arg:"-o,--output" help:"Archive path (default: wows-BUILD.zip in the current directory)"`
	Include       []string `arg:"--include,separate" help:"Virtual path or directory prefix to include; repeatable"`
	ClientVersion string   `arg:"--client-version" help:"Exact client version when known"`
}

type queryCommand struct {
	Archive string `arg:"positional,required" help:"Archive path"`
	Prefix  string `arg:"positional" help:"Virtual path or parameter ID prefix"`
	Region  string `arg:"--region" help:"Query GameParams identifiers in this region"`
	Output  string `arg:"-o,--output" help:"Write the exact matching file to this path"`
}

type arguments struct {
	Extract *extractCommand `arg:"subcommand:extract"`
	Query   *queryCommand   `arg:"subcommand:query"`
}

// main dispatches the extract and query commands.
func main() {
	var (
		args   arguments
		parser *arg.Parser
		err    error
	)

	if parser = arg.MustParse(&args); parser.Subcommand() == nil {
		parser.Fail("missing subcommand")
	}

	if err = run(args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run executes one CLI command.
func run(args arguments) (err error) {
	var (
		archive     *wowsextractor.Archive
		archivePath string
		entries     []wowsextractor.Entry
		params      *wowsextractor.GameParams
		names       []string
		value       json.RawMessage
		reader      io.ReadCloser
		output      *os.File
	)

	switch {
	case args.Extract != nil:
		if archivePath, err = wowsextractor.Extract(context.Background(), args.Extract.GameDir, args.Extract.Output, args.Extract.Include, args.Extract.ClientVersion); err != nil {
			return
		}

		fmt.Fprintln(os.Stdout, archivePath)
	case args.Query != nil:
		if archive, err = wowsextractor.Load(args.Query.Archive); err != nil {
			return
		}

		defer func() {
			err = errors.Join(err, archive.Close())
		}()
		if args.Query.Region != "" {
			if params, err = archive.LoadGameParams(args.Query.Region); err != nil {
				return
			}
			if args.Query.Output != "" {
				if value, err = params.Get(args.Query.Prefix); err != nil {
					return
				}
				err = os.WriteFile(args.Query.Output, append(value, '\n'), 0o600)
				return
			}
			names = params.Query(args.Query.Prefix)
			err = json.NewEncoder(os.Stdout).Encode(names)
			return
		}

		if args.Query.Output != "" {
			if reader, err = archive.Open(args.Query.Prefix); err != nil {
				return
			}

			defer func() {
				err = errors.Join(err, reader.Close())
			}()

			if output, err = os.Create(args.Query.Output); err != nil {
				return
			}

			if _, err = io.Copy(output, reader); err != nil {
				err = errors.Join(err, output.Close())
				return
			}

			err = output.Close()
			return
		}

		if entries, err = archive.Query(args.Query.Prefix); err != nil {
			return
		}

		err = json.NewEncoder(os.Stdout).Encode(entries)
	}

	return
}
