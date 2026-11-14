package experiment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"msbd5017/portfolio-proof/internal/strategy"
	"msbd5017/portfolio-proof/strategies/buyhold"
	"msbd5017/portfolio-proof/strategies/meanreversion"
	"msbd5017/portfolio-proof/strategies/trend"
)

type strategyConfig struct {
	SchemaVersion    string   `json:"schema_version"`
	DevelopmentStart string   `json:"development_start"`
	DevelopmentEnd   string   `json:"development_end"`
	Symbols          []string `json:"symbols"`
	BuyAndHold       struct {
		RebalanceAfterInitial bool `json:"rebalance_after_initial"`
	} `json:"buy_and_hold"`
	Trend struct {
		FastWindow int `json:"fast_window"`
		SlowWindow int `json:"slow_window"`
	} `json:"trend"`
	MeanReversion struct {
		Lookback          int    `json:"lookback"`
		EntryZ            string `json:"entry_z"`
		ExitZ             string `json:"exit_z"`
		StandardDeviation string `json:"standard_deviation"`
	} `json:"mean_reversion"`
}

type M3Config struct {
	Start     time.Time
	End       time.Time
	Symbols   []string
	Factories []strategy.Factory
}

func LoadM3Config(root string) (M3Config, error) {
	content, err := os.ReadFile(filepath.Join(root, "configs", "strategies.json"))
	if err != nil {
		return M3Config{}, fmt.Errorf("read strategy config: %w", err)
	}
	var raw strategyConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return M3Config{}, fmt.Errorf("decode strategy config: %w", err)
	}
	start, err := time.ParseInLocation("2006-01-02", raw.DevelopmentStart, time.UTC)
	if err != nil {
		return M3Config{}, fmt.Errorf("development start: %w", err)
	}
	end, err := time.ParseInLocation("2006-01-02", raw.DevelopmentEnd, time.UTC)
	if err != nil || end.Before(start) {
		return M3Config{}, fmt.Errorf("invalid development end")
	}
	entry, err := strconv.ParseFloat(raw.MeanReversion.EntryZ, 64)
	if err != nil {
		return M3Config{}, fmt.Errorf("entry_z: %w", err)
	}
	exit, err := strconv.ParseFloat(raw.MeanReversion.ExitZ, 64)
	if err != nil {
		return M3Config{}, fmt.Errorf("exit_z: %w", err)
	}
	if raw.MeanReversion.StandardDeviation != "population" {
		return M3Config{}, fmt.Errorf("standard_deviation must be population")
	}
	if raw.SchemaVersion != "1.0.0" || raw.BuyAndHold.RebalanceAfterInitial {
		return M3Config{}, fmt.Errorf("unsupported strategy config schema or buy-and-hold policy")
	}
	if len(raw.Symbols) < 3 || !finite(entry) || !finite(exit) {
		return M3Config{}, fmt.Errorf("strategy config requires at least three symbols and finite thresholds")
	}
	trendStrategy, err := trend.New(raw.Trend.FastWindow, raw.Trend.SlowWindow)
	if err != nil {
		return M3Config{}, err
	}
	meanStrategy, err := meanreversion.New(raw.MeanReversion.Lookback, entry, exit)
	if err != nil {
		return M3Config{}, err
	}
	return M3Config{
		Start:   start,
		End:     end,
		Symbols: append([]string(nil), raw.Symbols...),
		Factories: []strategy.Factory{
			{Name: buyhold.Name, New: buyhold.New},
			{Name: trend.Name, New: func() strategy.Strategy { return trend.MustNew(trendStrategy.Fast, trendStrategy.Slow) }},
			{Name: meanreversion.Name, New: func() strategy.Strategy {
				return meanreversion.MustNew(meanStrategy.Lookback, meanStrategy.EntryZ, meanStrategy.ExitZ)
			}},
		},
	}, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
