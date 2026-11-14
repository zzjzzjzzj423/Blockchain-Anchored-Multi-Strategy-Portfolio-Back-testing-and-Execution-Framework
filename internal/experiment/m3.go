package experiment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/strategies/buyhold"
)

type M3Report struct {
	ArtifactSHA256          map[string]string `json:"artifact_sha256"`
	BuyHoldTradeDates       int               `json:"buy_hold_trade_dates"`
	Days                    int               `json:"days"`
	DevelopmentEnd          string            `json:"development_end"`
	DevelopmentStart        string            `json:"development_start"`
	ExposureMaximum         string            `json:"exposure_maximum"`
	ExposureMinimum         string            `json:"exposure_minimum"`
	FutureDataExcluded      bool              `json:"future_data_excluded"`
	NoLookAhead             bool              `json:"no_look_ahead"`
	ParallelByteIdentical   bool              `json:"parallel_byte_identical"`
	SchemaVersion           string            `json:"schema_version"`
	SignalRows              int               `json:"signal_rows"`
	Status                  string            `json:"status"`
	Strategies              int               `json:"strategies"`
	TargetWeightRows        int               `json:"target_weight_rows"`
	WASMCompatibilityTarget string            `json:"wasm_compatibility_target"`
	WASMSHA256              map[string]string `json:"wasm_sha256"`
}

// ValidateM3 executes both modes over 2021-2024, checks prefix invariance,
// writes the parallel artifact set, and returns a deterministic report.
func ValidateM3(ctx context.Context, root string) (M3Report, error) {
	config, err := LoadM3Config(root)
	if err != nil {
		return M3Report{}, err
	}
	if config.Start != projectdata.DevelopmentStart || config.End != projectdata.DevelopmentEnd {
		return M3Report{}, fmt.Errorf("strategy and data development boundaries disagree")
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return M3Report{}, err
	}
	if err := validateDevelopmentCoverage(bars, config); err != nil {
		return M3Report{}, err
	}
	serial, err := RunAll(ctx, bars, config.Factories, false)
	if err != nil {
		return M3Report{}, err
	}
	parallel, err := RunAll(ctx, bars, config.Factories, true)
	if err != nil {
		return M3Report{}, err
	}
	serialArtifacts, err := ArtifactBytes(serial)
	if err != nil {
		return M3Report{}, err
	}
	parallelArtifacts, err := ArtifactBytes(parallel)
	if err != nil {
		return M3Report{}, err
	}
	identical := equalArtifacts(serialArtifacts, parallelArtifacts)
	if !identical {
		return M3Report{}, fmt.Errorf("serial and parallel artifacts differ")
	}

	cutoff := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
	noLookAhead, err := verifyPrefixInvariance(ctx, bars, cutoff, config)
	if err != nil || !noLookAhead {
		if err == nil {
			err = fmt.Errorf("prefix output changed after future mutation or truncation")
		}
		return M3Report{}, err
	}

	minimum, maximum := 1.0, 0.0
	buyHoldDates := make(map[string]struct{})
	for _, ledger := range parallel.Ledger {
		if ledger.Exposure < minimum {
			minimum = ledger.Exposure
		}
		if ledger.Exposure > maximum {
			maximum = ledger.Exposure
		}
		if ledger.Exposure < -1e-12 || ledger.Exposure > 1+1e-12 {
			return M3Report{}, fmt.Errorf("exposure outside [0,1]")
		}
	}
	for _, trade := range parallel.Trades {
		if trade.Strategy == buyhold.Name {
			buyHoldDates[trade.Date.Format("2006-01-02")] = struct{}{}
		}
	}
	if len(buyHoldDates) != 1 {
		return M3Report{}, fmt.Errorf("buy-and-hold traded on %d dates, want 1", len(buyHoldDates))
	}

	artifactDirectory := filepath.Join(root, "artifacts", "development", "base_strategies")
	if err := os.MkdirAll(artifactDirectory, 0o755); err != nil {
		return M3Report{}, err
	}
	hashes := make(map[string]string, len(parallelArtifacts))
	paths := make([]string, 0, len(parallelArtifacts))
	for path := range parallelArtifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content := parallelArtifacts[path]
		if err := os.WriteFile(filepath.Join(artifactDirectory, path), content, 0o644); err != nil {
			return M3Report{}, err
		}
		digest := sha256.Sum256(content)
		hashes[path] = hex.EncodeToString(digest[:])
	}
	wasmHashes, err := hashWASMModules(root)
	if err != nil {
		return M3Report{}, err
	}
	minText, _ := canonical.Decimal12(minimum)
	maxText, _ := canonical.Decimal12(maximum)
	return M3Report{
		ArtifactSHA256: hashes, BuyHoldTradeDates: len(buyHoldDates),
		Days: 1461, DevelopmentStart: config.Start.Format("2006-01-02"), DevelopmentEnd: config.End.Format("2006-01-02"),
		ExposureMinimum: minText, ExposureMaximum: maxText, FutureDataExcluded: true,
		NoLookAhead: true, ParallelByteIdentical: true, SchemaVersion: "1.0.0",
		SignalRows: len(parallel.Signals), Status: "passed", Strategies: len(config.Factories),
		TargetWeightRows: len(parallel.TargetWeights), WASMCompatibilityTarget: "portable WASI cores behind Argo GetRange adapter boundary",
		WASMSHA256: wasmHashes,
	}, nil
}

