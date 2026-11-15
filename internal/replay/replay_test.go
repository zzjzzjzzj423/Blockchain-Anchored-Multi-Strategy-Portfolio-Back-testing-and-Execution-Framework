package replay

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"msbd5017/portfolio-proof/internal/evidence"
	"msbd5017/portfolio-proof/internal/experiment"
	"msbd5017/portfolio-proof/internal/merkle"
)

func TestFirstDifferenceLocatesPathAndOffset(t *testing.T) {
	expected := map[string][]byte{"b.json": []byte("same"), "a.csv": []byte("abcde")}
	actual := map[string][]byte{"b.json": []byte("same"), "a.csv": []byte("abXde")}
	difference := FirstDifference(expected, actual)
	if difference == nil || difference.Path != "a.csv" || difference.ByteOffset != 2 || difference.ExpectedByteHex != "63" || difference.ActualByteHex != "58" {
		t.Fatalf("unexpected difference: %#v", difference)
	}
	if FirstDifference(expected, expected) != nil {
		t.Fatal("identical maps reported a difference")
	}
	delete(actual, "a.csv")
	if difference := FirstDifference(expected, actual); difference == nil || difference.Path != "a.csv" {
		t.Fatalf("missing file was not localized: %#v", difference)
	}
}

func TestResultFilesMergesInStableOrder(t *testing.T) {
	artifacts := make(map[string][]byte)
	for _, name := range []string{
		"base_signals.csv", "base_executed_weights.csv", "base_target_weights.csv", "base_returns.csv",
		"ensemble_signals.csv", "ensemble_executed_weights.csv", "ensemble_target_weights.csv", "ensemble_returns.csv",
		"ensemble_strategy_weights.csv", "regime_labels.csv",
	} {
		artifacts[name] = []byte("date,strategy\n")
	}
	for _, name := range []string{"overall_metrics.json", "regime_metrics.json", "sensitivity.json"} {
		artifacts[name] = []byte("{}\n")
	}
	artifacts["base_orders.csv"] = []byte("date,strategy\n2025-01-02,z\n")
	artifacts["ensemble_orders.csv"] = []byte("date,strategy\n2025-01-01,a\n")
	artifacts["base_trades.csv"] = artifacts["base_orders.csv"]
	artifacts["ensemble_trades.csv"] = artifacts["ensemble_orders.csv"]
	artifacts["base_ledger.csv"] = artifacts["base_orders.csv"]
	artifacts["ensemble_ledger.csv"] = artifacts["ensemble_orders.csv"]
	result, err := resultFiles(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if string(result["results/orders.csv"]) != "date,strategy\n2025-01-01,a\n2025-01-02,z\n" {
		t.Fatalf("unstable merge: %q", result["results/orders.csv"])
	}
}

func TestValidateSourceIdentity(t *testing.T) {
	document := `{"git_tree_clean":true,"git_commit":"abc","verifier_source_sha256":"def","tool_versions":{"go":{"value":"go version ` + runtime.Version() + ` darwin/arm64"}}}`
	source, toolchain := validateSourceIdentity([]byte(document))
	if !source || !toolchain {
		t.Fatal("valid source identity rejected")
	}
	if source, _ := validateSourceIdentity([]byte(`{}`)); source {
		t.Fatal("empty source identity accepted")
	}
}

func TestMeasureCoreCoverageGate(t *testing.T) {
	report, err := MeasureCoreCoverage(context.Background(), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.MinimumPercent != "85.000000000000" || len(report.Packages) != 9 {
		t.Fatalf("unexpected coverage report: %#v", report)
	}
}

func TestRealBundleReplay(t *testing.T) {
	bundle := os.Getenv("M8_BUNDLE")
	if bundle == "" {
		t.Skip("set M8_BUNDLE for the real-bundle integration gate")
	}
	expected, err := evidence.ReadLeafFiles(bundle)
	if err != nil {
		t.Fatal(err)
	}
	runID, sourceCommit, err := manifestIdentity(expected["run_manifest.json"])
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := prepareWorkspace(expected)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)
	artifacts, err := experiment.ReplayFinalArtifacts(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := replayLeafSet(expected, artifacts, runID, sourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	if difference := FirstDifference(expected, actual); difference != nil {
		t.Fatalf("replay mismatch: %#v", difference)
	}
	tree, err := merkle.New(actual)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := evidence.VerifyBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if tree.RootHex() != verified.MerkleRoot || !strings.HasPrefix(tree.RootHex(), "05136f84") {
		t.Fatalf("root mismatch: %s", tree.RootHex())
	}
}
