package metrics

import (
	"math"
	"testing"
)

func TestComputeKnownSeries(t *testing.T) {
	observations := []Observation{
		{NetReturn: 0.10, GrossReturn: 0.11, Turnover: 0.2, Exposure: 0.8, Commission: 1, Slippage: 0.5, AccountingResidual: 1e-12},
		{NetReturn: -0.05, GrossReturn: -0.04, Turnover: 0.1, Exposure: 0.6, Commission: 1, Slippage: 0.5, AccountingResidual: 2e-12},
		{NetReturn: 0.02, GrossReturn: 0.03, Turnover: 0.0, Exposure: 0.7, Commission: 0, Slippage: 0, AccountingResidual: 0},
	}
	got, err := Compute(observations, 1000)
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := 1.1*0.95*1.02 - 1
	if math.Abs(got.TotalReturn-wantTotal) > 1e-14 {
		t.Fatalf("total return = %v, want %v", got.TotalReturn, wantTotal)
	}
	if got.Sharpe == nil || got.Sortino == nil || got.AnnualizedVolatility == nil {
		t.Fatal("expected defined ratios")
	}
	if math.Abs(got.MaximumDrawdown-0.05) > 1e-14 {
		t.Fatalf("maximum drawdown = %v", got.MaximumDrawdown)
	}
	if math.Abs(got.AnnualizedTurnover-36.5) > 1e-12 || math.Abs(got.TotalCostRatio-0.003) > 1e-14 {
		t.Fatalf("turnover/cost = %v/%v", got.AnnualizedTurnover, got.TotalCostRatio)
	}
	if math.Abs(got.AverageExposure-0.7) > 1e-14 || got.MaximumExposure != 0.8 {
		t.Fatalf("exposure = %v/%v", got.AverageExposure, got.MaximumExposure)
	}
}

func TestUndefinedRatiosHaveReasons(t *testing.T) {
	one, err := Compute([]Observation{{NetReturn: 0, GrossReturn: 0}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if one.Sharpe != nil || one.AnnualizedVolatility != nil || one.VolatilityNullReason == "" || one.SortinoNullReason == "" {
		t.Fatalf("unexpected one-day metrics: %#v", one)
	}
	flat, err := Compute([]Observation{{NetReturn: 0, GrossReturn: 0}, {NetReturn: 0, GrossReturn: 0}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if flat.Sharpe != nil || flat.SharpeNullReason == "" || flat.AnnualizedVolatility == nil || *flat.AnnualizedVolatility != 0 {
		t.Fatalf("unexpected flat metrics: %#v", flat)
	}
}

func TestRejectsInvalidInputs(t *testing.T) {
	if _, err := Compute(nil, 100); err == nil {
		t.Fatal("expected empty observation error")
	}
	if _, err := Compute([]Observation{{}}, 0); err == nil {
		t.Fatal("expected initial equity error")
	}
	if _, err := Compute([]Observation{{NetReturn: math.NaN()}}, 100); err == nil {
		t.Fatal("expected finite value error")
	}
	if _, err := Compute([]Observation{{Exposure: 2}}, 100); err == nil {
		t.Fatal("expected bound error")
	}
	if _, err := Compute([]Observation{{NetReturn: -2, GrossReturn: -2}}, 100); err == nil {
		t.Fatal("expected compounded equity error")
	}
}
