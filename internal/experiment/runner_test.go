package experiment

import (
	"bytes"
	"context"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	strategyapi "msbd5017/portfolio-proof/internal/strategy"
	"msbd5017/portfolio-proof/strategies/buyhold"
	"msbd5017/portfolio-proof/strategies/meanreversion"
	"msbd5017/portfolio-proof/strategies/trend"
)

func TestOneDayLagAndBuyHoldOnlyInitialTrade(t *testing.T) {
	factory := strategyapi.Factory{Name: buyhold.Name, New: buyhold.New}
	result, err := RunAll(context.Background(), syntheticBars(5), []strategyapi.Factory{factory}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 3 {
		t.Fatalf("trades = %d, want one initial trade per asset", len(result.Trades))
	}
	for _, trade := range result.Trades {
		if trade.Date.Format("2006-01-02") != "2024-01-02" {
			t.Fatalf("trade date = %s, want next valid day", trade.Date)
		}
	}
	for _, weight := range result.ExecutedWeights[:3] {
		if weight.Weight != 0 {
			t.Fatal("signal-day executed weights must remain cash")
		}
	}
	if result.Returns[0].NetReturn != 0 || result.Returns[1].NetReturn >= 0 {
		t.Fatalf("unexpected lagged returns: %#v", result.Returns[:2])
	}
}

func TestSerialAndParallelArtifactsAreByteIdentical(t *testing.T) {
	factories := testFactories()
	bars := syntheticBars(140)
	serial, err := RunAll(context.Background(), bars, factories, false)
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := RunAll(context.Background(), bars, factories, true)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := ArtifactBytes(serial)
	right, _ := ArtifactBytes(parallel)
	if !equalArtifacts(left, right) {
		for path := range left {
			if !bytes.Equal(left[path], right[path]) {
				t.Errorf("artifact differs: %s", path)
			}
		}
	}
}

func TestFutureMutationAndTruncationPreservePrefix(t *testing.T) {
	bars := syntheticBars(140)
	config := M3Config{Factories: testFactories()}
	cutoff := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	ok, err := verifyPrefixInvariance(context.Background(), bars, cutoff, config)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("future changes altered the historical prefix")
	}
}

func testFactories() []strategyapi.Factory {
	return []strategyapi.Factory{
		{Name: buyhold.Name, New: buyhold.New},
		{Name: trend.Name, New: func() strategyapi.Strategy { return trend.MustNew(3, 8) }},
		{Name: meanreversion.Name, New: func() strategyapi.Strategy { return meanreversion.MustNew(5, -0.8, 0) }},
	}
}

func syntheticBars(days int) []domain.MarketBar {
	symbols := []string{"BTC", "ETH", "SOL"}
	result := make([]domain.MarketBar, 0, days*len(symbols))
	for day := 0; day < days; day++ {
		date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day)
		for index, symbol := range symbols {
			base := 100.0 + float64(index*25)
			closePrice := base + float64((day%17)-8) + float64(day)/10
			result = append(result, domain.MarketBar{Date: date, Symbol: symbol, Open: closePrice, High: closePrice, Low: closePrice, Close: closePrice, Volume: 1})
		}
	}
	return result
}
