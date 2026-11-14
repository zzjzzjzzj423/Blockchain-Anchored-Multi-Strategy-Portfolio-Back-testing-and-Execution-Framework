package buyhold

import (
	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/strategy"
)

const Name = "buy_and_hold"

type Strategy struct {
	initialized bool
}

func New() strategy.Strategy { return &Strategy{} }

func (s *Strategy) Name() string { return Name }

func (s *Strategy) Decide(context strategy.Context) (strategy.Decision, error) {
	bars := strategy.SortedBars(context.Bars)
	signals := make([]domain.Signal, 0, len(bars))
	for _, bar := range bars {
		signals = append(signals, domain.Signal{Date: context.Date, Strategy: Name, Symbol: bar.Symbol, Value: 1})
	}
	decision := strategy.Decision{
		Date: context.Date, Signals: signals,
		TargetWeights: strategy.EqualActiveWeights(context.Date, Name, signals),
		Rebalance:     !s.initialized,
	}
	s.initialized = true
	return decision, nil
}
