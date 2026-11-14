package data

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

// DevelopmentStart and DevelopmentEnd are the frozen in-sample boundaries.
// Data after DevelopmentEnd may exist in the pinned snapshot but must not enter M3.
var (
	DevelopmentStart  = time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	DevelopmentEnd    = time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	FinalHoldoutStart = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	FinalHoldoutEnd   = time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
)

// LoadDevelopment reads the normalized snapshot and returns only the frozen
// development interval. Rows from 2025 are deliberately excluded here, before
// any strategy receives market data.
func LoadDevelopment(root string) ([]domain.MarketBar, error) {
	return loadNormalizedInterval(root, DevelopmentStart, DevelopmentEnd)
}

// LoadFullSnapshot opens the final holdout. Callers must enforce the M5 gate
// before invoking it. Earlier rows remain available as strategy warm-up, while
// final metrics are computed only over FinalHoldoutStart..FinalHoldoutEnd.
func LoadFullSnapshot(root string) ([]domain.MarketBar, error) {
	return loadNormalizedInterval(root, DevelopmentStart, FinalHoldoutEnd)
}

func loadNormalizedInterval(root string, start, end time.Time) ([]domain.MarketBar, error) {
	path := filepath.Join(root, filepath.FromSlash(normalizedRelativePath))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open normalized data: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read normalized header: %w", err)
	}
	wantHeader := []string{"date", "symbol", "open", "high", "low", "close", "volume"}
	if len(header) != len(wantHeader) {
		return nil, fmt.Errorf("normalized header has %d columns, want %d", len(header), len(wantHeader))
	}
	for index := range wantHeader {
		if header[index] != wantHeader[index] {
			return nil, fmt.Errorf("normalized header column %d is %q, want %q", index, header[index], wantHeader[index])
		}
	}

	var bars []domain.MarketBar
	row := 1
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		row++
		if readErr != nil {
			return nil, fmt.Errorf("read normalized row %d: %w", row, readErr)
		}
		if len(record) != len(wantHeader) {
			return nil, fmt.Errorf("normalized row %d has %d columns", row, len(record))
		}
		date, err := time.ParseInLocation("2006-01-02", record[0], time.UTC)
		if err != nil {
			return nil, fmt.Errorf("normalized row %d date: %w", row, err)
		}
		if date.Before(start) || date.After(end) {
			continue
		}
		values := make([]float64, 5)
		for index, field := range record[2:] {
			value, err := strconv.ParseFloat(field, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("normalized row %d column %d is not finite", row, index+2)
			}
			values[index] = value
		}
		bars = append(bars, domain.MarketBar{
			Date: date, Symbol: record[1], Open: values[0], High: values[1],
			Low: values[2], Close: values[3], Volume: values[4],
		})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("normalized data contains no rows in requested interval")
	}
	return bars, nil
}
