package experiment

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/metrics"
	"msbd5017/portfolio-proof/internal/strategy"
	"msbd5017/portfolio-proof/strategies/buyhold"
	"msbd5017/portfolio-proof/strategies/meanreversion"
	"msbd5017/portfolio-proof/strategies/trend"
)

var m5ReportPaths = []string{
	"reports/m5_final_holdout.json",
	"reports/m5_regime_metrics.json",
	"reports/m5_run_log.json",
	"reports/m5_sensitivity.json",
}

type M5Report struct {
	ArtifactSHA256       map[string]string `json:"artifact_sha256"`
	CandidateID          string            `json:"candidate_id"`
	FinalEnd             string            `json:"final_end"`
	FinalHoldoutOpened   bool              `json:"final_holdout_opened"`
	FinalStart           string            `json:"final_start"`
	FormalRunSequence    int               `json:"formal_run_sequence"`
	Metrics              map[string]any    `json:"metrics"`
	RegimeThreshold      string            `json:"regime_volatility_threshold"`
	SchemaVersion        string            `json:"schema_version"`
	SensitivityScenarios int               `json:"sensitivity_scenarios"`
	SourceCommit         string            `json:"source_commit"`
	Status               string            `json:"status"`
}

// RunM5Formal opens the frozen 2025 holdout exactly once and writes all M5
// reports only after every computation and acceptance check has succeeded.
func RunM5Formal(ctx context.Context, root string) (M5Report, error) {
	clean, err := gitClean(root)
	if err != nil || !clean {
		return M5Report{}, fmt.Errorf("formal M5 run requires a clean Git tree")
	}
	for _, relative := range m5ReportPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err == nil {
			return M5Report{}, fmt.Errorf("formal M5 report already exists: %s", relative)
		} else if !os.IsNotExist(err) {
			return M5Report{}, err
		}
	}
	config, err := LoadFinalExperimentConfig(root)
	if err != nil {
		return M5Report{}, err
	}
	if err := verifyFinalConfig(root, config); err != nil {
		return M5Report{}, err
	}
	threshold, err := strconv.ParseFloat(config.DevelopmentVolatilityThreshold, 64)
	if err != nil {
		return M5Report{}, err
	}
	commission, err := strconv.ParseFloat(config.BaselineCommissionRate, 64)
	if err != nil {
		return M5Report{}, err
	}
	slippage, err := strconv.ParseFloat(config.BaselineSlippageRate, 64)
	if err != nil {
		return M5Report{}, err
	}
	selected, selectedID, opened, err := loadSelectedCandidate(root)
	if err != nil || opened || selectedID != config.SelectedCandidateID {
		return M5Report{}, fmt.Errorf("selected parameters do not match unopened frozen final config")
	}
	sourceCommit, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return M5Report{}, err
	}

	// This is the only point at which the formal path opens 2025 rows.
	bars, err := projectdata.LoadFullSnapshot(root)
	if err != nil {
		return M5Report{}, err
	}
	if err := validateFullCoverage(bars); err != nil {
		return M5Report{}, err
	}
	base, ensemble, err := runFinalCandidate(ctx, bars, selected, commission, slippage)
	if err != nil {
		return M5Report{}, err
	}
	overall, summaries, err := overallMetricsDocument(base, ensemble.Result)
	if err != nil {
		return M5Report{}, err
	}
	if err := validateFinalSummaries(summaries); err != nil {
		return M5Report{}, err
	}
	labels, err := ClassifyRegimes(bars, projectdata.FinalHoldoutStart, projectdata.FinalHoldoutEnd, threshold)
	if err != nil {
		return M5Report{}, err
	}
	regimeDocument, err := regimeMetricsDocument(base, ensemble.Result, labels)
	if err != nil {
		return M5Report{}, err
	}
	sensitivityDocument, scenarioCount, err := runSensitivity(ctx, bars, selected, summaries)
	if err != nil {
		return M5Report{}, err
	}

	overallBytes, err := canonical.JSON(map[string]any{"final_end": config.FinalEnd, "final_start": config.FinalStart, "metrics": overall, "schema_version": "1.0.0"})
	if err != nil {
		return M5Report{}, err
	}
	regimeBytes, err := canonical.JSON(regimeDocument)
	if err != nil {
		return M5Report{}, err
	}
	sensitivityBytes, err := canonical.JSON(sensitivityDocument)
	if err != nil {
		return M5Report{}, err
	}
	runLogBytes, err := canonical.JSON(map[string]any{
		"command": "project experiment final", "final_holdout_opened": true,
		"formal_run_sequence": 1, "schema_version": "1.0.0", "source_commit": sourceCommit,
		"tree_clean_before_run": true,
	})
	if err != nil {
		return M5Report{}, err
	}

	artifacts, err := finalArtifactBytes(base, ensemble, labels)
	if err != nil {
		return M5Report{}, err
	}
	artifacts["overall_metrics.json"] = overallBytes
	artifacts["regime_metrics.json"] = regimeBytes
	artifacts["sensitivity.json"] = sensitivityBytes
	artifacts["final_run_log.json"] = runLogBytes
	hashes := make(map[string]string, len(artifacts))
	artifactDirectory := filepath.Join(root, "artifacts", "final_holdout")
	if err := os.MkdirAll(artifactDirectory, 0o755); err != nil {
		return M5Report{}, err
	}
	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(artifactDirectory, name), artifacts[name], 0o644); err != nil {
			return M5Report{}, err
		}
		hashes[name] = bytesSHA256(artifacts[name])
	}
	report := M5Report{
		ArtifactSHA256: hashes, CandidateID: selectedID, FinalStart: config.FinalStart, FinalEnd: config.FinalEnd,
		FinalHoldoutOpened: true, FormalRunSequence: 1, Metrics: overall,
		RegimeThreshold: config.DevelopmentVolatilityThreshold, SchemaVersion: "1.0.0",
		SensitivityScenarios: scenarioCount, SourceCommit: sourceCommit, Status: "passed",
	}
	reportBytes, err := canonical.JSON(report)
	if err != nil {
		return M5Report{}, err
	}
	tracked := map[string][]byte{
		m5ReportPaths[0]: reportBytes, m5ReportPaths[1]: regimeBytes,
		m5ReportPaths[2]: runLogBytes, m5ReportPaths[3]: sensitivityBytes,
	}
	for path, content := range tracked {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), content, 0o644); err != nil {
			return M5Report{}, err
		}
	}
	return report, nil
}

