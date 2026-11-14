package experiment

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/execution"
)

// ArtifactBytes returns the complete, canonical M3 evidence set.
func ArtifactBytes(result Result) (map[string][]byte, error) {
	artifacts := make(map[string][]byte)
	var err error
	if artifacts["signals.csv"], err = signalCSV(result.Signals); err != nil {
		return nil, err
	}
	if artifacts["target_weights.csv"], err = weightCSV(result.TargetWeights, "target_weight"); err != nil {
		return nil, err
	}
	if artifacts["executed_weights.csv"], err = weightCSV(result.ExecutedWeights, "executed_weight"); err != nil {
		return nil, err
	}
	if artifacts["returns.csv"], err = returnCSV(result.Returns); err != nil {
		return nil, err
	}
	if artifacts["orders.csv"], err = orderCSV(result.Orders); err != nil {
		return nil, err
	}
	if artifacts["trades.csv"], err = tradeCSV(result.Trades); err != nil {
		return nil, err
	}
	if artifacts["ledger.csv"], err = execution.LedgerCSV(result.Ledger); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func signalCSV(signals []domain.Signal) ([]byte, error) {
	return writeRecords([]string{"date", "strategy", "symbol", "signal"}, len(signals), func(index int) ([]string, error) {
		value, err := canonical.Decimal12(signals[index].Value)
		return []string{signals[index].Date.Format("2006-01-02"), signals[index].Strategy, signals[index].Symbol, value}, err
	})
}

func weightCSV(weights []domain.TargetWeight, column string) ([]byte, error) {
	return writeRecords([]string{"date", "strategy", "symbol", column}, len(weights), func(index int) ([]string, error) {
		value, err := canonical.Decimal12(weights[index].Weight)
		return []string{weights[index].Date.Format("2006-01-02"), weights[index].Strategy, weights[index].Symbol, value}, err
	})
}

func returnCSV(returns []ReturnRecord) ([]byte, error) {
	return writeRecords([]string{"date", "strategy", "gross_return", "net_return", "equity"}, len(returns), func(index int) ([]string, error) {
		gross, err := canonical.Decimal12(returns[index].GrossReturn)
		if err != nil {
			return nil, err
		}
		net, err := canonical.Decimal12(returns[index].NetReturn)
		if err != nil {
			return nil, err
		}
		equity, err := canonical.Decimal12(returns[index].Equity)
		return []string{returns[index].Date.Format("2006-01-02"), returns[index].Strategy, gross, net, equity}, err
	})
}

func orderCSV(orders []domain.Order) ([]byte, error) {
	header := []string{"date", "strategy", "symbol", "side", "target_weight", "reference_price", "requested_quantity", "executed_quantity", "scale_factor", "status", "skip_reason", "phase_order", "sequence"}
	return writeRecords(header, len(orders), func(index int) ([]string, error) {
		order := orders[index]
		values, err := decimals(order.TargetWeight, order.ReferencePrice, order.RequestedQuantity, order.ExecutedQuantity, order.ScaleFactor)
		if err != nil {
			return nil, err
		}
		return []string{order.Date.Format("2006-01-02"), order.Strategy, order.Symbol, string(order.Side), values[0], values[1], values[2], values[3], values[4], string(order.Status), order.SkipReason, strconv.Itoa(order.PhaseOrder), strconv.FormatUint(order.Sequence, 10)}, nil
	})
}

func tradeCSV(trades []domain.Trade) ([]byte, error) {
	header := []string{"date", "strategy", "symbol", "side", "quantity", "reference_price", "execution_price", "executed_notional", "commission", "slippage_cost", "phase_order", "sequence"}
	return writeRecords(header, len(trades), func(index int) ([]string, error) {
		trade := trades[index]
		values, err := decimals(trade.Quantity, trade.ReferencePrice, trade.ExecutionPrice, trade.ExecutedNotional, trade.Commission, trade.SlippageCost)
		if err != nil {
			return nil, err
		}
		return []string{trade.Date.Format("2006-01-02"), trade.Strategy, trade.Symbol, string(trade.Side), values[0], values[1], values[2], values[3], values[4], values[5], strconv.Itoa(trade.PhaseOrder), strconv.FormatUint(trade.Sequence, 10)}, nil
	})
}

func decimals(values ...float64) ([]string, error) {
	result := make([]string, len(values))
	for index, value := range values {
		formatted, err := canonical.Decimal12(value)
		if err != nil {
			return nil, err
		}
		result[index] = formatted
	}
	return result, nil
}

func writeRecords(header []string, count int, record func(int) ([]string, error)) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for index := 0; index < count; index++ {
		row, err := record(index)
		if err != nil {
			return nil, fmt.Errorf("format row %d: %w", index+2, err)
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
