package evidence

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"msbd5017/portfolio-proof/internal/canonical"
)

type deterministicEvent struct {
	Date       string            `json:"date"`
	EventType  string            `json:"event_type"`
	Payload    map[string]string `json:"payload"`
	PhaseOrder int               `json:"phase_order"`
	Sequence   uint64            `json:"sequence"`
	SideOrder  int               `json:"side_order"`
	Strategy   string            `json:"strategy"`
	Symbol     string            `json:"symbol"`
}

func deterministicEvents(files map[string][]byte) ([]byte, error) {
	var events []deterministicEvent
	sequence := uint64(0)
	for _, source := range []struct {
		path, eventType string
		phase           int
	}{
		{"results/base_strategy_signals.csv", "signal", 10},
		{"results/ensemble_signals.csv", "signal", 10},
		{"results/ensemble_weights.csv", "allocation", 20},
	} {
		rows, err := csvEvents(files[source.path], source.eventType, source.phase, &sequence)
		if err != nil {
			return nil, err
		}
		events = append(events, rows...)
	}
	orders, err := csvEvents(files["results/orders.csv"], "order", 0, &sequence)
	if err != nil {
		return nil, err
	}
	events = append(events, orders...)
	ledger, err := csvEvents(files["results/ledger.csv"], "accounting", 50, &sequence)
	if err != nil {
		return nil, err
	}
	events = append(events, ledger...)

	var metricsDocument map[string]any
	if err := json.Unmarshal(files["results/overall_metrics.json"], &metricsDocument); err != nil {
		return nil, err
	}
	metricsValue, ok := metricsDocument["metrics"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("overall metrics document has no metrics")
	}
	metricNames := make([]string, 0, len(metricsValue))
	for name := range metricsValue {
		metricNames = append(metricNames, name)
	}
	sort.Strings(metricNames)
	for _, name := range metricNames {
		sequence++
		encoded, err := canonical.JSON(metricsValue[name])
		if err != nil {
			return nil, err
		}
		events = append(events, deterministicEvent{
			Date: "2025-12-31", EventType: "metric", Payload: map[string]string{"canonical_sha256": digest(encoded)},
			PhaseOrder: 60, Sequence: sequence, SideOrder: 2, Strategy: name,
		})
	}
	sort.Slice(events, func(i, j int) bool {
		left, right := events[i], events[j]
		if left.Date != right.Date {
			return left.Date < right.Date
		}
		if left.PhaseOrder != right.PhaseOrder {
			return left.PhaseOrder < right.PhaseOrder
		}
		if left.Strategy != right.Strategy {
			return left.Strategy < right.Strategy
		}
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		if left.SideOrder != right.SideOrder {
			return left.SideOrder < right.SideOrder
		}
		return left.Sequence < right.Sequence
	})
	var output bytes.Buffer
	for _, event := range events {
		line, err := canonical.JSON(event)
		if err != nil {
			return nil, err
		}
		output.Write(line)
	}
	return output.Bytes(), nil
}

func csvEvents(content []byte, eventType string, fixedPhase int, nextSequence *uint64) ([]deterministicEvent, error) {
	records, err := csv.NewReader(bytes.NewReader(content)).ReadAll()
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("read %s CSV: %w", eventType, err)
	}
	header := records[0]
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[name] = index
	}
	get := func(row []string, name string) string {
		if index, ok := columns[name]; ok {
			return row[index]
		}
		return ""
	}
	result := make([]deterministicEvent, 0, len(records)-1)
	for _, row := range records[1:] {
		*nextSequence++
		phase := fixedPhase
		if text := get(row, "phase_order"); text != "" {
			phase, _ = strconv.Atoi(text)
		}
		sequence := *nextSequence
		if text := get(row, "sequence"); text != "" {
			sequence, _ = strconv.ParseUint(text, 10, 64)
		}
		sideOrder := 2
		switch strings.ToLower(get(row, "side")) {
		case "sell":
			sideOrder = 0
		case "buy":
			sideOrder = 1
		}
		payload := make(map[string]string, len(header))
		for index, name := range header {
			payload[name] = row[index]
		}
		result = append(result, deterministicEvent{
			Date: get(row, "date"), EventType: eventType, Payload: payload, PhaseOrder: phase,
			Sequence: sequence, SideOrder: sideOrder, Strategy: get(row, "strategy"), Symbol: get(row, "symbol"),
		})
	}
	return result, nil
}
