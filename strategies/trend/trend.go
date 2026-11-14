package trend

import (
	"fmt"

	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/strategy"
)

const Name = "sma_trend"

type Strategy struct {
	Fast int
	Slow int
}

func New(fast, slow int) (*Strategy, error) {
	if fast <= 0 || slow <= 0 || fast >= slow {
		return nil, fmt.Errorf("trend windows require 0 < fast < slow")
	}
	return &Strategy{Fast: fast, Slow: slow}, nil
}

func MustNew(fast, slow int) strategy.Strategy {
	value, err := New(fast, slow)
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
		start := context.Date.AddDate(0, 0, -(s.Slow - 1))
		rangeBars, err := context.History.GetRange(bar.Symbol, start, context.Date)
		if err != nil {
			return strategy.Decision{}, err
		}
		value := 0.0
		if len(rangeBars) == s.Slow {
			fast := meanClose(rangeBars[len(rangeBars)-s.Fast:])
			slow := meanClose(rangeBars)
			if fast > slow {
				value = 1
			}
		}
		signals = append(signals, domain.Signal{Date: context.Date, Strategy: Name, Symbol: bar.Symbol, Value: value})
	}
	return strategy.Decision{
		Date: context.Date, Signals: signals,
		TargetWeights: strategy.EqualActiveWeights(context.Date, Name, signals),
		Rebalance:     true,
	}, nil
}

func meanClose(bars []domain.MarketBar) float64 {
	total := 0.0
	for _, bar := range bars {
		total += bar.Close
	}
	return total / float64(len(bars))
}
