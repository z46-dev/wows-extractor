package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/alexflint/go-arg"
	"github.com/z46-dev/golog"
	wowsextractor "github.com/z46-dev/wows-extractor"
)

type extractCommand struct {
	GameDir       string   `arg:"positional,required" help:"World of Warships install directory"`
	Output        string   `arg:"-o,--output" help:"Archive path (default: wows-BUILD.zip in the current directory)"`
	Include       []string `arg:"--include,separate" help:"Virtual path or directory prefix to include; repeatable"`
	All           bool     `arg:"--all" help:"Include every indexed game file"`
	ClientVersion string   `arg:"--client-version" help:"Exact client version when known"`
	Quiet         bool     `arg:"-q,--quiet" help:"Hide extraction progress"`
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
		loader      *golog.Loader
		ctx         context.Context
		stop        context.CancelFunc
		info        os.FileInfo
		statErr     error
		percent     int
		lastPercent int = -1
		report      func(wowsextractor.Progress)
		prefixes    []string
	)

	switch {
	case args.Extract != nil:
		if prefixes, err = extractPrefixes(*args.Extract); err != nil {
			return
		}
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if !args.Extract.Quiet {
			if info, statErr = os.Stdout.Stat(); statErr == nil && info.Mode()&os.ModeCharDevice != 0 {
				loader = golog.New().Loader("Indexing game files...", golog.LoaderASCII, 10).Start()
				defer loader.Stop()
				report = func(progress wowsextractor.Progress) {
					if progress.Total <= 0 {
						return
					}
					if progress.Completed == 0 {
						loader.SetMessage("Copying game files...")
					}
					percent = int(float64(progress.Completed) * 100 / float64(progress.Total))
					if percent != lastPercent {
						loader.SetProgress(float64(progress.Completed) / float64(progress.Total))
						if percent >= 100 {
							loader.SetMessage("Finalizing archive...")
						}
						lastPercent = percent
					}
				}
			}
		}
		if archivePath, err = wowsextractor.ExtractWithProgress(ctx, args.Extract.GameDir, args.Extract.Output, prefixes, args.Extract.ClientVersion, report); err != nil {
			return
		}
		if loader != nil {
			loader.SetProgress(1).Stop()
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

// extractPrefixes chooses the default data set or an explicit file selection.
func extractPrefixes(args extractCommand) (prefixes []string, err error) {
	if args.All && len(args.Include) > 0 {
		err = fmt.Errorf("--all and --include cannot be combined")
		return
	}

	switch {
	case args.All:
		prefixes = nil
	case len(args.Include) > 0:
		prefixes = args.Include
	default:
		prefixes = []string{"scripts/", "content/GameParams.data", "bin/res/texts/"}
	}

	return
}
