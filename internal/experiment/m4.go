package experiment

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/strategies/buyhold"
	"msbd5017/portfolio-proof/strategies/meanreversion"
	"msbd5017/portfolio-proof/strategies/trend"
)

type M4Report struct {
	ArtifactSHA256      map[string]string `json:"artifact_sha256"`
	BaseRunCacheHits    int               `json:"base_run_cache_hits"`
	CandidateBudget     int               `json:"candidate_budget"`
	CandidatesEvaluated int               `json:"candidates_evaluated"`
	DevelopmentEnd      string            `json:"development_end"`
	DevelopmentStart    string            `json:"development_start"`
	EvaluationCacheHits int               `json:"evaluation_cache_hits"`
	FinalHoldoutOpened  bool              `json:"final_holdout_opened"`
	FoldCount           int               `json:"fold_count"`
	Proposer            string            `json:"proposer"`
	ProposalCount       int               `json:"proposal_count"`
	SchemaVersion       string            `json:"schema_version"`
	SelectedCandidateID string            `json:"selected_candidate_id"`
	SelectionRule       string            `json:"selection_rule"`
	Status              string            `json:"status"`
}

func ValidateM4(ctx context.Context, root string, budget int) (M4Report, error) {
	if budget != MaximumCandidateBudget {
		return M4Report{}, fmt.Errorf("M4 final gate requires exactly %d candidates", MaximumCandidateBudget)
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return M4Report{}, err
	}
	config, err := LoadM3Config(root)
	if err != nil {
		return M4Report{}, err
	}
	if err := validateDevelopmentCoverage(bars, config); err != nil {
		return M4Report{}, err
	}
	evaluator := NewCandidateEvaluator(bars)
	search, err := RunSearch(ctx, evaluator, ManualProposer{}, budget)
	if err != nil {
		return M4Report{}, err
	}
	if evaluator.EvaluationCount() != budget {
		return M4Report{}, fmt.Errorf("evaluated %d unique candidates, want %d", evaluator.EvaluationCount(), budget)
	}
	selected := search.Selected.Assessment.Candidate
	base, ensemble, err := evaluator.RunSelected(ctx, selected)
	if err != nil {
		return M4Report{}, err
	}

	traceBytes, err := SearchTraceJSONL(search.Trace)
	if err != nil {
		return M4Report{}, err
	}
	foldBytes, err := foldMetricsJSON(search.Evaluations)
	if err != nil {
		return M4Report{}, err
	}
	overallBytes, err := developmentMetricsJSON(base, ensemble.Result, config)
	if err != nil {
		return M4Report{}, err
	}
	allocationBytes, err := strategyAllocationCSV(ensemble.Allocations)
	if err != nil {
		return M4Report{}, err
	}
	selectedBytes, err := selectedParametersJSON(search.Selected, traceBytes, budget)
	if err != nil {
		return M4Report{}, err
	}

	artifacts := map[string][]byte{
		"search_trace.jsonl":            traceBytes,
		"fold_metrics.json":             foldBytes,
		"development_metrics.json":      overallBytes,
		"ensemble_strategy_weights.csv": allocationBytes,
	}
	baseArtifacts, err := ArtifactBytes(base)
	if err != nil {
		return M4Report{}, err
	}
	for name, content := range baseArtifacts {
		artifacts["selected_base_"+name] = content
	}
	ensembleArtifacts, err := ArtifactBytes(ensemble.Result)
	if err != nil {
		return M4Report{}, err
	}
	for name, content := range ensembleArtifacts {
		artifacts["selected_ensemble_"+name] = content
	}

	artifactDirectory := filepath.Join(root, "artifacts", "development", "walk_forward")
	if err := os.MkdirAll(artifactDirectory, 0o755); err != nil {
		return M4Report{}, err
	}
	hashes := make(map[string]string, len(artifacts)+1)
	paths := make([]string, 0, len(artifacts))
	for path := range artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(artifactDirectory, path), artifacts[path], 0o644); err != nil {
			return M4Report{}, err
		}
		hashes[path] = bytesSHA256(artifacts[path])
	}
	selectedPath := filepath.Join(root, "configs", "selected_parameters.json")
	if err := os.WriteFile(selectedPath, selectedBytes, 0o644); err != nil {
		return M4Report{}, err
	}
	hashes["configs/selected_parameters.json"] = bytesSHA256(selectedBytes)
	return M4Report{
		ArtifactSHA256: hashes, BaseRunCacheHits: evaluator.BaseRunCacheHits(), CandidateBudget: budget, CandidatesEvaluated: len(search.Evaluations),
		DevelopmentStart: config.Start.Format("2006-01-02"), DevelopmentEnd: config.End.Format("2006-01-02"),
		EvaluationCacheHits: evaluator.EvaluationCacheHits(), FinalHoldoutOpened: false, FoldCount: len(WalkForwardFolds()),
		Proposer: "manual_deterministic", ProposalCount: len(search.Trace), SchemaVersion: "1.0.0",
		SelectedCandidateID: search.Selected.Assessment.CandidateID,
		SelectionRule:       "median validation net Sharpe; worst max drawdown; median annualized turnover; canonical parameter JSON",
		Status:              "passed",
	}, nil
}

