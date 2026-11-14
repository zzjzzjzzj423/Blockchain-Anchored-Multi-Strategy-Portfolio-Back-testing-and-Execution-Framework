package evidence

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeterministicEventsUseFrozenOrder(t *testing.T) {
	files := map[string][]byte{
		"results/base_strategy_signals.csv": []byte("date,strategy,symbol,signal\n2025-01-01,z,B,1.000000000000\n"),
		"results/ensemble_signals.csv":      []byte("date,strategy,symbol,signal\n2025-01-01,ensemble,A,0.500000000000\n"),
		"results/ensemble_weights.csv":      []byte("date,strategy,reliability,realized_volatility,score,strategy_weight\n2025-01-01,z,1,1,1,1\n"),
		"results/orders.csv":                []byte("date,strategy,symbol,side,phase_order,sequence\n2025-01-01,z,B,buy,40,2\n2025-01-01,z,A,sell,30,1\n"),
		"results/ledger.csv":                []byte("date,strategy,phase_order,sequence\n2025-01-01,z,50,3\n"),
		"results/overall_metrics.json":      []byte("{\"metrics\":{\"z\":{\"sharpe\":\"1.0\"}}}\n"),
	}
	first, err := deterministicEvents(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := deterministicEvents(files)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("event log is not deterministic")
	}
	lines := strings.Split(strings.TrimSpace(string(first)), "\n")
	if len(lines) != 7 || !strings.Contains(lines[0], "\"phase_order\":10") || !strings.Contains(lines[3], "\"phase_order\":30") || !strings.Contains(lines[4], "\"phase_order\":40") {
		t.Fatalf("unexpected event order: %s", first)
	}
}
