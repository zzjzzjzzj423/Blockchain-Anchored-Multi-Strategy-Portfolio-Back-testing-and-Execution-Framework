package meanreversion

import (
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	strategyapi "msbd5017/portfolio-proof/internal/strategy"
)

func TestEntryHoldExit(t *testing.T) {
	instance, err := New(3, -0.5, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0, 0, 1, 1, 0}
	got := runSignals(t, instance, []float64{10, 10, 8, 8, 12})
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("day %d signal = %v, want %v", index+1, got[index], want[index])
		}
	}
}

func TestZeroStandardDeviationStaysCash(t *testing.T) {
	instance, _ := New(3, -0.5, 0)
	for index, signal := range runSignals(t, instance, []float64{5, 5, 5, 5}) {
		if signal != 0 {
			t.Fatalf("day %d signal = %v, want cash", index+1, signal)
		}
	}
}

func TestRejectsInvalidParameters(t *testing.T) {
	if _, err := New(1, -1, 0); err == nil {
		t.Fatal("expected invalid lookback error")
	}
	if _, err := New(3, 0, 0); err == nil {
		t.Fatal("expected invalid threshold error")
	}
}

func runSignals(t *testing.T, instance *Strategy, closes []float64) []float64 {
	t.Helper()
	history := strategyapi.NewMemoryHistory()
	result := make([]float64, 0, len(closes))
	for index, closePrice := range closes {
		date := time.Date(2024, 1, index+1, 0, 0, 0, 0, time.UTC)
		bars := []domain.MarketBar{{Date: date, Symbol: "BTC", Close: closePrice}}
		if err := history.Add(bars); err != nil {
			t.Fatal(err)
		}
		decision, err := instance.Decide(strategyapi.Context{Date: date, Bars: bars, History: history})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, decision.Signals[0].Value)
	}
	return result
}
