package trend

import (
	"errors"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	strategyapi "msbd5017/portfolio-proof/internal/strategy"
)

func TestWarmupAndCrossBoundary(t *testing.T) {
	instance, err := New(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	history := strategyapi.NewMemoryHistory()
	closes := []float64{3, 2, 1, 10}
	want := []float64{0, 0, 0, 1}
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
		if decision.Signals[0].Value != want[index] {
			t.Fatalf("day %d signal = %v, want %v", index+1, decision.Signals[0].Value, want[index])
		}
	}
}

func TestEqualMovingAveragesStayCash(t *testing.T) {
	instance, _ := New(2, 3)
	history := strategyapi.NewMemoryHistory()
	for index := 0; index < 3; index++ {
		date := time.Date(2024, 1, index+1, 0, 0, 0, 0, time.UTC)
		bars := []domain.MarketBar{{Date: date, Symbol: "BTC", Close: 5}}
		_ = history.Add(bars)
		decision, err := instance.Decide(strategyapi.Context{Date: date, Bars: bars, History: history})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Signals[0].Value != 0 {
			t.Fatal("equal SMA values must not create a long signal")
		}
	}
}

func TestRejectsInvalidWindows(t *testing.T) {
	if _, err := New(10, 10); err == nil {
		t.Fatal("expected invalid window error")
	}
}

func TestRangeErrorIsReturned(t *testing.T) {
	instance, _ := New(2, 3)
	date := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	_, err := instance.Decide(strategyapi.Context{
		Date: date, Bars: []domain.MarketBar{{Date: date, Symbol: "BTC", Close: 1}},
		History: failingHistory{},
	})
	if err == nil {
		t.Fatal("expected range error")
	}
}

func TestMustNewPanicsForInvalidWindows(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = MustNew(3, 2)
}

type failingHistory struct{}

func (failingHistory) GetRange(string, time.Time, time.Time) ([]domain.MarketBar, error) {
	return nil, errors.New("range unavailable")
}
