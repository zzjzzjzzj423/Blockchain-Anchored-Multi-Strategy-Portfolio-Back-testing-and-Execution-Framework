package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	configRelativePath = "configs/data.json"
	rawRelativeRoot    = "data/raw/binance/spot/monthly/klines"
)

var frozenSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}

// Config is the frozen description of the Binance Vision snapshot.
type Config struct {
	AccessedOn    string   `json:"accessed_on"`
	BaseURL       string   `json:"base_url"`
	EndMonth      string   `json:"end_month"`
	Interval      string   `json:"interval"`
	Provider      string   `json:"provider"`
	SchemaVersion string   `json:"schema_version"`
	StartMonth    string   `json:"start_month"`
	Symbols       []string `json:"symbols"`
}

// Source identifies one monthly archive and its official checksum.
type Source struct {
	Symbol               string
	Month                string
	ArchiveName          string
	CSVName              string
	ArchiveRelativePath  string
	CSVRelativePath      string
	ChecksumRelativePath string
	ArchiveURL           string
	ChecksumURL          string
}

// LoadConfig reads the repository data configuration and rejects unknown fields.
func LoadConfig(root string) (Config, error) {
	path := filepath.Join(root, filepath.FromSlash(configRelativePath))
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open %s: %w", configRelativePath, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", configRelativePath, err)
	}
	if err := config.ValidateFrozen(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// ValidateFrozen prevents a config edit from silently changing SPEC 4.
func (c Config) ValidateFrozen() error {
	if c.SchemaVersion != "1.0.0" {
		return fmt.Errorf("data config schema_version must be 1.0.0")
	}
	if c.Provider != "Binance Vision" {
		return fmt.Errorf("data provider must be Binance Vision")
	}
	if c.BaseURL != "https://data.binance.vision/data/spot/monthly/klines" {
		return fmt.Errorf("unexpected Binance Vision base_url")
	}
	if c.Interval != "1d" || c.StartMonth != "2021-01" || c.EndMonth != "2025-12" {
		return fmt.Errorf("data interval and period must remain 1d, 2021-01 through 2025-12")
	}
	if c.AccessedOn != "2026-11-14" {
		return fmt.Errorf("accessed_on must identify the frozen snapshot date 2026-11-14")
	}
	if len(c.Symbols) != len(frozenSymbols) {
		return fmt.Errorf("symbols must be BTCUSDT, ETHUSDT, SOLUSDT")
	}
	symbols := append([]string(nil), c.Symbols...)
	sort.Strings(symbols)
	for index := range frozenSymbols {
		if symbols[index] != frozenSymbols[index] {
			return fmt.Errorf("symbols must be BTCUSDT, ETHUSDT, SOLUSDT")
		}
	}
	return nil
}

// Sources returns the 180 monthly inputs in stable symbol/month order.
func (c Config) Sources() ([]Source, error) {
	start, err := time.Parse("2006-01", c.StartMonth)
	if err != nil {
		return nil, fmt.Errorf("parse start_month: %w", err)
	}
	end, err := time.Parse("2006-01", c.EndMonth)
	if err != nil {
		return nil, fmt.Errorf("parse end_month: %w", err)
	}

	symbols := append([]string(nil), c.Symbols...)
	sort.Strings(symbols)
	var sources []Source
	for _, symbol := range symbols {
		for month := start; !month.After(end); month = month.AddDate(0, 1, 0) {
			monthText := month.Format("2006-01")
			archiveName := fmt.Sprintf("%s-%s-%s.zip", symbol, c.Interval, monthText)
			csvName := fmt.Sprintf("%s-%s-%s.csv", symbol, c.Interval, monthText)
			relativeDir := filepath.ToSlash(filepath.Join(rawRelativeRoot, symbol, c.Interval))
			archivePath := relativeDir + "/" + archiveName
			url := fmt.Sprintf("%s/%s/%s/%s", c.BaseURL, symbol, c.Interval, archiveName)
			sources = append(sources, Source{
				Symbol:               symbol,
				Month:                monthText,
				ArchiveName:          archiveName,
				CSVName:              csvName,
				ArchiveRelativePath:  archivePath,
				CSVRelativePath:      relativeDir + "/" + csvName,
				ChecksumRelativePath: archivePath + ".CHECKSUM",
				ArchiveURL:           url,
				ChecksumURL:          url + ".CHECKSUM",
			})
		}
	}
	return sources, nil
}

func (c Config) expectedRange() (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01", c.StartMonth)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endMonth, err := time.Parse("2006-01", c.EndMonth)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := endMonth.AddDate(0, 1, -1)
	return start.UTC(), end.UTC(), nil
}
