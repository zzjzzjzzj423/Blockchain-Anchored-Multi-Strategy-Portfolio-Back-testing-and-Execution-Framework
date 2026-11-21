package experiment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexProposerUsesDevelopmentFeedbackAndValidatesCandidate(t *testing.T) {
	baseline := BaselineCandidate()
	id, err := baseline.ID()
	if err != nil {
		t.Fatal(err)
	}
	history := []CandidateAssessment{{Candidate: baseline, CandidateID: id, MedianValidationSharpe: 0.3, WorstValidationDrawdown: 0.2, MedianAnnualizedTurnover: 3}}
	directory := t.TempDir()
	bridge := filepath.Join(directory, "bridge.py")
	input := filepath.Join(directory, "feedback.json")
	script := "import sys\nfrom pathlib import Path\nPath(" + pythonString(input) + ").write_text(sys.stdin.read())\nprint('{\"parameters\":{\"trend\":{\"fast_window\":10,\"slow_window\":100},\"mean_reversion\":{\"lookback\":20,\"entry_z\":-1,\"exit_z\":0},\"allocator\":{\"reliability_window\":63,\"volatility_window\":20}},\"rationale\":\"test adjacent trend window\"}')\n"
	if err := os.WriteFile(bridge, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &CodexProposer{Context: context.Background(), Bridge: bridge, Timeout: 10 * time.Second}
	proposal, err := p.Propose(history)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Proposer != "codex_app_server" || p.Calls != 1 || p.Fallbacks != 0 {
		t.Fatalf("unexpected proposal state: %+v, calls=%d fallback=%d", proposal, p.Calls, p.Fallbacks)
	}
	feedback, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(feedback), "median_validation_sharpe") || strings.Contains(string(feedback), "holdout") {
		t.Fatalf("feedback boundary violated: %s", feedback)
	}
}

func TestCodexProposerRejectsRepeatedCandidate(t *testing.T) {
	baseline := BaselineCandidate()
	id, _ := baseline.ID()
	history := []CandidateAssessment{{Candidate: baseline, CandidateID: id}}
	bridge := filepath.Join(t.TempDir(), "bridge.py")
	script := `print('{"parameters":{"trend":{"fast_window":20,"slow_window":100},"mean_reversion":{"lookback":20,"entry_z":-1,"exit_z":0},"allocator":{"reliability_window":63,"volatility_window":20}},"rationale":"repeat"}')` + "\n"
	if err := os.WriteFile(bridge, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &CodexProposer{Bridge: bridge, Timeout: 10 * time.Second}
	if _, err := p.Propose(history); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("wanted repeated-candidate rejection, got %v", err)
	}
	p.AllowManualFallback = true
	proposal, err := p.Propose(history)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Proposer != "manual_deterministic_fallback" || p.Fallbacks != 1 {
		t.Fatalf("fallback was not recorded: %+v", proposal)
	}
}

func pythonString(value string) string { return "'" + strings.ReplaceAll(value, "'", "\\'") + "'" }
