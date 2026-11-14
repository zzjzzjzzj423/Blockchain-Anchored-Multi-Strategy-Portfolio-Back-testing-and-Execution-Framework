package experiment

import (
	"context"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestEvaluatorCachesDuplicateCandidate(t *testing.T) {
	evaluator := NewCandidateEvaluator(syntheticBars(140))
	candidate := Candidate{
		Trend:         TrendParameters{FastWindow: 10, SlowWindow: 60},
		MeanReversion: MeanReversionParameters{Lookback: 10, EntryZ: -1, ExitZ: 0},
		Allocator:     AllocatorParameters{ReliabilityWindow: 30, VolatilityWindow: 20},
	}
	// The synthetic period does not cover the frozen validation folds, so use
	// runCandidate to exercise the expensive base-run cache directly.
	if _, _, err := evaluator.RunSelected(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if len(evaluator.baseRuns) != 1 {
		t.Fatalf("base cache entries = %d", len(evaluator.baseRuns))
	}
	second := candidate
	second.Allocator = AllocatorParameters{ReliabilityWindow: 63, VolatilityWindow: 30}
	if _, _, err := evaluator.RunSelected(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if len(evaluator.baseRuns) != 1 {
		t.Fatalf("allocator-only change reran base strategies")
	}
}

func TestMedian(t *testing.T) {
	if median([]float64{4, 1, 3, 2}) != 2.5 || median([]float64{3, 1, 2}) != 2 {
		t.Fatal("median mismatch")
	}
}

func TestEvaluatorWalkForwardAndEvaluationCache(t *testing.T) {
	start := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	symbols := []string{"BTC", "ETH", "SOL"}
	bars := make([]domain.MarketBar, 0, 1461*3)
	for day := 0; day < 1461; day++ {
		date := start.AddDate(0, 0, day)
		for index, symbol := range symbols {
			price := 100 + float64(index*25) + float64((day%31)-15) + float64(day)/20
			bars = append(bars, domain.MarketBar{Date: date, Symbol: symbol, Open: price, High: price, Low: price, Close: price, Volume: 1})
		}
	}
	evaluator := NewCandidateEvaluator(bars)
	first, err := evaluator.Evaluate(context.Background(), BaselineCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Folds) != 4 || evaluator.EvaluationCount() != 1 {
		t.Fatalf("folds/count = %d/%d", len(first.Folds), evaluator.EvaluationCount())
	}
	second, err := evaluator.Evaluate(context.Background(), BaselineCandidate())
	if err != nil || second.Assessment.CandidateID != first.Assessment.CandidateID || evaluator.EvaluationCount() != 1 {
		t.Fatalf("cached evaluation mismatch: %v", err)
	}
}
