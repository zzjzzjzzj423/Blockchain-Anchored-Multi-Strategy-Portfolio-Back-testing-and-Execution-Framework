package portfolio

import (
	"fmt"
	"math"
	"sort"
)

const annualization = 365.0

type Allocation struct {
	Strategy           string
	Reliability        float64
	RealizedVolatility float64
	Score              float64
	Weight             float64
}

// Allocate applies the frozen reliability/inverse-volatility rule. Callers
// must pass history ending at d-1; a full window is required for each statistic.
func Allocate(history map[string][]float64, reliabilityWindow, volatilityWindow int) ([]Allocation, error) {
	if reliabilityWindow < 2 || volatilityWindow < 2 || len(history) == 0 {
		return nil, fmt.Errorf("allocator requires non-empty history and windows >= 2")
	}
	names := make([]string, 0, len(history))
	for name := range history {
		if name == "" {
			return nil, fmt.Errorf("strategy name must not be empty")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Allocation, 0, len(names))
	totalScore := 0.0
	for _, name := range names {
		values := history[name]
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("%s history contains a non-finite return", name)
			}
		}
		reliability := 0.0
		if len(values) >= reliabilityWindow {
			window := values[len(values)-reliabilityWindow:]
			standardDeviation := sampleStandardDeviation(window)
			if standardDeviation > 0 {
				reliability = average(window) / standardDeviation * math.Sqrt(annualization)
			}
		}
		volatility := 0.0
		if len(values) >= volatilityWindow {
			window := values[len(values)-volatilityWindow:]
			volatility = sampleStandardDeviation(window) * math.Sqrt(annualization)
		}
		score := 0.0
		if reliability > 0 && len(values) >= volatilityWindow {
			score = reliability / math.Max(volatility, 1e-12)
		}
		result = append(result, Allocation{Strategy: name, Reliability: reliability, RealizedVolatility: volatility, Score: score})
		totalScore += score
	}
	if totalScore > 0 {
		for index := range result {
			result[index].Weight = result[index].Score / totalScore
		}
	}
	return result, nil
}

func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func sampleStandardDeviation(values []float64) float64 {
	mean := average(values)
	total := 0.0
	for _, value := range values {
		difference := value - mean
		total += difference * difference
	}
	return math.Sqrt(total / float64(len(values)-1))
}
