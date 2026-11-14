package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/internal/doctor"
)

const version = "0.1.0-m0"

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}

	switch args[0] {
	case "doctor":
		report := doctor.Check(ctx, doctor.DefaultTools())
		doctor.Print(os.Stdout, report)
		if !report.Ready() {
			return 1
		}
		return 0
	case "data":
		return runData(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0
	case "version", "-v", "--version":
		fmt.Printf("project %s\n", version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printUsage()
		return 2
	}
}

func runData(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "data command requires one of: build, download, normalize, validate")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("project data "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	workers := flags.Int("workers", 8, "parallel download workers")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid data options: %v\n", err)
		return 2
	}

	config, err := projectdata.LoadConfig(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "data config failed: %v\n", err)
		return 1
	}
	downloader := projectdata.NewDownloader(*workers)
	if action == "build" || action == "download" {
		stats, err := downloader.Download(ctx, *root, config)
		if err != nil {
			projectdata.WriteFailureReport(*root, "download", err)
			fmt.Fprintf(os.Stderr, "data download failed: %v\n", err)
			return 1
		}
		fmt.Printf("download verified: %d monthly archives (%d workers)\n", stats.Archives, stats.Workers)
		if action == "download" {
			return 0
		}
	}
	if action != "build" && action != "normalize" && action != "validate" {
		fmt.Fprintf(os.Stderr, "unknown data command %q\n", action)
		return 2
	}
	result, err := projectdata.Build(ctx, *root, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "data validation failed: %v\n", err)
		return 1
	}
	fmt.Printf("data validation passed: files=%d rows=%d sha256=%s\n", result.Files, result.NormalizedRows, result.NormalizedSHA256)
	return 0
}

func printUsage() {
	fmt.Print(`Usage:
  project doctor    check required and future toolchains
  project data      build, download, normalize, or validate the fixed snapshot
  project version   print the CLI version
  project help      show this help
`)
}