func runFinalCandidate(ctx context.Context, bars []domain.MarketBar, candidate Candidate, commission, slippage float64) (Result, EnsembleRun, error) {
	factories, err := factoriesFor(candidate)
	if err != nil {
		return Result{}, EnsembleRun{}, err
	}
	base, err := RunAllWithCosts(ctx, bars, factories, true, commission, slippage)
	if err != nil {
		return Result{}, EnsembleRun{}, err
	}
	ensemble, err := RunEnsembleWithCosts(ctx, bars, base, candidate.Allocator, commission, slippage)
	return base, ensemble, err
}

func factoriesFor(candidate Candidate) ([]strategy.Factory, error) {
	trendStrategy, err := trend.New(candidate.Trend.FastWindow, candidate.Trend.SlowWindow)
	if err != nil {
		return nil, err
	}
	meanStrategy, err := meanreversion.New(candidate.MeanReversion.Lookback, candidate.MeanReversion.EntryZ, candidate.MeanReversion.ExitZ)
	if err != nil {
		return nil, err
	}
	return []strategy.Factory{
		{Name: buyhold.Name, New: buyhold.New},
		{Name: trend.Name, New: func() strategy.Strategy { return trend.MustNew(trendStrategy.Fast, trendStrategy.Slow) }},
		{Name: meanreversion.Name, New: func() strategy.Strategy {
			return meanreversion.MustNew(meanStrategy.Lookback, meanStrategy.EntryZ, meanStrategy.ExitZ)
		}},
	}, nil
}

func overallMetricsDocument(base, ensemble Result) (map[string]any, map[string]metrics.Summary, error) {
	result := make(map[string]any)
	summaries := make(map[string]metrics.Summary)
	for _, item := range []struct {
		name   string
		result Result
	}{{buyhold.Name, base}, {trend.Name, base}, {meanreversion.Name, base}, {EnsembleName, ensemble}} {
		summary, err := MetricsFor(item.result, item.name, projectdata.FinalHoldoutStart, projectdata.FinalHoldoutEnd)
		if err != nil {
			return nil, nil, err
		}
		summaries[item.name] = summary
		result[item.name] = metricsDocument(summary)
	}
	return result, summaries, nil
}

