package experiment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
)

const finalConfigRelativePath = "configs/final_experiment.json"

type FinalExperimentConfig struct {
	BaselineCommissionRate         string `json:"baseline_commission_rate"`
	BaselineSlippageRate           string `json:"baseline_slippage_rate"`
	CostsSHA256                    string `json:"costs_sha256"`
	DataSHA256                     string `json:"data_sha256"`
	DevelopmentVolatilityThreshold string `json:"development_volatility_threshold"`
	FinalEnd                       string `json:"final_end"`
	FinalStart                     string `json:"final_start"`
	PreFinalCommit                 string `json:"pre_final_commit"`
	PreFinalTag                    string `json:"pre_final_tag"`
	SchemaVersion                  string `json:"schema_version"`
	SelectedCandidateID            string `json:"selected_candidate_id"`
	SelectedParametersSHA256       string `json:"selected_parameters_sha256"`
	SpecSHA256                     string `json:"spec_sha256"`
	StrategiesSHA256               string `json:"strategies_sha256"`
}

func FreezeM5Config(root string) (FinalExperimentConfig, error) {
	clean, err := gitClean(root)
	if err != nil || !clean {
		return FinalExperimentConfig{}, fmt.Errorf("M5 freeze requires a clean Git tree")
	}
	preFinal, err := gitOutput(root, "rev-parse", "pre-final-v1^{}")
	if err != nil {
		return FinalExperimentConfig{}, fmt.Errorf("resolve pre-final-v1: %w", err)
	}
	if _, err := gitOutput(root, "merge-base", "--is-ancestor", preFinal, "HEAD"); err != nil {
		return FinalExperimentConfig{}, fmt.Errorf("pre-final-v1 is not an ancestor of HEAD")
	}
	candidate, selectedID, opened, err := loadSelectedCandidate(root)
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	if opened {
		return FinalExperimentConfig{}, fmt.Errorf("selected parameters already mark final holdout opened")
	}
	wantID, _ := candidate.ID()
	if wantID != selectedID {
		return FinalExperimentConfig{}, fmt.Errorf("selected candidate ID does not match parameters")
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	threshold, err := DevelopmentVolatilityThreshold(bars)
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	thresholdText, err := canonical.Decimal12(threshold)
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	hash := func(path string) (string, error) {
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if readErr != nil {
			return "", readErr
		}
		return bytesSHA256(content), nil
	}
	dataHash, err := hash("data/normalized/daily_ohlcv.csv")
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	selectedHash, err := hash("configs/selected_parameters.json")
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	costsHash, err := hash("configs/costs.json")
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	strategiesHash, err := hash("configs/strategies.json")
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	specHash, err := hash("SPEC.md")
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	config := FinalExperimentConfig{
		BaselineCommissionRate: "0.001000000000", BaselineSlippageRate: "0.000500000000",
		CostsSHA256: costsHash, DataSHA256: dataHash, DevelopmentVolatilityThreshold: thresholdText,
		FinalStart: projectdata.FinalHoldoutStart.Format("2006-01-02"), FinalEnd: projectdata.FinalHoldoutEnd.Format("2006-01-02"),
		PreFinalCommit: preFinal, PreFinalTag: "pre-final-v1", SchemaVersion: "1.0.0",
		SelectedCandidateID: selectedID, SelectedParametersSHA256: selectedHash,
		SpecSHA256: specHash, StrategiesSHA256: strategiesHash,
	}
	content, err := canonical.JSON(config)
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	if err := os.WriteFile(filepath.Join(root, finalConfigRelativePath), content, 0o644); err != nil {
		return FinalExperimentConfig{}, err
	}
	return config, nil
}

func LoadFinalExperimentConfig(root string) (FinalExperimentConfig, error) {
	content, err := os.ReadFile(filepath.Join(root, finalConfigRelativePath))
	if err != nil {
		return FinalExperimentConfig{}, err
	}
	var config FinalExperimentConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return FinalExperimentConfig{}, err
	}
	if config.SchemaVersion != "1.0.0" || config.FinalStart != "2025-01-01" || config.FinalEnd != "2025-12-31" || config.PreFinalTag != "pre-final-v1" {
		return FinalExperimentConfig{}, fmt.Errorf("invalid frozen final experiment config")
	}
	return config, nil
}

func verifyFinalConfig(root string, config FinalExperimentConfig) error {
	checks := map[string]string{
		"SPEC.md":                          config.SpecSHA256,
		"configs/costs.json":               config.CostsSHA256,
		"configs/selected_parameters.json": config.SelectedParametersSHA256,
		"configs/strategies.json":          config.StrategiesSHA256,
		"data/normalized/daily_ohlcv.csv":  config.DataSHA256,
	}
	for path, want := range checks {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if got := bytesSHA256(content); got != want {
			return fmt.Errorf("frozen hash mismatch for %s", path)
		}
	}
	return nil
}

func loadSelectedCandidate(root string) (Candidate, string, bool, error) {
	type document struct {
		CandidateBudget    int             `json:"candidate_budget"`
		CandidateID        string          `json:"candidate_id"`
		FinalHoldoutOpened bool            `json:"final_holdout_opened"`
		Parameters         json.RawMessage `json:"parameters"`
		SchemaVersion      string          `json:"schema_version"`
		SearchTraceSHA256  string          `json:"search_trace_sha256"`
		SelectionMetrics   json.RawMessage `json:"selection_metrics"`
		SelectionRule      string          `json:"selection_rule"`
	}
	content, err := os.ReadFile(filepath.Join(root, "configs", "selected_parameters.json"))
	if err != nil {
		return Candidate{}, "", false, err
	}
	var raw document
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Candidate{}, "", false, err
	}
	var parameters canonicalCandidate
	decoder = json.NewDecoder(bytes.NewReader(raw.Parameters))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parameters); err != nil {
		return Candidate{}, "", false, err
	}
	entry, err := strconv.ParseFloat(parameters.MeanReversion.EntryZ, 64)
	if err != nil {
		return Candidate{}, "", false, err
	}
	exit, err := strconv.ParseFloat(parameters.MeanReversion.ExitZ, 64)
	if err != nil {
		return Candidate{}, "", false, err
	}
	candidate := Candidate{
		Trend:         parameters.Trend,
		MeanReversion: MeanReversionParameters{Lookback: parameters.MeanReversion.Lookback, EntryZ: entry, ExitZ: exit},
		Allocator:     AllocatorParameters{ReliabilityWindow: parameters.Allocator.ReliabilityWindow, VolatilityWindow: parameters.Allocator.VolatilityWindow},
	}
	if raw.SchemaVersion != "1.0.0" || raw.CandidateBudget != MaximumCandidateBudget || raw.CandidateID == "" {
		return Candidate{}, "", false, fmt.Errorf("invalid selected parameter document")
	}
	if err := candidate.Validate(); err != nil {
		return Candidate{}, "", false, err
	}
	return candidate, raw.CandidateID, raw.FinalHoldoutOpened, nil
}

func gitClean(root string) (bool, error) {
	output, err := gitOutput(root, "status", "--porcelain")
	return output == "", err
}

func gitOutput(root string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
