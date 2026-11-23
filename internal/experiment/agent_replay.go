package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
)

func agentArtifacts(ctx context.Context, evaluator *CandidateEvaluator, search SearchResult, config M3Config) (map[string][]byte, error) {
	base, ensemble, err := evaluator.RunSelected(ctx, search.Selected.Assessment.Candidate)
	if err != nil {
		return nil, err
	}
	output := map[string][]byte{}
	for prefix, result := range map[string]Result{"base_": base, "ensemble_": ensemble.Result} {
		artifacts, err := ArtifactBytes(result)
		if err != nil {
			return nil, err
		}
		for name, content := range artifacts {
			output[prefix+name] = content
		}
	}
	output["development_metrics.json"], err = developmentMetricsJSON(base, ensemble.Result, config)
	if err != nil {
		return nil, err
	}
	output["ensemble_strategy_weights.csv"], err = strategyAllocationCSV(ensemble.Allocations)
	if err != nil {
		return nil, err
	}
	parameters, err := candidateDocument(search.Selected.Assessment.Candidate)
	if err != nil {
		return nil, err
	}
	output["selected_parameters.json"], err = canonical.JSON(map[string]any{
		"schema_version": "agent-workflow-1", "candidate_id": search.Selected.Assessment.CandidateID,
		"parameters": parameters, "final_holdout_opened": false, "candidate_budget": len(search.Evaluations),
	})
	return output, err
}

// ReplayAgentSearch re-evaluates recorded proposals without calling a model.
// Equality includes every fold and the selected strategy's full execution ledger.
func ReplayAgentSearch(ctx context.Context, root, directory string) error {
	raw, err := os.ReadFile(filepath.Join(directory, "fold_metrics.json"))
	if err != nil {
		return err
	}
	var document struct {
		Candidates []struct {
			Parameters canonicalCandidate `json:"parameters"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	if len(document.Candidates) < 1 || len(document.Candidates) > MaximumCandidateBudget {
		return fmt.Errorf("invalid replay candidate count")
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return err
	}
	config, err := LoadM3Config(root)
	if err != nil {
		return err
	}
	if err := validateDevelopmentCoverage(bars, config); err != nil {
		return err
	}
	evaluator := NewCandidateEvaluator(bars)
	search := SearchResult{}
	for _, entry := range document.Candidates {
		c := entry.Parameters
		var entryZ, exitZ float64
		if _, err := fmt.Sscan(c.MeanReversion.EntryZ, &entryZ); err != nil {
			return err
		}
		if _, err := fmt.Sscan(c.MeanReversion.ExitZ, &exitZ); err != nil {
			return err
		}
		candidate := Candidate{Trend: c.Trend, MeanReversion: MeanReversionParameters{Lookback: c.MeanReversion.Lookback, EntryZ: entryZ, ExitZ: exitZ}, Allocator: AllocatorParameters{ReliabilityWindow: c.Allocator.ReliabilityWindow, VolatilityWindow: c.Allocator.VolatilityWindow}}
		evaluation, err := evaluator.Evaluate(ctx, candidate)
		if err != nil {
			return err
		}
		search.Evaluations = append(search.Evaluations, evaluation)
		if len(search.Evaluations) == 1 || BetterCandidate(evaluation.Assessment, search.Selected.Assessment) {
			search.Selected = evaluation
		}
	}
	folds, err := foldMetricsJSON(search.Evaluations)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, folds) {
		return fmt.Errorf("replay fold metrics differ")
	}
	artifacts, err := agentArtifacts(ctx, evaluator, search, config)
	if err != nil {
		return err
	}
	for name, want := range artifacts {
		actual, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, want) {
			return fmt.Errorf("replay differs: %s", name)
		}
	}
	return nil
}
