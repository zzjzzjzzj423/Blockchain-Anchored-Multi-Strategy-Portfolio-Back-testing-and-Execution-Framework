package strategy

import (
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestMemoryHistoryRangeIsInclusiveAndAppendOnly(t *testing.T) {
	history := NewMemoryHistory()
	for index := 0; index < 3; index++ {
		date := time.Date(2024, 1, index+1, 0, 0, 0, 0, time.UTC)
		if err := history.Add([]domain.MarketBar{{Date: date, Symbol: "BTC", Close: float64(index + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	values, err := history.GetRange("BTC", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].Close != 2 || values[1].Close != 3 {
		t.Fatalf("range = %#v", values)
	}
	if err := history.Add([]domain.MarketBar{{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Symbol: "BTC", Close: 3}}); err == nil {
		t.Fatal("expected append order error")
	}
}

func TestEqualActiveWeights(t *testing.T) {
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	weights := EqualActiveWeights(date, "test", []domain.Signal{
		{Symbol: "BTC", Value: 1}, {Symbol: "ETH", Value: 0}, {Symbol: "SOL", Value: 1},
	})
	if weights[0].Weight != 0.5 || weights[1].Weight != 0 || weights[2].Weight != 0.5 {
		t.Fatalf("weights = %#v", weights)
	}
}

func TestInvalidRangeAndSortedBars(t *testing.T) {
	history := NewMemoryHistory()
	date := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := history.GetRange("", date, date); err == nil {
		t.Fatal("expected invalid range error")
	}
	bars := SortedBars([]domain.MarketBar{{Symbol: "SOL"}, {Symbol: "BTC"}, {Symbol: "ETH"}})
	if bars[0].Symbol != "BTC" || bars[1].Symbol != "ETH" || bars[2].Symbol != "SOL" {
		t.Fatalf("sorted bars = %#v", bars)
	}
}
