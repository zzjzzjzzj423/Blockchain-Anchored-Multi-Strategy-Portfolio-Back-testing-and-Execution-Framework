package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/chain"
	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/internal/doctor"
	"msbd5017/portfolio-proof/internal/evidence"
	"msbd5017/portfolio-proof/internal/execution"
	"msbd5017/portfolio-proof/internal/experiment"
	projectreplay "msbd5017/portfolio-proof/internal/replay"
)

const version = "1.0.0-m9"

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
	case "experiment":
		return runExperiment(ctx, args[1:])
	case "evidence":
		return runEvidence(ctx, args[1:])
	case "chain":
		return runChain(ctx, args[1:])
	case "replay":
		return runReplay(ctx, args[1:])
	case "mutate-test":
		return runMutation(args[1:])
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

func runReplay(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("project replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	replays := flags.Int("replays", 10, "clean replay count")
	mutations := flags.Int("mutations", 100, "single-byte mutation count")
	seed := flags.Int64("seed", 5017, "mutation seed")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "invalid replay options: %v\n", err)
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "replay requires a bundle path")
		return 2
	}
	bundle := flags.Arg(0)
	if !filepath.IsAbs(bundle) {
		bundle = filepath.Join(*root, bundle)
	}
	result, err := projectreplay.RunM8(ctx, *root, bundle, *replays, *mutations, *seed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "M8 replay failed: %v\n", err)
		return 1
	}
	fmt.Printf("M8 passed: replay=%d/%d roots=%d/%d mutations=%d/%d localized=%d/%d\n",
		result.Replay.ArtifactIdenticalRuns, result.Replay.ReplayCount, result.Replay.RootIdenticalRuns, result.Replay.ReplayCount,
		result.Mutation.Detected, result.Mutation.Count, result.Mutation.Localized, result.Mutation.Count)
	return 0
}

func runMutation(args []string) int {
	flags := flag.NewFlagSet("project mutate-test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	count := flags.Int("count", 100, "single-byte mutation count")
	seed := flags.Int64("seed", 5017, "mutation seed")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "invalid mutation options: %v\n", err)
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "mutate-test requires a bundle path")
		return 2
	}
	bundle := flags.Arg(0)
	files, err := evidence.ReadLeafFiles(bundle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read bundle: %v\n", err)
		return 1
	}
	report, err := projectreplay.RunMutations(bundle, files, *count, *seed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mutation test failed: %v\n", err)
		return 1
	}
	fmt.Printf("mutations passed: detected=%d/%d localized=%d/%d seed=%d\n", report.Detected, report.Count, report.Localized, report.Count, report.Seed)
	return 0
}

func runChain(ctx context.Context, args []string) int {
	if len(args) == 0 || (args[0] != "deploy" && args[0] != "rehearse") {
		fmt.Fprintln(os.Stderr, "chain command requires one of: deploy, rehearse")
		return 2
	}
	flags := flag.NewFlagSet("project chain deploy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid chain options: %v\n", err)
		return 2
	}
	var report chain.M7Report
	var err error
	if action := args[0]; action == "deploy" {
		report, err = chain.RunM7(ctx, *root)
	} else {
		report, err = chain.RehearseM7(ctx, *root)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "M7 chain validation failed: %v\n", err)
		return 1
	}
	fmt.Printf("M7 chain validation passed: contract=%s root=%s proof_depth=%d tx=%s\n", report.ContractAddress, report.MerkleRoot, report.ProofDepth, report.SubmissionTransaction)
	return 0
}

