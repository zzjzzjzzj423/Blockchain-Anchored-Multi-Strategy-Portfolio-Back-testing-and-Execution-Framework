package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
)

const (
	normalizedRelativePath = "data/normalized/daily_ohlcv.csv"
	manifestRelativePath   = "data/data_manifest.json"
	reportRelativePath     = "data/validation_report.json"
)

// Bar is one validated daily market observation.
type Bar struct {
	Date    time.Time
	Symbol  string
	Open    float64
	High    float64
	Low     float64
	Close   float64
	Volume  float64
	decimal [5]string
}

// FileManifest records both compressed and extracted identities for one month.
type FileManifest struct {
	ArchiveBytes         int64  `json:"archive_bytes"`
	ArchivePath          string `json:"archive_path"`
	ArchiveSHA256        string `json:"archive_sha256"`
	ChecksumDocumentPath string `json:"checksum_document_path"`
	ChecksumDocumentSHA  string `json:"checksum_document_sha256"`
	ChecksumURL          string `json:"checksum_url"`
	CSVBytes             int64  `json:"csv_bytes"`
	CSVPath              string `json:"csv_path"`
	CSVSHA256            string `json:"csv_sha256"`
	FirstDate            string `json:"first_date"`
	LastDate             string `json:"last_date"`
	MaxRawCloseTime      int64  `json:"max_raw_close_time"`
	MaxRawOpenTime       int64  `json:"max_raw_open_time"`
	MinRawCloseTime      int64  `json:"min_raw_close_time"`
	MinRawOpenTime       int64  `json:"min_raw_open_time"`
	Month                string `json:"month"`
	OfficialSHA256       string `json:"official_sha256"`
	RowCount             int    `json:"row_count"`
	SourceURL            string `json:"source_url"`
	Symbol               string `json:"symbol"`
	TimestampUnit        string `json:"timestamp_unit"`
}

type datasetManifest struct {
	AccessedOn string   `json:"accessed_on"`
	EndDate    string   `json:"end_date"`
	Frequency  string   `json:"frequency"`
	Provider   string   `json:"provider"`
	StartDate  string   `json:"start_date"`
	Symbols    []string `json:"symbols"`
	Timezone   string   `json:"timezone"`
}

type normalizedManifest struct {
	Bytes  int    `json:"bytes"`
	Path   string `json:"path"`
	Rows   int    `json:"rows"`
	SHA256 string `json:"sha256"`
}

// DataManifest is the deterministic identity of the fixed market data snapshot.
type DataManifest struct {
	Dataset       datasetManifest    `json:"dataset"`
	Files         []FileManifest     `json:"files"`
	Normalized    normalizedManifest `json:"normalized"`
	SchemaVersion string             `json:"schema_version"`
}

type validationChecks struct {
	AllChecksumsValid         bool `json:"all_checksums_valid"`
	DatesContinuous           bool `json:"dates_continuous"`
	DatesStrictlyIncreasing   bool `json:"dates_strictly_increasing"`
	FiniteValues              bool `json:"finite_values"`
	NoDuplicateDates          bool `json:"no_duplicate_dates"`
	OHLCValid                 bool `json:"ohlc_valid"`
	TimestampBoundaryExplicit bool `json:"timestamp_boundary_explicit"`
}

// ValidationReport is written on both successful and failed builds.
type ValidationReport struct {
	Checks                validationChecks  `json:"checks"`
	Errors                []string          `json:"errors"`
	ExpectedRowsPerSymbol int               `json:"expected_rows_per_symbol"`
	RowsBySymbol          map[string]int    `json:"rows_by_symbol"`
	SchemaVersion         string            `json:"schema_version"`
	Status                string            `json:"status"`
	TimestampRules        map[string]string `json:"timestamp_rules"`
	TotalRows             int               `json:"total_rows"`
}

// BuildResult reports deterministic output identities.
type BuildResult struct {
	Files            int
	NormalizedRows   int
	NormalizedSHA256 string
}

type parsedFile struct {
	bars     []Bar
	manifest FileManifest
}

