package strategy

import (
	"fmt"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

// RangeSource is the small host boundary required by the strategies. Its shape
// intentionally mirrors Argo's GetRange API while keeping financial logic
// independent from a particular Argo runtime release.
type RangeSource interface {
	GetRange(symbol string, start, end time.Time) ([]domain.MarketBar, error)
}

// Context contains exactly the information available after one daily close.
type Context struct {
	Date    time.Time
	Bars    []domain.MarketBar
	History RangeSource
}

// Decision is produced at close d and may only be executed on the next valid
// date. Rebalance=false preserves current holdings even though daily target
// weights are still recorded.
type Decision struct {
	Date          time.Time
	Signals       []domain.Signal
	TargetWeights []domain.TargetWeight
	Rebalance     bool
}

// Strategy is the deterministic core shared by native tests and Argo adapters.
type Strategy interface {
	Name() string
	Decide(Context) (Decision, error)
}

// Factory creates fresh state for each independent backtest.
type Factory struct {
	Name string
	New  func() Strategy
}

// MemoryHistory is an append-only implementation of the Argo-like range API.
// The runner appends date d immediately before asking for the date d decision,
// so a strategy cannot access a future observation through this interface.
type MemoryHistory struct {
	bySymbol map[string][]domain.MarketBar
}

func NewMemoryHistory() *MemoryHistory {
	return &MemoryHistory{bySymbol: make(map[string][]domain.MarketBar)}
}

func (history *MemoryHistory) Add(bars []domain.MarketBar) error {
	sorted := append([]domain.MarketBar(nil), bars...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Symbol < sorted[j].Symbol })
	for _, bar := range sorted {
		series := history.bySymbol[bar.Symbol]
		if len(series) > 0 && !bar.Date.After(series[len(series)-1].Date) {
			return fmt.Errorf("history for %s must be appended in increasing date order", bar.Symbol)
		}
		history.bySymbol[bar.Symbol] = append(series, bar)
	}
	return nil
}

func (history *MemoryHistory) GetRange(symbol string, start, end time.Time) ([]domain.MarketBar, error) {
	if symbol == "" || end.Before(start) {
		return nil, fmt.Errorf("invalid range request")
	}
	series := history.bySymbol[symbol]
	result := make([]domain.MarketBar, 0, len(series))
	for _, bar := range series {
		if bar.Date.Before(start) || bar.Date.After(end) {
			continue
		}
		result = append(result, bar)
	}
	return result, nil
}

// EqualActiveWeights turns per-asset long/cash signals into a long-only target.
func EqualActiveWeights(date time.Time, name string, signals []domain.Signal) []domain.TargetWeight {
	active := 0
	for _, signal := range signals {
		if signal.Value > 0 {
			active++
		}
	}
	weight := 0.0
	if active > 0 {
		weight = 1 / float64(active)
	}
	weights := make([]domain.TargetWeight, 0, len(signals))
	for _, signal := range signals {
		value := 0.0
		if signal.Value > 0 {
			value = weight
		}
		weights = append(weights, domain.TargetWeight{Date: date, Strategy: name, Symbol: signal.Symbol, Weight: value})
	}
	return weights
}

func SortedBars(bars []domain.MarketBar) []domain.MarketBar {
	result := append([]domain.MarketBar(nil), bars...)
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })
	return result
}
