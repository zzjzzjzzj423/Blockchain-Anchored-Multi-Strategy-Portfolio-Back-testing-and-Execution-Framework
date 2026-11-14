package execution

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestRejectsInvalidConfigs(t *testing.T) {
	t.Parallel()

	valid := DefaultConfig("test")
	tests := []Config{
		func() Config { value := valid; value.Strategy = ""; return value }(),
		func() Config { value := valid; value.InitialCash = 0; return value }(),
		func() Config { value := valid; value.CommissionRate = -1; return value }(),
		func() Config { value := valid; value.SlippageRate = math.Inf(1); return value }(),
		func() Config { value := valid; value.MinimumNotional = -1; return value }(),
		func() Config { value := valid; value.QuantityDecimals = 13; return value }(),
		func() Config { value := valid; value.CashTolerance = 0; return value }(),
	}
	for index, config := range tests {
		if _, err := New(config); err == nil {
			t.Errorf("invalid config %d was accepted: %+v", index, config)
		}
	}
}

func TestRejectsMalformedDayInputs(t *testing.T) {
	t.Parallel()

	date := utcDate(2024, 1, 1)
	tests := []DayInput{
		{Date: date.Add(time.Hour), Bars: bars(date.Add(time.Hour), "AAA", 100)},
		{Date: date, Bars: bars(date, "AAA", 100), TargetWeights: weights("AAA", 0.5)},
		{Date: date, Bars: bars(date.AddDate(0, 0, 1), "AAA", 100)},
		{Date: date, Bars: []domain.MarketBar{{Date: date, Symbol: "AAA", Close: 0}}},
		{Date: date, Bars: bars(date, "AAA", 100, "AAA", 100)},
		{Date: date, Bars: bars(date, "AAA", 100), Rebalance: true, TargetWeights: []domain.TargetWeight{{Strategy: "other", Symbol: "AAA", Weight: 0.5}}},
		{Date: date, Bars: bars(date, "AAA", 100), Rebalance: true, TargetWeights: weights("AAA", -0.1)},
		{Date: date, Bars: bars(date, "AAA", 100), Rebalance: true, TargetWeights: weights("BBB", 0.5)},
		{Date: date, Bars: bars(date, "AAA", 100), Rebalance: true, TargetWeights: weights("AAA", 0.4, "AAA", 0.5)},
	}
	for index, input := range tests {
		engine := newTestEngine(t)
		if _, err := engine.Step(input); err == nil {
			t.Errorf("invalid day input %d was accepted: %+v", index, input)
		}
	}
}

func TestDatesMustIncreaseAndHeldSymbolsNeedPrices(t *testing.T) {
	t.Parallel()

	date := utcDate(2024, 1, 1)
	engine := newTestEngine(t)
	_, err := engine.Step(DayInput{Date: date, Bars: bars(date, "AAA", 100), Rebalance: true, TargetWeights: weights("AAA", 0.5)})
	if err != nil {
		t.Fatal(err)
	}
	if engine.Cash() < 0 {
		t.Fatal("cash became negative")
	}
	if _, err := engine.Step(DayInput{Date: date, Bars: bars(date, "AAA", 100)}); err == nil {
		t.Fatal("repeated date was accepted")
	}
	if _, err := engine.Step(DayInput{Date: date.AddDate(0, 0, 1), Bars: nil}); err == nil {
		t.Fatal("missing price for held symbol was accepted")
	}
}

func TestTinySellIsSkipped(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t)
	first := utcDate(2024, 1, 1)
	_, err := engine.Step(DayInput{Date: first, Bars: bars(first, "AAA", 100), Rebalance: true, TargetWeights: weights("AAA", 0.02)})
	if err != nil {
		t.Fatal(err)
	}
	second := first.AddDate(0, 0, 1)
	result, err := engine.Step(DayInput{Date: second, Bars: bars(second, "AAA", 25), Rebalance: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Orders) != 1 || result.Orders[0].SkipReason != "below_min_notional" || len(result.Trades) != 0 {
		t.Fatalf("tiny sell was not skipped: %+v", result)
	}
}

func TestFrozenConfigRejectsTrailingDocument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "configs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"cash_tolerance":"0.000000010000","commission_rate":"0.001000000000","initial_cash":"1000000.000000000000","minimum_notional":"10.000000000000","quantity_decimals":8,"residual_tolerance":"0.000000000100","schema_version":"1.0.0","slippage_rate":"0.000500000000"} {}`
	if err := os.WriteFile(filepath.Join(directory, "costs.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrozenConfig(root, "test"); err == nil {
		t.Fatal("multiple JSON documents were accepted")
	}
}

func TestLedgerCSVRejectsNonFiniteNumber(t *testing.T) {
	t.Parallel()

	_, err := LedgerCSV([]domain.LedgerEntry{{Date: utcDate(2024, 1, 1), Equity: math.Inf(1)}})
	if err == nil {
		t.Fatal("non-finite ledger value was accepted")
	}
}
