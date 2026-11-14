package portfolio

import (
	"math"
	"testing"
)

func TestAllocateAllZeroScoresMeansCash(t *testing.T) {
	allocations, err := Allocate(map[string][]float64{
		"a": {0, 0, 0},
		"b": {-0.01, 0, -0.01},
	}, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, allocation := range allocations {
		if allocation.Weight != 0 {
			t.Fatalf("weight = %v, want cash", allocation.Weight)
		}
	}
}

func TestAllocateNormalizesPositiveScoresAndSorts(t *testing.T) {
	history := map[string][]float64{
		"z": {0.01, -0.005, 0.02, 0.01},
		"a": {0.02, 0.01, -0.001, 0.015},
	}
	allocations, err := Allocate(history, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	if allocations[0].Strategy != "a" || allocations[1].Strategy != "z" {
		t.Fatalf("allocation order = %#v", allocations)
	}
	total := allocations[0].Weight + allocations[1].Weight
	if math.Abs(total-1) > 1e-14 || allocations[0].Weight <= 0 || allocations[1].Weight <= 0 {
		t.Fatalf("weights = %#v", allocations)
	}
}

func TestAllocateRequiresFullLaggedWindows(t *testing.T) {
	allocations, err := Allocate(map[string][]float64{"a": {0.01, 0.02}}, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if allocations[0].Reliability != 0 || allocations[0].Score != 0 || allocations[0].Weight != 0 {
		t.Fatalf("warm-up allocation = %#v", allocations[0])
	}
}

func TestAllocateRejectsInvalidInput(t *testing.T) {
	if _, err := Allocate(nil, 3, 2); err == nil {
		t.Fatal("expected empty history error")
	}
	if _, err := Allocate(map[string][]float64{"": {0, 1}}, 3, 2); err == nil {
		t.Fatal("expected name error")
	}
	if _, err := Allocate(map[string][]float64{"a": {math.NaN()}}, 3, 2); err == nil {
		t.Fatal("expected finite return error")
	}
}