func validateFinalSummaries(summaries map[string]metrics.Summary) error {
	for name, summary := range summaries {
		if summary.Days != 365 || summary.MaximumExposure > 1+1e-10 || summary.MaximumAccountingResidual >= 1e-10 {
			return fmt.Errorf("%s violates final metric gates", name)
		}
	}
	return nil
}

func regimeMetricsDocument(base, ensemble Result, labels []RegimeLabel) (map[string]any, error) {
	regimes := make(map[string]any)
	for _, name := range RegimeNames {
		dates := make(map[string]struct{})
		for _, label := range labels {
			if label.Name == name {
				dates[label.Date.Format("2006-01-02")] = struct{}{}
			}
		}
		if len(dates) == 0 {
			return nil, fmt.Errorf("regime %s has no final observations", name)
		}
		strategyMetrics := make(map[string]any)
		for _, item := range []struct {
			name   string
			result Result
		}{{buyhold.Name, base}, {trend.Name, base}, {meanreversion.Name, base}, {EnsembleName, ensemble}} {
			summary, err := MetricsForDates(item.result, item.name, dates)
			if err != nil {
				return nil, err
			}
			document := metricsDocument(summary)
			mean, err := meanNetReturn(item.result, item.name, dates)
			if err != nil {
				return nil, err
			}
			document["mean_daily_return"] = decimal(mean)
			strategyMetrics[item.name] = document
		}
		regimes[name] = map[string]any{
			"days": len(dates), "fraction": decimal(float64(len(dates)) / float64(len(labels))),
			"metrics": strategyMetrics,
		}
	}
	return map[string]any{"regimes": regimes, "schema_version": "1.0.0"}, nil
}

func runSensitivity(ctx context.Context, bars []domain.MarketBar, selected Candidate, baseline map[string]metrics.Summary) (map[string]any, int, error) {
	scenarios, err := SensitivityScenarios(selected)
	if err != nil {
		return nil, 0, err
	}
	documents := make([]any, 0, len(scenarios))
	for _, scenario := range scenarios {
		base, ensemble, err := runFinalCandidate(ctx, bars, scenario.Candidate, scenario.CommissionRate, scenario.SlippageRate)
		if err != nil {
			return nil, 0, fmt.Errorf("sensitivity %s: %w", scenario.Name, err)
		}
		metricsDoc, summaries, err := overallMetricsDocument(base, ensemble.Result)
		if err != nil {
			return nil, 0, err
		}
		deltas := make(map[string]any)
		for name, summary := range summaries {
			deltas[name] = sensitivityDelta(summary, baseline[name])
		}
		documents = append(documents, map[string]any{
			"changed_field": scenario.ChangedField, "commission_rate": decimal(scenario.CommissionRate),
			"deltas_from_baseline": deltas, "direction": scenario.Direction, "metrics": metricsDoc,
			"name": scenario.Name, "parameters": sensitivityCandidateDocument(scenario.Candidate),
			"slippage_rate": decimal(scenario.SlippageRate),
		})
	}
	return map[string]any{
		"method": "one_factor_at_a_time", "scenario_count": len(scenarios),
		"scenarios": documents, "schema_version": "1.0.0", "used_for_selection": false,
	}, len(scenarios), nil
}

func sensitivityDelta(value, baseline metrics.Summary) map[string]any {
	sharpe := any(nil)
	if value.Sharpe != nil && baseline.Sharpe != nil {
		sharpe = decimal(*value.Sharpe - *baseline.Sharpe)
	}
	return map[string]any{
		"annualized_cost_drag": decimal(value.AnnualizedCostDrag - baseline.AnnualizedCostDrag),
		"annualized_return":    decimal(value.AnnualizedReturn - baseline.AnnualizedReturn),
		"annualized_turnover":  decimal(value.AnnualizedTurnover - baseline.AnnualizedTurnover),
		"maximum_drawdown":     decimal(value.MaximumDrawdown - baseline.MaximumDrawdown),
		"sharpe":               sharpe,
	}
}

