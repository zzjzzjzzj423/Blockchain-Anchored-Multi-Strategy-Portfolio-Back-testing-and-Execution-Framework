package execution

import (
	"math"
	"reflect"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestPureCashAndMinimumNotionalSkip(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t)
	first := utcDate(2024, 1, 1)
	result, err := engine.Step(DayInput{Date: first, Bars: bars(first, "AAA", 100)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ledger.Equity != 1000 || result.Ledger.Cash != 1000 || len(result.Trades) != 0 {
		t.Fatalf("unexpected pure-cash result: %+v", result)
	}

	second := utcDate(2024, 1, 2)
	result, err = engine.Step(DayInput{
		Date:          second,
		Bars:          bars(second, "AAA", 100),
		Rebalance:     true,
		TargetWeights: weights("AAA", 0.005),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Orders) != 1 || result.Orders[0].Status != domain.OrderSkipped || result.Orders[0].SkipReason != "below_min_notional" {
		t.Fatalf("small order was not deterministically skipped: %+v", result.Orders)
	}
	if len(result.Trades) != 0 || result.Ledger.Equity != 1000 {
		t.Fatalf("skipped order changed the portfolio: %+v", result)
	}
}

func TestUniformBuyScalingCostsAndQuantityPrecision(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t)
	date := utcDate(2024, 1, 1)
	result, err := engine.Step(DayInput{
		Date:          date,
		Bars:          bars(date, "BBB", 50, "AAA", 100),
		Rebalance:     true,
		TargetWeights: weights("BBB", 0.5, "AAA", 0.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 || result.Trades[0].Symbol != "AAA" || result.Trades[1].Symbol != "BBB" {
		t.Fatalf("trades are not stable symbol order: %+v", result.Trades)
	}
	if result.Orders[0].ScaleFactor >= 1 || result.Orders[0].ScaleFactor != result.Orders[1].ScaleFactor {
		t.Fatalf("buy orders did not receive one uniform scale: %+v", result.Orders)
	}
	for _, trade := range result.Trades {
		quantized := math.RoundToEven(trade.Quantity*1e8) / 1e8
		if math.Abs(trade.Quantity-quantized) > 1e-12 {
			t.Fatalf("quantity has more than 8 decimals: %.12f", trade.Quantity)
		}
		wantExecutionPrice := trade.ReferencePrice * 1.0005
		wantCommission := trade.ExecutedNotional * 0.001
		if !closeEnough(trade.ExecutionPrice, wantExecutionPrice, 1e-12) || !closeEnough(trade.Commission, wantCommission, 1e-12) {
			t.Fatalf("incorrect cost formula: %+v", trade)
		}
	}
	if result.Ledger.Cash < 0 || result.Ledger.Cash > 0.001 {
		t.Fatalf("scaled buys left unexpected cash %.12f", result.Ledger.Cash)
	}
	assertResiduals(t, result.Ledger)
}

func TestSellsExecuteBeforeBuysAndSymbolsAreSorted(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t)
	first := utcDate(2024, 1, 1)
	_, err := engine.Step(DayInput{
		Date:          first,
		Bars:          bars(first, "BBB", 50, "AAA", 100, "CCC", 25),
		Rebalance:     true,
		TargetWeights: weights("BBB", 0.5, "AAA", 0.5),
	})
	if err != nil {
		t.Fatal(err)
	}

	second := utcDate(2024, 1, 2)
	result, err := engine.Step(DayInput{
		Date:          second,
		Bars:          bars(second, "CCC", 25, "BBB", 50, "AAA", 100),
		Rebalance:     true,
		TargetWeights: weights("CCC", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 3 {
		t.Fatalf("trade count = %d, want 3", len(result.Trades))
	}
	wantSymbols := []string{"AAA", "BBB", "CCC"}
	wantSides := []domain.Side{domain.SideSell, domain.SideSell, domain.SideBuy}
	for index, trade := range result.Trades {
		if trade.Symbol != wantSymbols[index] || trade.Side != wantSides[index] {
			t.Fatalf("trade[%d] = %+v", index, trade)
		}
		if index > 0 && trade.Sequence <= result.Trades[index-1].Sequence {
			t.Fatal("trade sequence is not strictly increasing")
		}
	}
	assertResiduals(t, result.Ledger)
}

func TestInputOrderingCannotChangeResults(t *testing.T) {
	t.Parallel()

	firstEngine := newTestEngine(t)
	secondEngine := newTestEngine(t)
	date := utcDate(2024, 1, 1)
	first, err := firstEngine.Step(DayInput{
		Date:          date,
		Bars:          bars(date, "AAA", 100, "BBB", 50),
		Rebalance:     true,
		TargetWeights: weights("AAA", 0.4, "BBB", 0.6),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondEngine.Step(DayInput{
		Date:          date,
		Bars:          bars(date, "BBB", 50, "AAA", 100),
		Rebalance:     true,
		TargetWeights: weights("BBB", 0.6, "AAA", 0.4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input ordering changed output:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestRejectsInvalidTargetWeights(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t)
	date := utcDate(2024, 1, 1)
	_, err := engine.Step(DayInput{
		Date:          date,
		Bars:          bars(date, "AAA", 100, "BBB", 50),
		Rebalance:     true,
		TargetWeights: weights("AAA", 0.6, "BBB", 0.5),
	})
	if err == nil {
		t.Fatal("weight sum greater than one was accepted")
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	config := DefaultConfig("test_strategy")
	config.InitialCash = 1000
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func utcDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func bars(date time.Time, values ...any) []domain.MarketBar {
	result := make([]domain.MarketBar, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		symbol := values[index].(string)
		close := testNumber(values[index+1])
		result = append(result, domain.MarketBar{Date: date, Symbol: symbol, Open: close, High: close, Low: close, Close: close})
	}
	return result
}

func weights(values ...any) []domain.TargetWeight {
	result := make([]domain.TargetWeight, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		result = append(result, domain.TargetWeight{Strategy: "test_strategy", Symbol: values[index].(string), Weight: testNumber(values[index+1])})
	}
	return result
}

func assertResiduals(t *testing.T, entry domain.LedgerEntry) {
	t.Helper()
	if entry.AccountingResidual >= 1e-10 || entry.PnLIdentityResidual >= 1e-10 {
		t.Fatalf("residuals exceed tolerance: %+v", entry)
	}
}

func closeEnough(left, right, tolerance float64) bool {
	return math.Abs(left-right) <= tolerance
}

func testNumber(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	default:
		panic("test value is not numeric")
	}
}
