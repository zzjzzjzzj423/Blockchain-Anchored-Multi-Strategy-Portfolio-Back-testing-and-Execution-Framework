package data

import "testing"

func TestFrozenSources(t *testing.T) {
	t.Parallel()

	config := frozenTestConfig()
	if err := config.ValidateFrozen(); err != nil {
		t.Fatal(err)
	}
	sources, err := config.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 180 {
		t.Fatalf("Sources() returned %d inputs, want 180", len(sources))
	}
	if got := sources[0].ArchiveURL; got != "https://data.binance.vision/data/spot/monthly/klines/BTCUSDT/1d/BTCUSDT-1d-2021-01.zip" {
		t.Fatalf("first URL = %q", got)
	}
	if got := sources[len(sources)-1].ArchiveURL; got != "https://data.binance.vision/data/spot/monthly/klines/SOLUSDT/1d/SOLUSDT-1d-2025-12.zip" {
		t.Fatalf("last URL = %q", got)
	}
}

func frozenTestConfig() Config {
	return Config{
		AccessedOn:    "2026-11-14",
		BaseURL:       "https://data.binance.vision/data/spot/monthly/klines",
		EndMonth:      "2025-12",
		Interval:      "1d",
		Provider:      "Binance Vision",
		SchemaVersion: "1.0.0",
		StartMonth:    "2021-01",
		Symbols:       []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
	}
}
