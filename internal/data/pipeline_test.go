package data

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMillisecondAndMicrosecondRecordsHaveSameUTCSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		month string
		unit  string
	}{
		{month: "2024-01", unit: "milliseconds"},
		{month: "2025-01", unit: "microseconds"},
	}
	for _, test := range tests {
		date := test.month + "-01"
		openTime, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatal(err)
		}
		closeTime := openTime.AddDate(0, 0, 1)
		var rawOpen, rawClose int64
		if test.unit == "milliseconds" {
			rawOpen = openTime.UnixMilli()
			rawClose = closeTime.Add(-time.Millisecond).UnixMilli()
		} else {
			rawOpen = openTime.UnixMicro()
			rawClose = closeTime.Add(-time.Microsecond).UnixMicro()
		}
		record := []string{
			integer(rawOpen), "100", "110", "90", "105", "42", integer(rawClose),
		}
		bar, _, _, err := parseBarRecord(record, "BTCUSDT", test.month)
		if err != nil {
			t.Fatalf("parse %s record: %v", test.unit, err)
		}
		if got := bar.Date.Format("15:04:05.999999999Z07:00"); got != "00:00:00Z" {
			t.Fatalf("%s open time = %s", test.unit, got)
		}
	}
}

func TestInvalidOHLCFails(t *testing.T) {
	t.Parallel()

	openTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	record := []string{
		integer(openTime.UnixMilli()), "100", "99", "90", "105", "42",
		integer(openTime.AddDate(0, 0, 1).Add(-time.Millisecond).UnixMilli()),
	}
	if _, _, _, err := parseBarRecord(record, "BTCUSDT", "2024-01"); err == nil || !strings.Contains(err.Error(), "OHLC") {
		t.Fatalf("invalid OHLC error = %v", err)
	}
}

func TestCoverageRejectsMissingAndDuplicateDates(t *testing.T) {
	t.Parallel()

	config := Config{StartMonth: "2024-01", EndMonth: "2024-01", Symbols: []string{"BTCUSDT"}}
	var complete []Bar
	for day := 1; day <= 31; day++ {
		complete = append(complete, validBar(time.Date(2024, 1, day, 0, 0, 0, 0, time.UTC)))
	}
	if _, _, err := validateCoverage(complete[:30], config); err == nil || !strings.Contains(err.Error(), "missing date") {
		t.Fatalf("missing date error = %v", err)
	}
	duplicated := append(append([]Bar(nil), complete...), complete[0])
	if _, _, err := validateCoverage(duplicated, config); err == nil || !strings.Contains(err.Error(), "duplicate date") {
		t.Fatalf("duplicate date error = %v", err)
	}
}

func TestNormalizedCSVIsByteStableAndSorted(t *testing.T) {
	t.Parallel()

	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	btc := validBar(date)
	btc.Symbol = "BTCUSDT"
	eth := validBar(date)
	eth.Symbol = "ETHUSDT"

	first, err := normalizedCSV([]Bar{eth, btc})
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizedCSV([]Bar{btc, eth})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("normalized output changed with input ordering")
	}
	lines := strings.Split(string(first), "\n")
	if !strings.HasPrefix(lines[1], "2024-01-01,BTCUSDT,") || !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("unexpected canonical CSV: %q", first)
	}
}

func TestChecksumMismatchFailsBeforeParsing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := Source{
		ArchiveName:          "BTCUSDT-1d-2024-01.zip",
		ArchiveRelativePath:  "data/raw/BTCUSDT-1d-2024-01.zip",
		ChecksumRelativePath: "data/raw/BTCUSDT-1d-2024-01.zip.CHECKSUM",
		CSVRelativePath:      "data/raw/BTCUSDT-1d-2024-01.csv",
	}
	if err := os.MkdirAll(filepath.Join(root, "data/raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, source.ArchiveRelativePath), []byte("not the expected archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksum := strings.Repeat("0", 64) + "  " + source.ArchiveName + "\n"
	if err := os.WriteFile(filepath.Join(root, source.ChecksumRelativePath), []byte(checksum), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSource(root, source); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
}

func validBar(date time.Time) Bar {
	return Bar{Date: date, Symbol: "BTCUSDT", Open: 100, High: 110, Low: 90, Close: 105, Volume: 42}
}

func integer(value int64) string {
	return strconv.FormatInt(value, 10)
}