func runEvidence(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "evidence command requires one of: build, verify, prove, verify-proof")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("project evidence "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	bundle := flags.String("bundle", "", "evidence bundle path")
	leafPath := flags.String("path", "", "bundle-relative leaf path")
	proofPath := flags.String("proof", "", "proof JSON path")
	rebuilds := flags.Int("rebuilds", 10, "deterministic root build count")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid evidence options: %v\n", err)
		return 2
	}
	resolve := func(value string) string {
		if value == "" || filepath.IsAbs(value) {
			return value
		}
		return filepath.Join(*root, value)
	}
	switch action {
	case "build":
		report, err := evidence.Build(ctx, *root, *rebuilds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "M6 evidence build failed: %v\n", err)
			return 1
		}
		fmt.Printf("M6 evidence passed: run_id=%s leaves=%d root=%s rebuilds=%d\n", report.RunID, report.LeafCount, report.MerkleRoot, report.DeterministicBuilds)
		return 0
	case "verify":
		if *bundle == "" {
			fmt.Fprintln(os.Stderr, "evidence verify requires --bundle")
			return 2
		}
		report, err := evidence.VerifyBundle(resolve(*bundle))
		if err != nil {
			fmt.Fprintf(os.Stderr, "bundle verification failed: %v\n", err)
			return 1
		}
		fmt.Printf("bundle verified: run_id=%s leaves=%d root=%s\n", report.RunID, report.LeafCount, report.MerkleRoot)
		return 0
	case "prove":
		if *bundle == "" || *leafPath == "" {
			fmt.Fprintln(os.Stderr, "evidence prove requires --bundle and --path")
			return 2
		}
		name, err := evidence.FindProof(resolve(*bundle), *leafPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "find proof: %v\n", err)
			return 1
		}
		fmt.Println(name)
		return 0
	case "verify-proof":
		if *bundle == "" || *proofPath == "" {
			fmt.Fprintln(os.Stderr, "evidence verify-proof requires --bundle and --proof")
			return 2
		}
		if err := evidence.VerifyProofFile(resolve(*bundle), resolve(*proofPath)); err != nil {
			fmt.Fprintf(os.Stderr, "proof verification failed: %v\n", err)
			return 1
		}
		fmt.Println("proof verified")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown evidence command %q\n", action)
		return 2
	}
}

func runExperiment(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "experiment command requires one of: select, freeze-final, final")
		return 2
	}
	if args[0] == "freeze-final" {
		flags := flag.NewFlagSet("project experiment freeze-final", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", ".", "repository root")
		if err := flags.Parse(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "invalid freeze-final options: %v\n", err)
			return 2
		}
		config, err := experiment.FreezeM5Config(*root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "M5 config freeze failed: %v\n", err)
			return 1
		}
		fmt.Printf("M5 config frozen: candidate=%s threshold=%s holdout_opened=false\n", config.SelectedCandidateID, config.DevelopmentVolatilityThreshold)
		return 0
	}
	if args[0] == "final" {
		flags := flag.NewFlagSet("project experiment final", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", ".", "repository root")
		if err := flags.Parse(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "invalid final options: %v\n", err)
			return 2
		}
		report, err := experiment.RunM5Formal(ctx, *root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "M5 formal run failed: %v\n", err)
			return 1
		}
		fmt.Printf("M5 formal run passed: days=365 scenarios=%d source=%s holdout_opened=%t\n", report.SensitivityScenarios, report.SourceCommit, report.FinalHoldoutOpened)
		return 0
	}
	if args[0] != "select" {
		fmt.Fprintf(os.Stderr, "unknown experiment command %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("project experiment select", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	budget := flags.Int("budget", experiment.MaximumCandidateBudget, "unique candidate budget")
	output := flags.String("output", "reports/m4_parameter_selection.json", "validation report path")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "invalid experiment options: %v\n", err)
		return 2
	}
	report, err := experiment.ValidateM4(ctx, *root, *budget)
	if err != nil {
		fmt.Fprintf(os.Stderr, "M4 validation failed: %v\n", err)
		return 1
	}
	reportPath := *output
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(*root, reportPath)
	}
	if err := experiment.WriteM4Report(reportPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "write M4 report: %v\n", err)
		return 1
	}
	fmt.Printf("M4 validation passed: candidates=%d folds=%d selected=%s holdout_opened=%t\n", report.CandidatesEvaluated, report.FoldCount, report.SelectedCandidateID, report.FinalHoldoutOpened)
	return 0
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
  project experiment select run M4 walk-forward selection and freeze parameters
  project experiment freeze-final freeze the M5 regime threshold without opening 2025
  project experiment final run the one-time formal 2025 holdout experiment
  project evidence build build the canonical bundle and ordered SHA-256 Merkle tree
  project evidence verify verify a complete bundle and every inclusion proof
  project evidence prove locate a leaf inclusion proof
  project evidence verify-proof verify one proof against bundle content and root
  project chain deploy deploy the registry and validate the frozen M6 proof on Anvil
  project chain rehearse repeat the deployment/proof demo without changing frozen evidence
  project replay <bundle> deterministically replay M8 and run the mutation/QA gates
  project mutate-test <bundle> run deterministic single-byte bundle mutations
  project version   print the CLI version
  project help      show this help
`)
}