func sensitivityCandidateDocument(candidate Candidate) map[string]any {
	return map[string]any{
		"allocator":      map[string]any{"reliability_window": candidate.Allocator.ReliabilityWindow, "volatility_window": candidate.Allocator.VolatilityWindow},
		"mean_reversion": map[string]any{"entry_z": decimal(candidate.MeanReversion.EntryZ), "exit_z": decimal(candidate.MeanReversion.ExitZ), "lookback": candidate.MeanReversion.Lookback},
		"trend":          map[string]any{"fast_window": candidate.Trend.FastWindow, "slow_window": candidate.Trend.SlowWindow},
	}
}

func finalArtifactBytes(base Result, ensemble EnsembleRun, labels []RegimeLabel) (map[string][]byte, error) {
	start, end := projectdata.FinalHoldoutStart, projectdata.FinalHoldoutEnd
	artifacts := make(map[string][]byte)
	baseBytes, err := ArtifactBytes(filterResultPeriod(base, start, end))
	if err != nil {
		return nil, err
	}
	for name, content := range baseBytes {
		artifacts["base_"+name] = content
	}
	ensembleBytes, err := ArtifactBytes(filterResultPeriod(ensemble.Result, start, end))
	if err != nil {
		return nil, err
	}
	for name, content := range ensembleBytes {
		artifacts["ensemble_"+name] = content
	}
	allocations := make([]StrategyAllocation, 0)
	for _, allocation := range ensemble.Allocations {
		if !allocation.Date.Before(start) && !allocation.Date.After(end) {
			allocations = append(allocations, allocation)
		}
	}
	artifacts["ensemble_strategy_weights.csv"], err = strategyAllocationCSV(allocations)
	if err != nil {
		return nil, err
	}
	artifacts["regime_labels.csv"], err = regimeLabelsCSV(labels)
	return artifacts, err
}

func filterResultPeriod(result Result, start, end time.Time) Result {
	inside := func(date time.Time) bool { return !date.Before(start) && !date.After(end) }
	filtered := Result{}
	for _, value := range result.Signals {
		if inside(value.Date) {
			filtered.Signals = append(filtered.Signals, value)
		}
	}
	for _, value := range result.TargetWeights {
		if inside(value.Date) {
			filtered.TargetWeights = append(filtered.TargetWeights, value)
		}
	}
	for _, value := range result.ExecutedWeights {
		if inside(value.Date) {
			filtered.ExecutedWeights = append(filtered.ExecutedWeights, value)
		}
	}
	for _, value := range result.Orders {
		if inside(value.Date) {
			filtered.Orders = append(filtered.Orders, value)
		}
	}
	for _, value := range result.Trades {
		if inside(value.Date) {
			filtered.Trades = append(filtered.Trades, value)
		}
	}
	for _, value := range result.Ledger {
		if inside(value.Date) {
			filtered.Ledger = append(filtered.Ledger, value)
		}
	}
	for _, value := range result.Returns {
		if inside(value.Date) {
			filtered.Returns = append(filtered.Returns, value)
		}
	}
	return filtered
}

func regimeLabelsCSV(labels []RegimeLabel) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"date", "regime", "trend_126", "vol_30"}); err != nil {
		return nil, err
	}
	for _, label := range labels {
		if err := writer.Write([]string{label.Date.Format("2006-01-02"), label.Name, decimal(label.Trend126), decimal(label.Vol30)}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}

func meanNetReturn(result Result, strategyName string, dates map[string]struct{}) (float64, error) {
	total, count := 0.0, 0
	for _, record := range result.Returns {
		if record.Strategy == strategyName {
			if _, ok := dates[record.Date.Format("2006-01-02")]; ok {
				total += record.NetReturn
				count++
			}
		}
	}
	if count != len(dates) {
		return 0, fmt.Errorf("incomplete regime returns for %s", strategyName)
	}
	return total / float64(count), nil
}

func validateFullCoverage(bars []domain.MarketBar) error {
	days, err := groupDays(bars)
	if err != nil {
		return err
	}
	if len(days) != 1826 || !days[0].date.Equal(projectdata.DevelopmentStart) || !days[len(days)-1].date.Equal(projectdata.FinalHoldoutEnd) {
		return fmt.Errorf("full snapshot coverage mismatch")
	}
	for _, day := range days {
		if len(day.bars) != 3 {
			return fmt.Errorf("incomplete full snapshot day")
		}
	}
	return nil
}

func decimal(value float64) string {
	formatted, _ := canonical.Decimal12(value)
	return formatted
}