func WriteM4Report(path string, report M4Report) error {
	content, err := canonical.JSON(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func selectedParametersJSON(selected Evaluation, trace []byte, budget int) ([]byte, error) {
	parameters, err := candidateDocument(selected.Assessment.Candidate)
	if err != nil {
		return nil, err
	}
	medianSharpe, err := canonical.Decimal12(selected.Assessment.MedianValidationSharpe)
	if err != nil {
		return nil, err
	}
	worstDrawdown, _ := canonical.Decimal12(selected.Assessment.WorstValidationDrawdown)
	medianTurnover, _ := canonical.Decimal12(selected.Assessment.MedianAnnualizedTurnover)
	return canonical.JSON(map[string]any{
		"candidate_budget":     budget,
		"candidate_id":         selected.Assessment.CandidateID,
		"final_holdout_opened": false,
		"parameters":           parameters,
		"schema_version":       "1.0.0",
		"search_trace_sha256":  bytesSHA256(trace),
		"selection_metrics": map[string]any{
			"median_annualized_turnover":        medianTurnover,
			"median_validation_sharpe":          medianSharpe,
			"worst_validation_maximum_drawdown": worstDrawdown,
		},
		"selection_rule": "median validation net Sharpe; worst max drawdown; median annualized turnover; canonical parameter JSON",
	})
}

func foldMetricsJSON(evaluations []Evaluation) ([]byte, error) {
	documents := make([]any, 0, len(evaluations))
	for _, evaluation := range evaluations {
		parameters, err := candidateDocument(evaluation.Assessment.Candidate)
		if err != nil {
			return nil, err
		}
		validation, err := evaluationDocument(evaluation)
		if err != nil {
			return nil, err
		}
		documents = append(documents, map[string]any{
			"candidate_id": evaluation.Assessment.CandidateID,
			"parameters":   parameters,
			"validation":   validation,
		})
	}
	return canonical.JSON(map[string]any{"candidates": documents, "schema_version": "1.0.0"})
}

func developmentMetricsJSON(base Result, ensemble Result, config M3Config) ([]byte, error) {
	strategies := []struct {
		name   string
		result Result
	}{
		{name: buyhold.Name, result: base},
		{name: trend.Name, result: base},
		{name: meanreversion.Name, result: base},
		{name: EnsembleName, result: ensemble},
	}
	document := make(map[string]any, len(strategies))
	for _, item := range strategies {
		summary, err := MetricsFor(item.result, item.name, config.Start, config.End)
		if err != nil {
			return nil, err
		}
		document[item.name] = metricsDocument(summary)
	}
	return canonical.JSON(map[string]any{
		"development_end":   config.End.Format("2006-01-02"),
		"development_start": config.Start.Format("2006-01-02"),
		"metrics":           document,
		"schema_version":    "1.0.0",
	})
}

func strategyAllocationCSV(allocations []StrategyAllocation) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"date", "strategy", "reliability", "realized_volatility", "score", "strategy_weight"}); err != nil {
		return nil, err
	}
	for _, allocation := range allocations {
		values := []float64{allocation.Reliability, allocation.RealizedVolatility, allocation.Score, allocation.Weight}
		formatted := make([]string, len(values))
		for index, value := range values {
			text, err := canonical.Decimal12(value)
			if err != nil {
				return nil, err
			}
			formatted[index] = text
		}
		if err := writer.Write([]string{allocation.Date.Format("2006-01-02"), allocation.Strategy, formatted[0], formatted[1], formatted[2], formatted[3]}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
