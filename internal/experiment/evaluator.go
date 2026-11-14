package experiment

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/metrics"
	"msbd5017/portfolio-proof/internal/strategy"
	"msbd5017/portfolio-proof/strategies/buyhold"
	"msbd5017/portfolio-proof/strategies/meanreversion"
	"msbd5017/portfolio-proof/strategies/trend"
)

type FoldEvaluation struct {
	Fold    Fold
	Metrics metrics.Summary
}

type Evaluation struct {
	Assessment CandidateAssessment
	Folds      []FoldEvaluation
}

type CandidateEvaluator struct {
	bars                []domain.MarketBar
	evaluations         map[string]Evaluation
	baseRuns            map[string]Result
	evaluationCount     int
	evaluationCacheHits int
	baseRunCacheHits    int
}

func NewCandidateEvaluator(bars []domain.MarketBar) *CandidateEvaluator {
	return &CandidateEvaluator{
		bars:        append([]domain.MarketBar(nil), bars...),
		evaluations: make(map[string]Evaluation),
		baseRuns:    make(map[string]Result),
	}
}

func (evaluator *CandidateEvaluator) Evaluate(ctx context.Context, candidate Candidate) (Evaluation, error) {
	if err := candidate.Validate(); err != nil {
		return Evaluation{}, err
	}
	id, _ := candidate.ID()
	if cached, ok := evaluator.evaluations[id]; ok {
		evaluator.evaluationCacheHits++
		return cached, nil
	}
	base, ensemble, err := evaluator.runCandidate(ctx, candidate)
	_ = base
	if err != nil {
		return Evaluation{}, err
	}
	folds := WalkForwardFolds()
	if err := ValidateFolds(folds); err != nil {
		return Evaluation{}, err
	}
	evaluation := Evaluation{Folds: make([]FoldEvaluation, 0, len(folds))}
	sharpes := make([]float64, 0, len(folds))
	turnovers := make([]float64, 0, len(folds))
	worstDrawdown := 0.0
	for _, fold := range folds {
		summary, err := MetricsFor(ensemble.Result, EnsembleName, fold.ValidationStart, fold.ValidationEnd)
		if err != nil {
			return Evaluation{}, fmt.Errorf("candidate %s fold %d: %w", id, fold.ID, err)
		}
		evaluation.Folds = append(evaluation.Folds, FoldEvaluation{Fold: fold, Metrics: summary})
		if summary.Sharpe == nil {
			sharpes = append(sharpes, -math.MaxFloat64)
		} else {
			sharpes = append(sharpes, *summary.Sharpe)
		}
		turnovers = append(turnovers, summary.AnnualizedTurnover)
		if summary.MaximumDrawdown > worstDrawdown {
			worstDrawdown = summary.MaximumDrawdown
		}
	}
	evaluation.Assessment = CandidateAssessment{
		Candidate: candidate, CandidateID: id,
		MedianValidationSharpe:   median(sharpes),
		WorstValidationDrawdown:  worstDrawdown,
		MedianAnnualizedTurnover: median(turnovers),
	}
	evaluator.evaluations[id] = evaluation
	evaluator.evaluationCount++
	return evaluation, nil
}

func (evaluator *CandidateEvaluator) RunSelected(ctx context.Context, candidate Candidate) (Result, EnsembleRun, error) {
	return evaluator.runCandidate(ctx, candidate)
}

func (evaluator *CandidateEvaluator) EvaluationCount() int { return evaluator.evaluationCount }

func (evaluator *CandidateEvaluator) EvaluationCacheHits() int {
	return evaluator.evaluationCacheHits
}

func (evaluator *CandidateEvaluator) BaseRunCacheHits() int { return evaluator.baseRunCacheHits }

func (evaluator *CandidateEvaluator) runCandidate(ctx context.Context, candidate Candidate) (Result, EnsembleRun, error) {
	baseKey := fmt.Sprintf("%d/%d/%d/%g/%g", candidate.Trend.FastWindow, candidate.Trend.SlowWindow, candidate.MeanReversion.Lookback, candidate.MeanReversion.EntryZ, candidate.MeanReversion.ExitZ)
	base, ok := evaluator.baseRuns[baseKey]
	if !ok {
		factories := []strategy.Factory{
			{Name: buyhold.Name, New: buyhold.New},
			{Name: trend.Name, New: func() strategy.Strategy { return trend.MustNew(candidate.Trend.FastWindow, candidate.Trend.SlowWindow) }},
			{Name: meanreversion.Name, New: func() strategy.Strategy {
				return meanreversion.MustNew(candidate.MeanReversion.Lookback, candidate.MeanReversion.EntryZ, candidate.MeanReversion.ExitZ)
			}},
		}
		var err error
		base, err = RunAll(ctx, evaluator.bars, factories, true)
		if err != nil {
			return Result{}, EnsembleRun{}, err
		}
		evaluator.baseRuns[baseKey] = base
	} else {
		evaluator.baseRunCacheHits++
	}
	ensemble, err := RunEnsemble(ctx, evaluator.bars, base, candidate.Allocator)
	if err != nil {
		return Result{}, EnsembleRun{}, err
	}
	return base, ensemble, nil
}

func MetricsFor(result Result, strategyName string, start, end time.Time) (metrics.Summary, error) {
	if end.Before(start) {
		return metrics.Summary{}, fmt.Errorf("invalid metric period")
	}
	dates := make(map[string]struct{})
	for _, record := range result.Returns {
		if record.Strategy == strategyName && !record.Date.Before(start) && !record.Date.After(end) {
			dates[record.Date.Format("2006-01-02")] = struct{}{}
		}
	}
	return MetricsForDates(result, strategyName, dates)
}

// MetricsForDates evaluates an ordered, possibly non-contiguous regime subset.
func MetricsForDates(result Result, strategyName string, dates map[string]struct{}) (metrics.Summary, error) {
	if len(dates) == 0 {
		return metrics.Summary{}, fmt.Errorf("metric date set is empty")
	}
	returns := make(map[string]ReturnRecord)
	for _, record := range result.Returns {
		date := record.Date.Format("2006-01-02")
		if record.Strategy == strategyName {
			if _, ok := dates[date]; ok {
				returns[date] = record
			}
		}
	}
	ledgers := make([]domain.LedgerEntry, 0, len(returns))
	for _, entry := range result.Ledger {
		date := entry.Date.Format("2006-01-02")
		if entry.Strategy == strategyName {
			if _, ok := dates[date]; ok {
				ledgers = append(ledgers, entry)
			}
		}
	}
	sort.Slice(ledgers, func(i, j int) bool { return ledgers[i].Date.Before(ledgers[j].Date) })
	if len(ledgers) == 0 || len(ledgers) != len(returns) {
		return metrics.Summary{}, fmt.Errorf("incomplete returns or ledger for %s", strategyName)
	}
	initialEquity := ledgers[0].Equity - ledgers[0].NetPnL
	observations := make([]metrics.Observation, 0, len(ledgers))
	for _, ledger := range ledgers {
		date := ledger.Date.Format("2006-01-02")
		record, ok := returns[date]
		if !ok {
			return metrics.Summary{}, fmt.Errorf("missing return for %s", date)
		}
		observations = append(observations, metrics.Observation{
			NetReturn: record.NetReturn, GrossReturn: record.GrossReturn,
			Turnover: ledger.Turnover, Exposure: ledger.Exposure,
			Commission: ledger.Commission, Slippage: ledger.SlippageCost,
			AccountingResidual: ledger.AccountingResidual,
		})
	}
	return metrics.Compute(observations, initialEquity)
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}
