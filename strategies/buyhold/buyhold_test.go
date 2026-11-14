package buyhold

import (
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	strategyapi "msbd5017/portfolio-proof/internal/strategy"
)

func TestOnlyFirstDecisionRebalances(t *testing.T) {
	instance := New()
	history := strategyapi.NewMemoryHistory()
	for index := 0; index < 2; index++ {
		date := time.Date(2024, 1, index+1, 0, 0, 0, 0, time.UTC)
		bars := []domain.MarketBar{{Date: date, Symbol: "BTC", Close: 100}, {Date: date, Symbol: "ETH", Close: 50}, {Date: date, Symbol: "SOL", Close: 10}}
		if err := history.Add(bars); err != nil {
			t.Fatal(err)
		}
		decision, err := instance.Decide(strategyapi.Context{Date: date, Bars: bars, History: history})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Rebalance != (index == 0) {
			t.Fatalf("day %d rebalance = %t", index+1, decision.Rebalance)
		}
		for _, weight := range decision.TargetWeights {
			if weight.Weight != 1.0/3.0 {
				t.Fatalf("weight = %v, want 1/3", weight.Weight)
			}
		}
	}
}