func hashWASMModules(root string) (map[string]string, error) {
	result := make(map[string]string, 3)
	for _, name := range []string{"buyhold", "meanreversion", "trend"} {
		path := filepath.Join(root, "bin", "strategies", name+".wasm")
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s WASM (run make wasm first): %w", name, err)
		}
		digest := sha256.Sum256(content)
		result[name+".wasm"] = hex.EncodeToString(digest[:])
	}
	return result, nil
}

func WriteM3Report(path string, report M3Report) error {
	content, err := canonical.JSON(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func validateDevelopmentCoverage(bars []domain.MarketBar, config M3Config) error {
	counts := make(map[string]int)
	for _, bar := range bars {
		if bar.Date.Before(config.Start) || bar.Date.After(config.End) {
			return fmt.Errorf("bar outside development period")
		}
		counts[bar.Symbol]++
	}
	for _, symbol := range config.Symbols {
		if counts[symbol] != 1461 {
			return fmt.Errorf("%s has %d development rows, want 1461", symbol, counts[symbol])
		}
		delete(counts, symbol)
	}
	if len(counts) != 0 {
		return fmt.Errorf("development data contains unexpected symbols")
	}
	return nil
}

func verifyPrefixInvariance(ctx context.Context, bars []domain.MarketBar, cutoff time.Time, config M3Config) (bool, error) {
	prefix := make([]domain.MarketBar, 0, len(bars))
	mutated := append([]domain.MarketBar(nil), bars...)
	for index := range mutated {
		if !mutated[index].Date.After(cutoff) {
			prefix = append(prefix, mutated[index])
			continue
		}
		factor := 1.25 + float64((index%7)+1)/100
		mutated[index].Open *= factor
		mutated[index].High *= factor
		mutated[index].Low *= factor
		mutated[index].Close *= factor
	}
	prefixRun, err := RunAll(ctx, prefix, config.Factories, false)
	if err != nil {
		return false, err
	}
	mutatedRun, err := RunAll(ctx, mutated, config.Factories, true)
	if err != nil {
		return false, err
	}
	mutatedPrefix := filterResult(mutatedRun, cutoff)
	left, err := ArtifactBytes(prefixRun)
	if err != nil {
		return false, err
	}
	right, err := ArtifactBytes(mutatedPrefix)
	if err != nil {
		return false, err
	}
	return equalArtifacts(left, right), nil
}

func filterResult(result Result, end time.Time) Result {
	filtered := Result{}
	for _, value := range result.Signals {
		if !value.Date.After(end) {
			filtered.Signals = append(filtered.Signals, value)
		}
	}
	for _, value := range result.TargetWeights {
		if !value.Date.After(end) {
			filtered.TargetWeights = append(filtered.TargetWeights, value)
		}
	}
	for _, value := range result.ExecutedWeights {
		if !value.Date.After(end) {
			filtered.ExecutedWeights = append(filtered.ExecutedWeights, value)
		}
	}
	for _, value := range result.Orders {
		if !value.Date.After(end) {
			filtered.Orders = append(filtered.Orders, value)
		}
	}
	for _, value := range result.Trades {
		if !value.Date.After(end) {
			filtered.Trades = append(filtered.Trades, value)
		}
	}
	for _, value := range result.Ledger {
		if !value.Date.After(end) {
			filtered.Ledger = append(filtered.Ledger, value)
		}
	}
	for _, value := range result.Returns {
		if !value.Date.After(end) {
			filtered.Returns = append(filtered.Returns, value)
		}
	}
	return filtered
}

func equalArtifacts(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for path, content := range left {
		if !bytes.Equal(content, right[path]) {
			return false
		}
	}
	return true
}
