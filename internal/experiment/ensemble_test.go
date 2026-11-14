package experiment

import (
	"context"
	"testing"

	strategyapi "msbd5017/portfolio-proof/internal/strategy"
)

func TestEnsembleAllocationUsesOnlyPriorReturns(t *testing.T) {
	bars := syntheticBars(12)
	base, err := RunAll(context.Background(), bars, testFactories(), false)
	if err != nil {
		t.Fatal(err)
	}
	parameters := AllocatorParameters{ReliabilityWindow: 3, VolatilityWindow: 2}
	first, err := RunEnsemble(context.Background(), bars, base, parameters)
	if err != nil {
		t.Fatal(err)
	}
	mutated := base
	mutated.Returns = append([]ReturnRecord(nil), base.Returns...)
	date := bars[3*6].Date
	for index := range mutated.Returns {
		if mutated.Returns[index].Date.Equal(date) {
			mutated.Returns[index].NetReturn += 10
		}
	}
	second, err := RunEnsemble(context.Background(), bars, mutated, parameters)
	if err != nil {
		t.Fatal(err)
	}
	for index := range first.Allocations {
		if first.Allocations[index].Date.Equal(date) && first.Allocations[index] != second.Allocations[index] {
			t.Fatalf("date d allocation changed with date d return: %#v vs %#v", first.Allocations[index], second.Allocations[index])
		}
	}
}

func TestEnsembleWarmupIsCashAndExecutionIsLagged(t *testing.T) {
	bars := syntheticBars(10)
	base, err := RunAll(context.Background(), bars, testFactories(), false)
	if err != nil {
		t.Fatal(err)
	}
	run, err := RunEnsemble(context.Background(), bars, base, AllocatorParameters{ReliabilityWindow: 5, VolatilityWindow: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, allocation := range run.Allocations[:15] {
		if allocation.Weight != 0 {
			t.Fatalf("warm-up allocation = %#v", allocation)
		}
	}
	for _, weight := range run.Result.ExecutedWeights[:3] {
		if weight.Weight != 0 {
			t.Fatal("first date must be cash")
		}
	}
}

func TestEnsembleRejectsWrongBaseStrategyCount(t *testing.T) {
	bars := syntheticBars(3)
	base, err := RunAll(context.Background(), bars, []strategyapi.Factory{testFactories()[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunEnsemble(context.Background(), bars, base, AllocatorParameters{ReliabilityWindow: 3, VolatilityWindow: 2}); err == nil {
		t.Fatal("expected base strategy count error")
	}
}
