package meanreversion

import (
	"fmt"
	"math"

	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/strategy"
)

const Name = "zscore_mean_reversion"

type Strategy struct {
	Lookback int
	EntryZ   float64
	ExitZ    float64
	long     map[string]bool
}

func New(lookback int, entryZ, exitZ float64) (*Strategy, error) {
	if lookback < 2 || math.IsNaN(entryZ) || math.IsNaN(exitZ) || math.IsInf(entryZ, 0) || math.IsInf(exitZ, 0) || entryZ >= exitZ {
		return nil, fmt.Errorf("mean reversion requires lookback >= 2 and entry_z < exit_z")
	}
	return &Strategy{Lookback: lookback, EntryZ: entryZ, ExitZ: exitZ, long: make(map[string]bool)}, nil
}

func MustNew(lookback int, entryZ, exitZ float64) strategy.Strategy {
	value, err := New(lookback, entryZ, exitZ)
	if err != nil {
		panic(err)
	}
	return value
}

func (s *Strategy) Name() string { return Name }

func (s *Strategy) Decide(context strategy.Context) (strategy.Decision, error) {
	bars := strategy.SortedBars(context.Bars)
	signals := make([]domain.Signal, 0, len(bars))
	for _, bar := range bars {
		start := context.Date.AddDate(0, 0, -(s.Lookback - 1))
		rangeBars, err := context.History.GetRange(bar.Symbol, start, context.Date)
		if err != nil {
			return strategy.Decision{}, err
		}
		if len(rangeBars) != s.Lookback {
			s.long[bar.Symbol] = false
		} else {
			mean, standardDeviation := populationMoments(rangeBars)
			if standardDeviation == 0 {
				s.long[bar.Symbol] = false
			} else {
				z := (rangeBars[len(rangeBars)-1].Close - mean) / standardDeviation
				if !s.long[bar.Symbol] && z <= s.EntryZ {
					s.long[bar.Symbol] = true
				} else if s.long[bar.Symbol] && z >= s.ExitZ {
					s.long[bar.Symbol] = false
				}
			}
		}
		value := 0.0
		if s.long[bar.Symbol] {
			value = 1
		}
		signals = append(signals, domain.Signal{Date: context.Date, Strategy: Name, Symbol: bar.Symbol, Value: value})
	}
	return strategy.Decision{
		Date: context.Date, Signals: signals,
		TargetWeights: strategy.EqualActiveWeights(context.Date, Name, signals),
		Rebalance:     true,
	}, nil
}

func populationMoments(bars []domain.MarketBar) (float64, float64) {
	mean := 0.0
	for _, bar := range bars {
		mean += bar.Close
	}
	mean /= float64(len(bars))
	variance := 0.0
	for _, bar := range bars {
		difference := bar.Close - mean
		variance += difference * difference
	}
	variance /= float64(len(bars))
	return mean, math.Sqrt(variance)
}
