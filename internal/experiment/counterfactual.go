package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
)

// Counterfactual runs measured interventions on development data, never holdout.
func Counterfactual(ctx context.Context, root, bundle string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(bundle, "selected_parameters.json"))
	if err != nil {
		return nil, err
	}
	var record struct {
		Parameters canonicalCandidate `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	c := record.Parameters
	var entry, exit float64
	if _, err := fmt.Sscan(c.MeanReversion.EntryZ, &entry); err != nil {
		return nil, err
	}
	if _, err := fmt.Sscan(c.MeanReversion.ExitZ, &exit); err != nil {
		return nil, err
	}
	selected := Candidate{Trend: c.Trend, MeanReversion: MeanReversionParameters{Lookback: c.MeanReversion.Lookback, EntryZ: entry, ExitZ: exit}, Allocator: AllocatorParameters{ReliabilityWindow: c.Allocator.ReliabilityWindow, VolatilityWindow: c.Allocator.VolatilityWindow}}
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return nil, err
	}
	config, err := LoadM3Config(root)
	if err != nil {
		return nil, err
	}
	if err := validateDevelopmentCoverage(bars, config); err != nil {
		return nil, err
	}
	neighbor := selected
	windows := []int{10, 20, 30}
	for i, v := range windows {
		if v == selected.Trend.FastWindow {
			neighbor.Trend.FastWindow = windows[(i+1)%len(windows)]
			break
		}
	}
	scenarios := []struct {
		Name      string
		Candidate Candidate
		Fee, Slip float64
	}{
		{"baseline", selected, 0.001, 0.0005}, {"double_commission", selected, 0.002, 0.0005},
		{"double_slippage", selected, 0.001, 0.001}, {"zero_cost_diagnostic", selected, 0, 0},
		{"adjacent_trend_parameter", neighbor, 0.001, 0.0005},
	}
	results := []any{}
	baselineSharpe := 0.0
	fragile := false
	for index, scenario := range scenarios {
		_, run, err := runFinalCandidate(ctx, bars, scenario.Candidate, scenario.Fee, scenario.Slip)
		if err != nil {
			return nil, err
		}
		sharpes := []float64{}
		folds := []any{}
		worst := 0.0
		for _, fold := range WalkForwardFolds() {
			summary, err := MetricsFor(run.Result, EnsembleName, fold.ValidationStart, fold.ValidationEnd)
			if err != nil {
				return nil, err
			}
			if summary.Sharpe == nil {
				return nil, fmt.Errorf("undefined stress Sharpe")
			}
			sharpes = append(sharpes, *summary.Sharpe)
			if summary.MaximumDrawdown > worst {
				worst = summary.MaximumDrawdown
			}
			folds = append(folds, map[string]any{"fold": fold.ID, "metrics": metricsDocument(summary)})
		}
		sort.Float64s(sharpes)
		med := (sharpes[1] + sharpes[2]) / 2
		if index == 0 {
			baselineSharpe = med
		}
		drop := baselineSharpe - med
		if scenario.Name != "zero_cost_diagnostic" && (drop > 0.3 || med < 0) {
			fragile = true
		}
		parameters, err := candidateDocument(scenario.Candidate)
		if err != nil {
			return nil, err
		}
		results = append(results, map[string]any{"scenario": scenario.Name, "commission": scenario.Fee, "slippage": scenario.Slip, "parameters": parameters, "median_validation_sharpe": med, "sharpe_drop_from_baseline": drop, "worst_drawdown": worst, "folds": folds})
	}
	return canonical.JSON(map[string]any{"schema_version": "3.0.0", "development_only": true, "holdout_opened": false, "falsification_rule": "flag fragility when an intervention reduces median development Sharpe by more than 0.3, or turns it negative; zero-cost is diagnostic only", "fragility_detected": fragile, "scenarios": results})
}
