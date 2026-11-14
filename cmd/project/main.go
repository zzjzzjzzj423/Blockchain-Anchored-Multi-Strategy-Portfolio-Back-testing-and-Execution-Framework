package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/internal/doctor"
	"msbd5017/portfolio-proof/internal/execution"
	"msbd5017/portfolio-proof/internal/experiment"
)

const version = "0.4.0-m3"

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
	case "execution":
		return runExecution(args[1:])
	case "strategies":
		return runStrategies(ctx, args[1:])
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

func runStrategies(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(os.Stderr, "strategies command requires: validate")
		return 2
	}
	flags := flag.NewFlagSet("project strategies validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	output := flags.String("output", "reports/m3_strategy_validation.json", "validation report path")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid strategy options: %v\n", err)
		return 2
	}
	report, err := experiment.ValidateM3(ctx, *root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strategy validation failed: %v\n", err)
		return 1
	}
	reportPath := *output
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(*root, reportPath)
	}
	if err := experiment.WriteM3Report(reportPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "write strategy report: %v\n", err)
		return 1
	}
	fmt.Printf("strategy validation passed: days=%d strategies=%d parallel_identical=%t no_look_ahead=%t\n", report.Days, report.Strategies, report.ParallelByteIdentical, report.NoLookAhead)
	return 0
}

func runExecution(args []string) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(os.Stderr, "execution command requires: validate")
		return 2
	}
	flags := flag.NewFlagSet("project execution validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	output := flags.String("output", "reports/m2_execution_validation.json", "validation report path")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid execution options: %v\n", err)
		return 2
	}
	if _, err := execution.LoadFrozenConfig(".", "m2_validation"); err != nil {
		fmt.Fprintf(os.Stderr, "execution config failed: %v\n", err)
		return 1
	}
	report, validationErr := execution.ValidateM2()
	content, err := canonical.JSON(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode execution report: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create report directory: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*output, content, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write execution report: %v\n", err)
		return 1
	}
	if validationErr != nil {
		fmt.Fprintln(os.Stderr, validationErr)
		return 1
	}
	fmt.Printf("execution validation passed: days=%d trades=%d ledger_sha256=%s\n", report.Days, report.Trades, report.GoldenLedgerSHA256)
	return 0
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
  project execution validate the deterministic accounting core
  project strategies validate the M3 strategies and deterministic merge
  project version   print the CLI version
  project help      show this help
`)
}
