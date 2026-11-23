package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV2BundlePartialProofAndMutation(t *testing.T) {
	source := t.TempDir()
	files := []string{"agent_events.jsonl", "search_trace.jsonl", "fold_metrics.json", "selected_parameters.json", "ensemble_trades.csv", "ensemble_ledger.csv", "market_data/quality.json", "market_data/cross_reference.json"}
	for _, name := range files {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("record\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "v2_metadata.json"), []byte(`{"schema_version":"2.0.0","replay_verified":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	report, err := PackV2(source, bundle)
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "bundle")
	other, err := PackV2(source, second)
	if err != nil {
		t.Fatal(err)
	}
	if report.MerkleRoot != other.MerkleRoot {
		t.Fatal("v2 packing nondeterministic")
	}
	proof, err := FindProof(bundle, "ensemble_trades.csv")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bundle, "ensemble_ledger.csv")); err != nil {
		t.Fatal(err)
	}
	// A selected file can be verified while unrelated leaves are unavailable.
	if err := VerifyProofFile(bundle, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyV2(bundle); err == nil {
		t.Fatal("missing leaf accepted")
	}
	if err := os.WriteFile(filepath.Join(bundle, "ensemble_trades.csv"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProofFile(bundle, proof); err == nil {
		t.Fatal("mutated selected file accepted")
	}
	if _, err := PackV2(source, second); err == nil {
		t.Fatal("immutable bundle overwritten")
	}
}
