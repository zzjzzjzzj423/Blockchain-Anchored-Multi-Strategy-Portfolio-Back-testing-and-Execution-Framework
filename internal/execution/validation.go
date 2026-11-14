package execution

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/domain"
)

// ValidationReport is the deterministic M2 acceptance summary.
type ValidationReport struct {
	Checks                 map[string]bool `json:"checks"`
	Days                   int             `json:"days"`
	Errors                 []string        `json:"errors"`
	GoldenLedgerSHA256     string          `json:"golden_ledger_sha256"`
	MaxAccountingResidual  string          `json:"max_accounting_residual"`
	MaxPnLIdentityResidual string          `json:"max_pnl_identity_residual"`
	Orders                 int             `json:"orders"`
	ResidualTolerance      string          `json:"residual_tolerance"`
	SchemaVersion          string          `json:"schema_version"`
	Status                 string          `json:"status"`
	Trades                 int             `json:"trades"`
}

// ValidateM2 runs fixed hand-worked scenarios without market data or strategies.
func ValidateM2() (ValidationReport, error) {
	forward, forwardResults, err := validationScenario(false)
	if err != nil {
		return ValidationReport{}, err
	}
	reversed, _, err := validationScenario(true)
	if err != nil {
		return ValidationReport{}, err
	}
	forwardCSV, err := LedgerCSV(forward)
	if err != nil {
		return ValidationReport{}, err
	}
	reversedCSV, err := LedgerCSV(reversed)
	if err != nil {
		return ValidationReport{}, err
	}

	maxAccounting := 0.0
	maxPnL := 0.0
	orders := 0
	trades := 0
	nonnegativeCash := true
	quantitiesAtEightDecimals := true
	var sequences []uint64
	for _, result := range forwardResults {
		maxAccounting = math.Max(maxAccounting, result.Ledger.AccountingResidual)
		maxPnL = math.Max(maxPnL, result.Ledger.PnLIdentityResidual)
		nonnegativeCash = nonnegativeCash && result.Ledger.Cash >= 0
		orders += len(result.Orders)
		trades += len(result.Trades)
		for _, order := range result.Orders {
			sequences = append(sequences, order.Sequence)
		}
		for _, trade := range result.Trades {
			sequences = append(sequences, trade.Sequence)
			quantized := math.RoundToEven(trade.Quantity*1e8) / 1e8
			quantitiesAtEightDecimals = quantitiesAtEightDecimals && math.Abs(trade.Quantity-quantized) < 1e-12
		}
		sequences = append(sequences, result.Ledger.Sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	sequencesIncreasing := len(sequences) > 0
	for index, sequence := range sequences {
		if sequence != uint64(index+1) {
			sequencesIncreasing = false
			break
		}
	}

	minimumSkipped, err := validateMinimumNotional()
	if err != nil {
		return ValidationReport{}, err
	}
	tradeOrderCorrect := len(forwardResults[3].Trades) == 2 &&
		forwardResults[3].Trades[0].Side == domain.SideSell &&
		forwardResults[3].Trades[1].Side == domain.SideBuy
	symbolOrderStable := len(forwardResults[1].Trades) == 2 &&
		forwardResults[1].Trades[0].Symbol == "AAA" &&
		forwardResults[1].Trades[1].Symbol == "BBB"
	uniformScale := len(forwardResults[1].Orders) == 2 &&
		forwardResults[1].Orders[0].ScaleFactor < 1 &&
		forwardResults[1].Orders[0].ScaleFactor == forwardResults[1].Orders[1].ScaleFactor
	liquidated := len(forwardResults[4].Positions) == 0

	checks := map[string]bool{
		"accounting_residual_below_1e-10": maxAccounting < 1e-10,
		"cash_never_negative":             nonnegativeCash,
		"input_order_invariant":           bytes.Equal(forwardCSV, reversedCSV),
		"minimum_notional_skipped":        minimumSkipped,
		"pnl_identity_below_1e-10":        maxPnL < 1e-10,
		"quantity_precision_8":            quantitiesAtEightDecimals,
		"sequence_strictly_increasing":    sequencesIncreasing,
		"sell_before_buy":                 tradeOrderCorrect,
		"symbol_order_stable":             symbolOrderStable,
		"uniform_buy_scale":               uniformScale,
		"zero_target_liquidates":          liquidated,
	}
	errors := make([]string, 0)
	checkNames := make([]string, 0, len(checks))
	for name := range checks {
		checkNames = append(checkNames, name)
	}
	sort.Strings(checkNames)
	for _, name := range checkNames {
		passed := checks[name]
		if !passed {
			errors = append(errors, name+" failed")
		}
	}
	status := "passed"
	if len(errors) != 0 {
		status = "failed"
	}
	maxAccountingText, _ := canonical.Decimal12(maxAccounting)
	maxPnLText, _ := canonical.Decimal12(maxPnL)
	report := ValidationReport{
		Checks:                 checks,
		Days:                   len(forward),
		Errors:                 errors,
		GoldenLedgerSHA256:     fmt.Sprintf("%x", sha256.Sum256(forwardCSV)),
		MaxAccountingResidual:  maxAccountingText,
		MaxPnLIdentityResidual: maxPnLText,
		Orders:                 orders,
		ResidualTolerance:      "0.000000000100",
		SchemaVersion:          "1.0.0",
		Status:                 status,
		Trades:                 trades,
	}
	if status != "passed" {
		return report, fmt.Errorf("M2 validation failed: %v", errors)
	}
	return report, nil
}

func validationScenario(reverse bool) ([]domain.LedgerEntry, []DayResult, error) {
	config := DefaultConfig("golden")
	config.InitialCash = 100_000
	engine, err := New(config)
	if err != nil {
		return nil, nil, err
	}
	inputs := []DayInput{
		validationInput(2024, 1, 1, false, []string{"AAA", "BBB"}, []float64{100, 50}, nil),
		validationInput(2024, 1, 2, true, []string{"AAA", "BBB"}, []float64{100, 50}, []float64{0.5, 0.5}),
		validationInput(2024, 1, 3, false, []string{"AAA", "BBB"}, []float64{110, 45}, nil),
		validationInput(2024, 1, 4, true, []string{"AAA", "BBB"}, []float64{105, 47}, []float64{0, 1}),
		validationInput(2024, 1, 5, true, []string{"AAA", "BBB"}, []float64{103, 49}, []float64{0, 0}),
	}
	results := make([]DayResult, 0, len(inputs))
	for _, input := range inputs {
		if reverse {
			for left, right := 0, len(input.Bars)-1; left < right; left, right = left+1, right-1 {
				input.Bars[left], input.Bars[right] = input.Bars[right], input.Bars[left]
			}
			for left, right := 0, len(input.TargetWeights)-1; left < right; left, right = left+1, right-1 {
				input.TargetWeights[left], input.TargetWeights[right] = input.TargetWeights[right], input.TargetWeights[left]
			}
		}
		result, err := engine.Step(input)
		if err != nil {
			return nil, nil, err
		}
		results = append(results, result)
	}
	return engine.Ledger(), results, nil
}

func validationInput(year int, month time.Month, day int, rebalance bool, symbols []string, prices, weights []float64) DayInput {
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	input := DayInput{Date: date, Rebalance: rebalance}
	for index, symbol := range symbols {
		price := prices[index]
		input.Bars = append(input.Bars, domain.MarketBar{Date: date, Symbol: symbol, Open: price, High: price, Low: price, Close: price})
		if rebalance && weights != nil {
			input.TargetWeights = append(input.TargetWeights, domain.TargetWeight{Strategy: "golden", Symbol: symbol, Weight: weights[index]})
		}
	}
	return input
}

func validateMinimumNotional() (bool, error) {
	config := DefaultConfig("golden")
	config.InitialCash = 1000
	engine, err := New(config)
	if err != nil {
		return false, err
	}
	input := validationInput(2024, 1, 1, true, []string{"AAA"}, []float64{100}, []float64{0.005})
	result, err := engine.Step(input)
	if err != nil {
		return false, err
	}
	return len(result.Orders) == 1 && result.Orders[0].Status == domain.OrderSkipped && result.Orders[0].SkipReason == "below_min_notional" && len(result.Trades) == 0, nil
}
