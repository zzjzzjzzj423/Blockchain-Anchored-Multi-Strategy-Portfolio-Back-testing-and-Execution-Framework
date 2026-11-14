package execution

import (
	"bytes"
	"encoding/csv"
	"strconv"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/domain"
)

// LedgerCSV serializes ledger entries with fixed columns and LF endings.
func LedgerCSV(entries []domain.LedgerEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	header := []string{
		"date", "strategy", "pretrade_equity", "cash", "position_value",
		"gross_pnl", "commission", "slippage_cost", "net_pnl", "equity",
		"executed_notional", "turnover", "exposure", "accounting_residual",
		"pnl_identity_residual", "cumulative_commission", "cumulative_slippage_cost",
		"cash_normalized", "phase_order", "sequence",
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		values := []float64{
			entry.PretradeEquity, entry.Cash, entry.PositionValue, entry.GrossPnL,
			entry.Commission, entry.SlippageCost, entry.NetPnL, entry.Equity,
			entry.ExecutedNotional, entry.Turnover, entry.Exposure, entry.AccountingResidual,
			entry.PnLIdentityResidual, entry.CumulativeCommission, entry.CumulativeSlippageCost,
		}
		formatted := make([]string, len(values))
		for index, value := range values {
			text, err := canonical.Decimal12(value)
			if err != nil {
				return nil, err
			}
			formatted[index] = text
		}
		record := []string{
			entry.Date.Format("2006-01-02"), entry.Strategy,
			formatted[0], formatted[1], formatted[2], formatted[3], formatted[4],
			formatted[5], formatted[6], formatted[7], formatted[8], formatted[9],
			formatted[10], formatted[11], formatted[12], formatted[13], formatted[14],
			strconv.FormatBool(entry.CashNormalized), strconv.Itoa(entry.PhaseOrder), strconv.FormatUint(entry.Sequence, 10),
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