// Build verifies every raw source, validates all bars, and writes canonical outputs.
func Build(ctx context.Context, root string, config Config) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}
	sources, err := config.Sources()
	if err != nil {
		return BuildResult{}, err
	}

	allBars := make([]Bar, 0, 5500)
	files := make([]FileManifest, 0, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return BuildResult{}, err
		}
		parsed, err := parseSource(root, source)
		if err != nil {
			writeFailureReport(root, "validation", err)
			return BuildResult{}, err
		}
		allBars = append(allBars, parsed.bars...)
		files = append(files, parsed.manifest)
	}

	rowsBySymbol, expectedRows, err := validateCoverage(allBars, config)
	if err != nil {
		writeFailureReport(root, "validation", err)
		return BuildResult{}, err
	}
	normalized, err := normalizedCSV(allBars)
	if err != nil {
		writeFailureReport(root, "normalization", err)
		return BuildResult{}, err
	}
	normalizedSHA := bytesSHA256(normalized)
	if err := writeAtomic(filepath.Join(root, filepath.FromSlash(normalizedRelativePath)), normalized); err != nil {
		return BuildResult{}, fmt.Errorf("write %s: %w", normalizedRelativePath, err)
	}

	start, end, _ := config.expectedRange()
	manifest := DataManifest{
		Dataset: datasetManifest{
			AccessedOn: config.AccessedOn,
			EndDate:    end.Format("2006-01-02"),
			Frequency:  config.Interval,
			Provider:   config.Provider,
			StartDate:  start.Format("2006-01-02"),
			Symbols:    sortedStrings(config.Symbols),
			Timezone:   "UTC",
		},
		Files: files,
		Normalized: normalizedManifest{
			Bytes:  len(normalized),
			Path:   normalizedRelativePath,
			Rows:   len(allBars),
			SHA256: normalizedSHA,
		},
		SchemaVersion: "1.0.0",
	}
	if err := writeCanonicalJSON(filepath.Join(root, filepath.FromSlash(manifestRelativePath)), manifest); err != nil {
		return BuildResult{}, err
	}

	report := ValidationReport{
		Checks: validationChecks{
			AllChecksumsValid:         true,
			DatesContinuous:           true,
			DatesStrictlyIncreasing:   true,
			FiniteValues:              true,
			NoDuplicateDates:          true,
			OHLCValid:                 true,
			TimestampBoundaryExplicit: true,
		},
		Errors:                []string{},
		ExpectedRowsPerSymbol: expectedRows,
		RowsBySymbol:          rowsBySymbol,
		SchemaVersion:         "1.0.0",
		Status:                "passed",
		TimestampRules: map[string]string{
			"2021-01_to_2024-12": "milliseconds",
			"2025-01_to_2025-12": "microseconds",
		},
		TotalRows: len(allBars),
	}
	if err := writeCanonicalJSON(filepath.Join(root, filepath.FromSlash(reportRelativePath)), report); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{Files: len(files), NormalizedRows: len(allBars), NormalizedSHA256: normalizedSHA}, nil
}

// WriteFailureReport records a stable failure without claiming successful checks.
func WriteFailureReport(root, stage string, err error) {
	writeFailureReport(root, stage, err)
}

func writeFailureReport(root, stage string, buildErr error) {
	report := map[string]any{
		"errors":         []string{buildErr.Error()},
		"schema_version": "1.0.0",
		"stage":          stage,
		"status":         "failed",
	}
	_ = writeCanonicalJSON(filepath.Join(root, filepath.FromSlash(reportRelativePath)), report)
}

func parseSource(root string, source Source) (parsedFile, error) {
	checksumPath := filepath.Join(root, filepath.FromSlash(source.ChecksumRelativePath))
	archivePath := filepath.Join(root, filepath.FromSlash(source.ArchiveRelativePath))
	csvPath := filepath.Join(root, filepath.FromSlash(source.CSVRelativePath))

	official, err := readOfficialChecksum(checksumPath, source.ArchiveName)
	if err != nil {
		return parsedFile{}, fmt.Errorf("parse %s: %w", source.ChecksumRelativePath, err)
	}
	archiveSHA, err := fileSHA256(archivePath)
	if err != nil {
		return parsedFile{}, fmt.Errorf("hash %s: %w", source.ArchiveRelativePath, err)
	}
	if archiveSHA != official {
		return parsedFile{}, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", source.ArchiveName, official, archiveSHA)
	}
	checksumSHA, err := fileSHA256(checksumPath)
	if err != nil {
		return parsedFile{}, fmt.Errorf("hash %s: %w", source.ChecksumRelativePath, err)
	}
	csvSHA, err := fileSHA256(csvPath)
	if err != nil {
		return parsedFile{}, fmt.Errorf("hash %s: %w", source.CSVRelativePath, err)
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return parsedFile{}, err
	}
	csvInfo, err := os.Stat(csvPath)
	if err != nil {
		return parsedFile{}, err
	}

	bars, timestamps, err := parseBars(csvPath, source)
	if err != nil {
		return parsedFile{}, fmt.Errorf("validate %s: %w", source.CSVRelativePath, err)
	}
	manifest := FileManifest{
		ArchiveBytes:         archiveInfo.Size(),
		ArchivePath:          source.ArchiveRelativePath,
		ArchiveSHA256:        archiveSHA,
		ChecksumDocumentPath: source.ChecksumRelativePath,
		ChecksumDocumentSHA:  checksumSHA,
		ChecksumURL:          source.ChecksumURL,
		CSVBytes:             csvInfo.Size(),
		CSVPath:              source.CSVRelativePath,
		CSVSHA256:            csvSHA,
		FirstDate:            bars[0].Date.Format("2006-01-02"),
		LastDate:             bars[len(bars)-1].Date.Format("2006-01-02"),
		MaxRawCloseTime:      timestamps.maxClose,
		MaxRawOpenTime:       timestamps.maxOpen,
		MinRawCloseTime:      timestamps.minClose,
		MinRawOpenTime:       timestamps.minOpen,
		Month:                source.Month,
		OfficialSHA256:       official,
		RowCount:             len(bars),
		SourceURL:            source.ArchiveURL,
		Symbol:               source.Symbol,
		TimestampUnit:        timestampUnit(source.Month),
	}
	return parsedFile{bars: bars, manifest: manifest}, nil
}

