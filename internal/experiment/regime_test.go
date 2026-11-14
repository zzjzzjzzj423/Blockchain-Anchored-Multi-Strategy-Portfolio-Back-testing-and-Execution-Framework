package experiment

import (
	"math"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestRegimeLabelsAreLaggedAndComplete(t *testing.T) {
	start := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := syntheticRegimeBars(start, 420)
	threshold, err := DevelopmentVolatilityThreshold(bars[:360*3])
	if err != nil || threshold <= 0 {
		t.Fatalf("threshold = %v, %v", threshold, err)
	}
	evaluationStart := start.AddDate(0, 0, 380)
	evaluationEnd := start.AddDate(0, 0, 419)
	labels, err := ClassifyRegimes(bars, evaluationStart, evaluationEnd, threshold)
	if err != nil || len(labels) != 40 {
		t.Fatalf("labels = %d, %v", len(labels), err)
	}

	mutated := append([]domain.MarketBar(nil), bars...)
	for index := range mutated {
		if mutated[index].Date.Equal(evaluationStart) {
			mutated[index].Close *= 3
		}
	}
	changed, err := ClassifyRegimes(mutated, evaluationStart, evaluationEnd, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if labels[0] != changed[0] {
		t.Fatalf("current-day prices changed current regime: %#v != %#v", labels[0], changed[0])
	}
}

func syntheticRegimeBars(start time.Time, days int) []domain.MarketBar {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	bars := make([]domain.MarketBar, 0, days*len(symbols))
	for day := 0; day < days; day++ {
		for symbolIndex, symbol := range symbols {
			price := 100 * (1 + 0.001*float64(day) + 0.01*math.Sin(float64(day+symbolIndex)))
			bars = append(bars, domain.MarketBar{Date: start.AddDate(0, 0, day), Symbol: symbol, Open: price, High: price, Low: price, Close: price, Volume: 1})
		}
	}
	return bars
}
