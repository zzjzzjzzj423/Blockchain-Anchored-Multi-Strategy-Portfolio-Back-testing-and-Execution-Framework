package evidence

import (
	"os"
	"path/filepath"
	"testing"

	"msbd5017/portfolio-proof/internal/merkle"
)

func TestBundleVerifierChecksAllProofsAndDetectsTamper(t *testing.T) {
	files := minimalLeafSet()
	tree, err := merkle.New(files)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := writeBundle(directory, files, tree); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyBundle(directory)
	if err != nil {
		t.Fatal(err)
	}
	if report.ProofsValid != len(files) || report.MerkleRoot != tree.RootHex() {
		t.Fatalf("unexpected verification report: %#v", report)
	}
	path := filepath.Join(directory, "results", "trades.csv")
	if err := os.WriteFile(path, []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBundle(directory); err == nil {
		t.Fatal("tampered bundle verified")
	}
}

func TestProofFileBindsBundleContent(t *testing.T) {
	files := minimalLeafSet()
	tree, _ := merkle.New(files)
	directory := t.TempDir()
	if err := writeBundle(directory, files, tree); err != nil {
		t.Fatal(err)
	}
	proofPath, err := FindProof(directory, "results/trades.csv")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProofFile(directory, proofPath); err != nil {
		t.Fatal(err)
	}
}

func minimalLeafSet() map[string][]byte {
	required := []string{
		"spec/SPEC.md", "data/LICENSE_DATA.md", "data/normalized/daily_ohlcv.csv",
		"config/experiment_protocol.json", "config/selected_parameters.json", "config/cost_model.json",
		"development/search_trace.jsonl", "manifests/data_manifest.json", "manifests/source_manifest.json",
		"manifests/reproducibility_profile.json", "results/base_strategy_signals.csv",
		"results/base_strategy_weights.csv", "results/base_strategy_returns.csv", "results/ensemble_weights.csv",
		"results/orders.csv", "results/trades.csv", "results/ledger.csv", "results/overall_metrics.json",
		"results/regime_metrics.json", "results/sensitivity.json", "logs/deterministic_events.jsonl",
	}
	files := make(map[string][]byte, len(required)+2)
	for _, name := range required {
		files[name] = []byte(name + "\n")
	}
	files["data/raw/sample.csv"] = []byte("raw\n")
	runID := computeRunID(files["manifests/data_manifest.json"], files["config/experiment_protocol.json"], files["config/selected_parameters.json"], files["manifests/source_manifest.json"])
	files["run_manifest.json"] = []byte("{\"run_id\":\"" + runID + "\"}\n")
	return files
}
