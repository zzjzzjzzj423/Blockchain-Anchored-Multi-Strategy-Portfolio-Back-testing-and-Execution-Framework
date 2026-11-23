package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentWorkflowReplayDetectsLedgerMutation(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"configs", "data/normalized"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	config, err := os.ReadFile("../../configs/strategies.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs/strategies.json"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	var csv strings.Builder
	csv.WriteString("date,symbol,open,high,low,close,volume\n")
	start := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	for day := 0; day < 1461; day++ {
		for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} {
			price := 100 + float64(day%31) + float64(day)/20
			fmt.Fprintf(&csv, "%s,%s,%.2f,%.2f,%.2f,%.2f,1\n", start.AddDate(0, 0, day).Format("2006-01-02"), symbol, price, price, price, price)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "data/normalized/daily_ohlcv.csv"), []byte(csv.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Budget one fixes the baseline and must not invoke the deliberately missing bridge.
	report, err := RunAgentSearch(context.Background(), root, 1, &CodexProposer{Bridge: "does-not-exist"})
	if err != nil {
		t.Fatal(err)
	}
	if report.CodexCalls != 0 {
		t.Fatal("unexpected model call")
	}
	if err := ReplayAgentSearch(context.Background(), root, report.OutputDirectory); err != nil {
		t.Fatal(err)
	}
	stress, err := Counterfactual(context.Background(), root, report.OutputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Counterfactual(context.Background(), root, report.OutputDirectory)
	if err != nil || !bytes.Equal(stress, again) {
		t.Fatalf("counterfactual not reproducible: %v", err)
	}
	stressPath := filepath.Join(report.OutputDirectory, "counterfactual.json")
	if err := os.WriteFile(stressPath, stress, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplayAgentSearch(context.Background(), root, report.OutputDirectory); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(stress, &document); err != nil {
		t.Fatal(err)
	}
	document["holdout_opened"] = true
	bad, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stressPath, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplayAgentSearch(context.Background(), root, report.OutputDirectory); err == nil || !strings.Contains(err.Error(), "counterfactual") {
		t.Fatalf("wanted counterfactual mutation rejection: %v", err)
	}
	if err := os.WriteFile(stressPath, stress, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(report.OutputDirectory, "ensemble_ledger.csv")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)-2] ^= 1
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplayAgentSearch(context.Background(), root, report.OutputDirectory); err == nil || !strings.Contains(err.Error(), "ensemble_ledger.csv") {
		t.Fatalf("wanted ledger mutation detection, got %v", err)
	}
}
