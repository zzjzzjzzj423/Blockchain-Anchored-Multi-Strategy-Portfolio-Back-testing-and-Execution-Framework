package execution

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"msbd5017/portfolio-proof/internal/domain"
)

func TestFiveDayGoldenLedger(t *testing.T) {
	t.Parallel()

	entries := fiveDayLedger(t, false)
	actual, err := LedgerCSV(entries)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "tests", "golden", "execution_ledger.csv")
	expected, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		index := firstDifferentByte(actual, expected)
		t.Fatalf("golden ledger mismatch at byte %d (actual length %d, expected length %d)\nactual:\n%s", index, len(actual), len(expected), actual)
	}
}

func firstDifferentByte(left, right []byte) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}

func TestFiveDayScenarioIsIndependentOfInputOrder(t *testing.T) {
	t.Parallel()

	forward, err := LedgerCSV(fiveDayLedger(t, false))
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := LedgerCSV(fiveDayLedger(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forward, reversed) {
		t.Fatal("five-day ledger changed when bar and target inputs were reversed")
	}
}

func fiveDayLedger(t *testing.T, reverse bool) []domain.LedgerEntry {
	t.Helper()
	config := DefaultConfig("golden")
	config.InitialCash = 100_000
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	inputs := []DayInput{
		{Date: utcDate(2024, 1, 1), Bars: bars(utcDate(2024, 1, 1), "AAA", 100.0, "BBB", 50.0)},
		{Date: utcDate(2024, 1, 2), Bars: bars(utcDate(2024, 1, 2), "AAA", 100.0, "BBB", 50.0), Rebalance: true, TargetWeights: weightsFor("golden", "AAA", 0.5, "BBB", 0.5)},
		{Date: utcDate(2024, 1, 3), Bars: bars(utcDate(2024, 1, 3), "AAA", 110.0, "BBB", 45.0)},
		{Date: utcDate(2024, 1, 4), Bars: bars(utcDate(2024, 1, 4), "AAA", 105.0, "BBB", 47.0), Rebalance: true, TargetWeights: weightsFor("golden", "BBB", 1.0)},
		{Date: utcDate(2024, 1, 5), Bars: bars(utcDate(2024, 1, 5), "AAA", 103.0, "BBB", 49.0), Rebalance: true},
	}
	for _, input := range inputs {
		if reverse {
			reverseBars(input.Bars)
			reverseWeights(input.TargetWeights)
		}
		result, err := engine.Step(input)
		if err != nil {
			t.Fatalf("step %s: %v", input.Date.Format("2006-01-02"), err)
		}
		assertResiduals(t, result.Ledger)
	}
	return engine.Ledger()
}

func weightsFor(strategy string, values ...any) []domain.TargetWeight {
	result := make([]domain.TargetWeight, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		result = append(result, domain.TargetWeight{Strategy: strategy, Symbol: values[index].(string), Weight: testNumber(values[index+1])})
	}
	return result
}

func reverseBars(values []domain.MarketBar) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseWeights(values []domain.TargetWeight) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