type timestampSummary struct {
	minOpen  int64
	maxOpen  int64
	minClose int64
	maxClose int64
}

func parseBars(path string, source Source) ([]Bar, timestampSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, timestampSummary{}, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false

	monthStart, err := time.Parse("2006-01", source.Month)
	if err != nil {
		return nil, timestampSummary{}, err
	}
	expectedDays := monthStart.AddDate(0, 1, -1).Day()
	bars := make([]Bar, 0, expectedDays)
	summary := timestampSummary{}
	rowNumber := 0
	for {
		record, readErr := reader.Read()
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, timestampSummary{}, fmt.Errorf("read CSV row %d: %w", rowNumber+1, readErr)
		}
		rowNumber++
		if rowNumber == 1 && isHeader(record) {
			continue
		}
		bar, rawOpen, rawClose, err := parseBarRecord(record, source.Symbol, source.Month)
		if err != nil {
			return nil, timestampSummary{}, fmt.Errorf("row %d: %w", rowNumber, err)
		}
		if len(bars) > 0 && !bar.Date.After(bars[len(bars)-1].Date) {
			return nil, timestampSummary{}, fmt.Errorf("dates are duplicate or not strictly increasing at %s", bar.Date.Format("2006-01-02"))
		}
		bars = append(bars, bar)
		if len(bars) == 1 {
			summary = timestampSummary{minOpen: rawOpen, maxOpen: rawOpen, minClose: rawClose, maxClose: rawClose}
		} else {
			summary.maxOpen = rawOpen
			summary.maxClose = rawClose
		}
	}
	if len(bars) != expectedDays {
		return nil, timestampSummary{}, fmt.Errorf("month %s has %d rows, expected %d", source.Month, len(bars), expectedDays)
	}
	if bars[0].Date.Day() != 1 || bars[len(bars)-1].Date.Day() != expectedDays {
		return nil, timestampSummary{}, fmt.Errorf("month %s does not cover every UTC date", source.Month)
	}
	return bars, summary, nil
}

func parseBarRecord(record []string, symbol, month string) (Bar, int64, int64, error) {
	if len(record) < 7 {
		return Bar{}, 0, 0, fmt.Errorf("has %d fields, expected at least 7", len(record))
	}
	for index := 0; index < 7; index++ {
		if strings.TrimSpace(record[index]) == "" {
			return Bar{}, 0, 0, fmt.Errorf("field %d is empty", index)
		}
	}
	rawOpen, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return Bar{}, 0, 0, fmt.Errorf("invalid open time: %w", err)
	}
	rawClose, err := strconv.ParseInt(record[6], 10, 64)
	if err != nil {
		return Bar{}, 0, 0, fmt.Errorf("invalid close time: %w", err)
	}
	unit := timestampUnit(month)
	openTime := timestampToTime(rawOpen, unit)
	closeTime := timestampToTime(rawClose, unit)
	if openTime.Location() != time.UTC {
		openTime = openTime.UTC()
	}
	if openTime.Format("2006-01") != month || openTime.Hour() != 0 || openTime.Minute() != 0 || openTime.Second() != 0 || openTime.Nanosecond() != 0 {
		return Bar{}, 0, 0, fmt.Errorf("open timestamp %d (%s) is not UTC midnight in %s", rawOpen, unit, month)
	}
	expectedClose := openTime.AddDate(0, 0, 1)
	if unit == "milliseconds" {
		expectedClose = expectedClose.Add(-time.Millisecond)
	} else {
		expectedClose = expectedClose.Add(-time.Microsecond)
	}
	if !closeTime.Equal(expectedClose) {
		return Bar{}, 0, 0, fmt.Errorf("close timestamp %d (%s) does not end the UTC candle", rawClose, unit)
	}

	values := make([]float64, 5)
	decimalValues := [5]string{}
	for index, field := range record[1:6] {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return Bar{}, 0, 0, fmt.Errorf("field %d is not finite", index+1)
		}
		values[index] = value
		decimalValues[index], err = canonical.DecimalString12(field)
		if err != nil {
			return Bar{}, 0, 0, fmt.Errorf("field %d is not a valid decimal: %w", index+1, err)
		}
	}
	bar := Bar{Date: openTime, Symbol: symbol, Open: values[0], High: values[1], Low: values[2], Close: values[3], Volume: values[4], decimal: decimalValues}
	if bar.Open <= 0 || bar.High <= 0 || bar.Low <= 0 || bar.Close <= 0 {
		return Bar{}, 0, 0, fmt.Errorf("OHLC values must be positive")
	}
	if bar.Volume < 0 {
		return Bar{}, 0, 0, fmt.Errorf("volume must be non-negative")
	}
	if !(bar.Low <= math.Min(bar.Open, bar.Close) && math.Max(bar.Open, bar.Close) <= bar.High) {
		return Bar{}, 0, 0, fmt.Errorf("invalid OHLC relationship")
	}
	return bar, rawOpen, rawClose, nil
}

