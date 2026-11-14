package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDevelopmentExcludes2025(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "data", "normalized")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "date,symbol,open,high,low,close,volume\n" +
		"2024-12-31,BTCUSDT,1,1,1,1,1\n" +
		"2025-01-01,BTCUSDT,2,2,2,2,2\n"
	if err := os.WriteFile(filepath.Join(directory, "daily_ohlcv.csv"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	bars, err := LoadDevelopment(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || bars[0].Date.Format("2006-01-02") != "2024-12-31" {
		t.Fatalf("bars = %#v", bars)
	}
}
