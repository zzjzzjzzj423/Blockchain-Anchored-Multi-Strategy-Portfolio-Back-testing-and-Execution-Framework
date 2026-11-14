package experiment

import (
	"fmt"
	"math"
)

type SensitivityScenario struct {
	Name           string
	ChangedField   string
	Direction      string
	Candidate      Candidate
	CommissionRate float64
	SlippageRate   float64
}

// SensitivityScenarios implements the frozen one-factor-at-a-time protocol.
func SensitivityScenarios(selected Candidate) ([]SensitivityScenario, error) {
	baselineCommission, baselineSlippage := 0.0010, 0.0005
	result := []SensitivityScenario{{
		Name: "baseline", ChangedField: "none", Direction: "baseline", Candidate: selected,
		CommissionRate: baselineCommission, SlippageRate: baselineSlippage,
	}}
	integer := []struct {
		name string
		get  func(Candidate) int
		set  func(*Candidate, int)
	}{
		{"trend.fast_window", func(c Candidate) int { return c.Trend.FastWindow }, func(c *Candidate, v int) { c.Trend.FastWindow = v }},
		{"trend.slow_window", func(c Candidate) int { return c.Trend.SlowWindow }, func(c *Candidate, v int) { c.Trend.SlowWindow = v }},
		{"mean_reversion.lookback", func(c Candidate) int { return c.MeanReversion.Lookback }, func(c *Candidate, v int) { c.MeanReversion.Lookback = v }},
		{"allocator.reliability_window", func(c Candidate) int { return c.Allocator.ReliabilityWindow }, func(c *Candidate, v int) { c.Allocator.ReliabilityWindow = v }},
		{"allocator.volatility_window", func(c Candidate) int { return c.Allocator.VolatilityWindow }, func(c *Candidate, v int) { c.Allocator.VolatilityWindow = v }},
	}
	for _, parameter := range integer {
		for _, variation := range []struct {
			direction string
			factor    float64
		}{{"minus_20_pct", 0.8}, {"plus_20_pct", 1.2}} {
			candidate := selected
			value := int(math.RoundToEven(float64(parameter.get(selected)) * variation.factor))
			if value < 2 {
				value = 2
			}
			parameter.set(&candidate, value)
			result = append(result, SensitivityScenario{
				Name: parameter.name + "." + variation.direction, ChangedField: parameter.name,
				Direction: variation.direction, Candidate: candidate,
				CommissionRate: baselineCommission, SlippageRate: baselineSlippage,
			})
		}
	}
	for _, parameter := range []struct {
		name string
		get  func(Candidate) float64
		set  func(*Candidate, float64)
	}{
		{"mean_reversion.entry_z", func(c Candidate) float64 { return c.MeanReversion.EntryZ }, func(c *Candidate, v float64) { c.MeanReversion.EntryZ = v }},
		{"mean_reversion.exit_z", func(c Candidate) float64 { return c.MeanReversion.ExitZ }, func(c *Candidate, v float64) { c.MeanReversion.ExitZ = v }},
	} {
		if parameter.get(selected) == 0 {
			continue
		}
		for _, variation := range []struct {
			direction string
			factor    float64
		}{{"minus_20_pct", 0.8}, {"plus_20_pct", 1.2}} {
			candidate := selected
			parameter.set(&candidate, -math.Abs(parameter.get(selected))*variation.factor)
			result = append(result, SensitivityScenario{
				Name: parameter.name + "." + variation.direction, ChangedField: parameter.name,
				Direction: variation.direction, Candidate: candidate,
				CommissionRate: baselineCommission, SlippageRate: baselineSlippage,
			})
		}
	}
	for _, value := range []struct {
		name string
		rate float64
	}{{"minus_20_pct", 0.0008}, {"plus_20_pct", 0.0012}} {
		result = append(result, SensitivityScenario{Name: "commission." + value.name, ChangedField: "commission", Direction: value.name, Candidate: selected, CommissionRate: value.rate, SlippageRate: baselineSlippage})
	}
	for _, value := range []struct {
		name string
		rate float64
	}{{"minus_20_pct", 0.0004}, {"plus_20_pct", 0.0006}} {
		result = append(result, SensitivityScenario{Name: "slippage." + value.name, ChangedField: "slippage", Direction: value.name, Candidate: selected, CommissionRate: baselineCommission, SlippageRate: value.rate})
	}
	for _, scenario := range result {
		if scenario.Candidate.Trend.FastWindow >= scenario.Candidate.Trend.SlowWindow || scenario.Candidate.MeanReversion.EntryZ >= scenario.Candidate.MeanReversion.ExitZ {
			return nil, fmt.Errorf("sensitivity scenario %s violates strategy constraints", scenario.Name)
		}
	}
	return result, nil
}