func validateCoverage(bars []Bar, config Config) (map[string]int, int, error) {
	start, end, err := config.expectedRange()
	if err != nil {
		return nil, 0, err
	}
	expectedRows := int(end.Sub(start).Hours()/24) + 1
	bySymbol := make(map[string]map[string]struct{}, len(config.Symbols))
	for _, symbol := range config.Symbols {
		bySymbol[symbol] = make(map[string]struct{}, expectedRows)
	}
	for _, bar := range bars {
		dates, ok := bySymbol[bar.Symbol]
		if !ok {
			return nil, 0, fmt.Errorf("unexpected symbol %s", bar.Symbol)
		}
		date := bar.Date.Format("2006-01-02")
		if _, exists := dates[date]; exists {
			return nil, 0, fmt.Errorf("duplicate date %s for %s", date, bar.Symbol)
		}
		dates[date] = struct{}{}
	}
	counts := make(map[string]int, len(bySymbol))
	for _, symbol := range sortedStrings(config.Symbols) {
		dates := bySymbol[symbol]
		for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
			dateText := date.Format("2006-01-02")
			if _, ok := dates[dateText]; !ok {
				return nil, 0, fmt.Errorf("missing date %s for %s", dateText, symbol)
			}
		}
		if len(dates) != expectedRows {
			return nil, 0, fmt.Errorf("%s has %d dates, expected %d", symbol, len(dates), expectedRows)
		}
		counts[symbol] = len(dates)
	}
	return counts, expectedRows, nil
}

func normalizedCSV(bars []Bar) ([]byte, error) {
	sorted := append([]Bar(nil), bars...)
	sort.Slice(sorted, func(i, j int) bool {
		leftDate := sorted[i].Date.Format("2006-01-02")
		rightDate := sorted[j].Date.Format("2006-01-02")
		if leftDate != rightDate {
			return leftDate < rightDate
		}
		return sorted[i].Symbol < sorted[j].Symbol
	})

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"date", "symbol", "open", "high", "low", "close", "volume"}); err != nil {
		return nil, err
	}
	for _, bar := range sorted {
		values := make([]string, 5)
		for index, value := range []float64{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume} {
			if bar.decimal[index] != "" {
				values[index] = bar.decimal[index]
			} else {
				formatted, err := canonical.Decimal12(value)
				if err != nil {
					return nil, err
				}
				values[index] = formatted
			}
		}
		record := []string{bar.Date.Format("2006-01-02"), bar.Symbol, values[0], values[1], values[2], values[3], values[4]}
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

func timestampUnit(month string) string {
	if month < "2025-01" {
		return "milliseconds"
	}
	return "microseconds"
}

func timestampToTime(value int64, unit string) time.Time {
	if unit == "milliseconds" {
		return time.UnixMilli(value).UTC()
	}
	return time.UnixMicro(value).UTC()
}

func isHeader(record []string) bool {
	if len(record) == 0 {
		return false
	}
	first := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(record[0]), " ", "_"))
	return first == "open_time" || first == "opentime"
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func bytesSHA256(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func writeCanonicalJSON(path string, value any) error {
	content, err := canonical.JSON(value)
	if err != nil {
		return err
	}
	if err := writeAtomic(path, content); err != nil {
		return fmt.Errorf("write %s: %w", filepath.ToSlash(path), err)
	}
	return nil
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
