package metrics

import (
	"fmt"
	"math"
)

const Annualization = 365.0

// Observation is one evaluation-period day. Returns must use the equity from
// the preceding close as denominator.
type Observation struct {
	NetReturn          float64
	GrossReturn        float64
	Turnover           float64
	Exposure           float64
	Commission         float64
	Slippage           float64
	AccountingResidual float64
}

// Summary implements every metric in SPEC section 12. Ratio pointers are nil
// when their denominators are undefined; the corresponding reason is explicit.
type Summary struct {
	Days                      int
	TotalReturn               float64
	AnnualizedReturn          float64
	AnnualizedVolatility      *float64
	VolatilityNullReason      string
	Sharpe                    *float64
	SharpeNullReason          string
	Sortino                   *float64
	SortinoNullReason         string
	MaximumDrawdown           float64
	AnnualizedTurnover        float64
	TotalCostRatio            float64
	AnnualizedCostDrag        float64
	AverageExposure           float64
	MaximumExposure           float64
	CashRatio                 float64
	MaximumAccountingResidual float64
}

func Compute(observations []Observation, initialEquity float64) (Summary, error) {
	if len(observations) == 0 {
		return Summary{}, fmt.Errorf("at least one observation is required")
	}
	if !finite(initialEquity) || initialEquity <= 0 {
		return Summary{}, fmt.Errorf("initial equity must be finite and positive")
	}
	netReturns := make([]float64, len(observations))
	grossReturns := make([]float64, len(observations))
	netGrowth, grossGrowth := 1.0, 1.0
	peak, equity, maxDrawdown := 1.0, 1.0, 0.0
	totalTurnover, totalCosts, totalExposure, maximumExposure, maximumResidual := 0.0, 0.0, 0.0, 0.0, 0.0
	for index, observation := range observations {
		values := []float64{observation.NetReturn, observation.GrossReturn, observation.Turnover, observation.Exposure, observation.Commission, observation.Slippage, observation.AccountingResidual}
		for _, value := range values {
			if !finite(value) {
				return Summary{}, fmt.Errorf("observation %d contains a non-finite value", index)
			}
		}
		if observation.Turnover < 0 || observation.Exposure < 0 || observation.Exposure > 1+1e-10 || observation.Commission < 0 || observation.Slippage < 0 || observation.AccountingResidual < 0 {
			return Summary{}, fmt.Errorf("observation %d violates metric bounds", index)
		}
		netReturns[index] = observation.NetReturn
		grossReturns[index] = observation.GrossReturn
		netGrowth *= 1 + observation.NetReturn
		grossGrowth *= 1 + observation.GrossReturn
		if netGrowth <= 0 || grossGrowth <= 0 {
			return Summary{}, fmt.Errorf("observation %d produces non-positive compounded equity", index)
		}
		equity *= 1 + observation.NetReturn
		if equity > peak {
			peak = equity
		}
		drawdown := 1 - equity/peak
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
		totalTurnover += observation.Turnover
		totalCosts += observation.Commission + observation.Slippage
		totalExposure += observation.Exposure
		if observation.Exposure > maximumExposure {
			maximumExposure = observation.Exposure
		}
		if observation.AccountingResidual > maximumResidual {
			maximumResidual = observation.AccountingResidual
		}
	}

	days := float64(len(observations))
	annualizedReturn := math.Pow(netGrowth, Annualization/days) - 1
	grossAnnualizedReturn := math.Pow(grossGrowth, Annualization/days) - 1
	averageExposure := totalExposure / days
	summary := Summary{
		Days: len(observations), TotalReturn: netGrowth - 1,
		AnnualizedReturn: annualizedReturn, MaximumDrawdown: maxDrawdown,
		AnnualizedTurnover: totalTurnover / days * Annualization,
		TotalCostRatio:     totalCosts / initialEquity,
		AnnualizedCostDrag: grossAnnualizedReturn - annualizedReturn,
		AverageExposure:    averageExposure, MaximumExposure: maximumExposure,
		CashRatio: 1 - averageExposure, MaximumAccountingResidual: maximumResidual,
	}
	mean := average(netReturns)
	standardDeviation, ok := sampleStandardDeviation(netReturns)
	if !ok {
		summary.VolatilityNullReason = "fewer than two observations"
		summary.SharpeNullReason = "fewer than two observations"
	} else {
		volatility := standardDeviation * math.Sqrt(Annualization)
		summary.AnnualizedVolatility = &volatility
		if standardDeviation == 0 {
			summary.SharpeNullReason = "return standard deviation is zero"
		} else {
			sharpe := mean / standardDeviation * math.Sqrt(Annualization)
			summary.Sharpe = &sharpe
		}
	}
	downsideSum := 0.0
	for _, value := range netReturns {
		if value < 0 {
			downsideSum += value * value
		}
	}
	downsideDeviation := math.Sqrt(downsideSum / days)
	if downsideDeviation == 0 {
		summary.SortinoNullReason = "downside deviation is zero"
	} else {
		sortino := mean / downsideDeviation * math.Sqrt(Annualization)
		summary.Sortino = &sortino
	}
	return summary, nil
}

func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func sampleStandardDeviation(values []float64) (float64, bool) {
	if len(values) < 2 {
		return 0, false
	}
	mean := average(values)
	total := 0.0
	for _, value := range values {
		difference := value - mean
		total += difference * difference
	}
	return math.Sqrt(total / float64(len(values)-1)), true
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
