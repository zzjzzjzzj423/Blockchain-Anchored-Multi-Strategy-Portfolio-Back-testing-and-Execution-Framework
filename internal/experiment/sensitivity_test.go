package experiment

import "testing"

func TestSensitivityScenariosAreFrozenOFAT(t *testing.T) {
	selected := Candidate{
		Trend:         TrendParameters{FastWindow: 10, SlowWindow: 150},
		MeanReversion: MeanReversionParameters{Lookback: 10, EntryZ: -0.5, ExitZ: -0.25},
		Allocator:     AllocatorParameters{ReliabilityWindow: 30, VolatilityWindow: 30},
	}
	scenarios, err := SensitivityScenarios(selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 19 || scenarios[0].Name != "baseline" {
		t.Fatalf("scenarios = %d, first = %s", len(scenarios), scenarios[0].Name)
	}
	byName := make(map[string]SensitivityScenario)
	for _, scenario := range scenarios {
		if _, exists := byName[scenario.Name]; exists {
			t.Fatalf("duplicate scenario %s", scenario.Name)
		}
		byName[scenario.Name] = scenario
	}
	if got := byName["trend.slow_window.plus_20_pct"].Candidate.Trend.SlowWindow; got != 180 {
		t.Fatalf("slow window = %d", got)
	}
	if got := byName["mean_reversion.entry_z.minus_20_pct"].Candidate.MeanReversion.EntryZ; got != -0.4 {
		t.Fatalf("entry z = %v", got)
	}
	if got := byName["commission.minus_20_pct"].CommissionRate; got != 0.0008 {
		t.Fatalf("commission = %v", got)
	}
}
