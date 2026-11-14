package experiment

import (
	"fmt"
	"math"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

const (
	RegimeBullLowVol  = "Bull/LowVol"
	RegimeBullHighVol = "Bull/HighVol"
	RegimeBearLowVol  = "Bear/LowVol"
	RegimeBearHighVol = "Bear/HighVol"
)

var RegimeNames = []string{RegimeBullLowVol, RegimeBullHighVol, RegimeBearLowVol, RegimeBearHighVol}

type RegimeLabel struct {
	Date     time.Time
	Name     string
	Trend126 float64
	Vol30    float64
}

type benchmarkPoint struct {
	date time.Time
	ret  float64
}

// DevelopmentVolatilityThreshold uses only lagged development observations.
func DevelopmentVolatilityThreshold(bars []domain.MarketBar) (float64, error) {
	points, err := benchmarkReturns(bars)
	if err != nil {
		return 0, err
	}
	values := make([]float64, 0, len(points))
	history := make([]float64, 0, len(points))
	for _, point := range points {
		if len(history) >= 30 {
			values = append(values, annualizedSampleVol(history[len(history)-30:]))
		}
		history = append(history, point.ret)
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("insufficient development history for volatility threshold")
	}
	sort.Float64s(values)
	return median(values), nil
}

// ClassifyRegimes labels dates using only benchmark returns through d-1.
func ClassifyRegimes(bars []domain.MarketBar, start, end time.Time, threshold float64) ([]RegimeLabel, error) {
	if !finite(threshold) || threshold < 0 || end.Before(start) {
		return nil, fmt.Errorf("invalid regime inputs")
	}
	points, err := benchmarkReturns(bars)
	if err != nil {
		return nil, err
	}
	history := make([]float64, 0, len(points))
	labels := make([]RegimeLabel, 0)
	for _, point := range points {
		if !point.date.Before(start) && !point.date.After(end) {
			if len(history) < 126 {
				return nil, fmt.Errorf("insufficient lagged benchmark history for %s", point.date.Format("2006-01-02"))
			}
			trend := compound(history[len(history)-126:]) - 1
			volatility := annualizedSampleVol(history[len(history)-30:])
			name := RegimeBearLowVol
			if trend >= 0 {
				name = RegimeBullLowVol
			}
			if volatility > threshold {
				if trend >= 0 {
					name = RegimeBullHighVol
				} else {
					name = RegimeBearHighVol
				}
			}
			labels = append(labels, RegimeLabel{Date: point.date, Name: name, Trend126: trend, Vol30: volatility})
		}
		history = append(history, point.ret)
	}
	want := int(end.Sub(start).Hours()/24) + 1
	if len(labels) != want {
		return nil, fmt.Errorf("classified %d dates, want %d", len(labels), want)
	}
	return labels, nil
}

func benchmarkReturns(bars []domain.MarketBar) ([]benchmarkPoint, error) {
	days, err := groupDays(bars)
	if err != nil {
		return nil, err
	}
	previous := make(map[string]float64)
	points := make([]benchmarkPoint, 0, len(days)-1)
	for _, current := range days {
		if len(current.bars) != 3 {
			return nil, fmt.Errorf("benchmark requires exactly three assets on %s", current.date.Format("2006-01-02"))
		}
		value := 0.0
		complete := len(previous) == 3
		for _, bar := range current.bars {
			prior, ok := previous[bar.Symbol]
			if complete && !ok {
				return nil, fmt.Errorf("benchmark symbol set changed")
			}
			if complete {
				value += (bar.Close/prior - 1) / 3
			}
			previous[bar.Symbol] = bar.Close
		}
		if complete {
			points = append(points, benchmarkPoint{date: current.date, ret: value})
		}
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("benchmark has no returns")
	}
	return points, nil
}

func annualizedSampleVol(values []float64) float64 {
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	sum := 0.0
	for _, value := range values {
		difference := value - mean
		sum += difference * difference
	}
	return math.Sqrt(sum/float64(len(values)-1)) * math.Sqrt(365)
}

func compound(values []float64) float64 {
	result := 1.0
	for _, value := range values {
		result *= 1 + value
	}
	return result
}
