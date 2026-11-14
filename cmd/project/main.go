package main

import (
	"context"
	"fmt"
	"os"

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

func printUsage() {
	fmt.Print(`Usage:
  project doctor    check required and future toolchains
  project version   print the CLI version
  project help      show this help
`)
}
