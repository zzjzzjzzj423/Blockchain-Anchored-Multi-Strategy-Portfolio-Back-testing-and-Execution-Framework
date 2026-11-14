package experiment

import (
	"context"
	"fmt"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/execution"
	"msbd5017/portfolio-proof/internal/portfolio"
)

const EnsembleName = "ensemble"

type StrategyAllocation struct {
	Date               time.Time
	Strategy           string
	Reliability        float64
	RealizedVolatility float64
	Score              float64
	Weight             float64
}

type EnsembleRun struct {
	Result      Result
	Allocations []StrategyAllocation
}

// RunEnsemble combines the already costed base-strategy returns and executed
// asset weights. Allocation for date d receives history only through d-1; the
// resulting asset target is executed by the frozen engine on d+1.
func RunEnsemble(ctx context.Context, bars []domain.MarketBar, base Result, parameters AllocatorParameters) (EnsembleRun, error) {
	days, err := groupDays(bars)
	if err != nil {
		return EnsembleRun{}, err
	}
	baseReturns, strategies, err := indexBaseReturns(base.Returns)
	if err != nil {
		return EnsembleRun{}, err
	}
	baseWeights, err := indexBaseWeights(base.ExecutedWeights)
	if err != nil {
		return EnsembleRun{}, err
	}
	history := make(map[string][]float64, len(strategies))
	for _, name := range strategies {
		history[name] = []float64{}
	}
	engine, err := execution.New(execution.DefaultConfig(EnsembleName))
	if err != nil {
		return EnsembleRun{}, err
	}
	previousEquity := execution.DefaultConfig(EnsembleName).InitialCash
	var pending []domain.TargetWeight
	run := EnsembleRun{}

	for _, current := range days {
		if err := ctx.Err(); err != nil {
			return EnsembleRun{}, err
		}
		input := execution.DayInput{Date: current.date, Bars: current.bars}
		if pending != nil {
			input.Rebalance = true
			input.TargetWeights = pending
		}
		dayResult, err := engine.Step(input)
		if err != nil {
			return EnsembleRun{}, err
		}
		run.Result.Orders = append(run.Result.Orders, dayResult.Orders...)
		run.Result.Trades = append(run.Result.Trades, dayResult.Trades...)
		run.Result.Ledger = append(run.Result.Ledger, dayResult.Ledger)
		run.Result.Returns = append(run.Result.Returns, ReturnRecord{
			Date: current.date, Strategy: EnsembleName,
			GrossReturn: dayResult.Ledger.GrossPnL / previousEquity,
			NetReturn:   dayResult.Ledger.NetPnL / previousEquity,
			Equity:      dayResult.Ledger.Equity,
		})
		previousEquity = dayResult.Ledger.Equity
		executed := make(map[string]float64, len(dayResult.Positions))
		for _, position := range dayResult.Positions {
			executed[position.Symbol] = position.Weight
		}
		for _, bar := range current.bars {
			run.Result.ExecutedWeights = append(run.Result.ExecutedWeights, domain.TargetWeight{Date: current.date, Strategy: EnsembleName, Symbol: bar.Symbol, Weight: executed[bar.Symbol]})
		}

		allocations, err := portfolio.Allocate(history, parameters.ReliabilityWindow, parameters.VolatilityWindow)
		if err != nil {
			return EnsembleRun{}, err
		}
		dateKey := current.date.Format("2006-01-02")
		assetWeights := make(map[string]float64, len(current.bars))
		for _, allocation := range allocations {
			run.Allocations = append(run.Allocations, StrategyAllocation{
				Date: current.date, Strategy: allocation.Strategy, Reliability: allocation.Reliability,
				RealizedVolatility: allocation.RealizedVolatility, Score: allocation.Score, Weight: allocation.Weight,
			})
			strategyWeights, ok := baseWeights[dateKey][allocation.Strategy]
			if !ok {
				return EnsembleRun{}, fmt.Errorf("missing %s base weights for %s", allocation.Strategy, dateKey)
			}
			for _, bar := range current.bars {
				baseWeight, ok := strategyWeights[bar.Symbol]
				if !ok {
					return EnsembleRun{}, fmt.Errorf("missing %s/%s base weight for %s", allocation.Strategy, bar.Symbol, dateKey)
				}
				assetWeights[bar.Symbol] += allocation.Weight * baseWeight
			}
		}
		pending = make([]domain.TargetWeight, 0, len(current.bars))
		totalAssetWeight := 0.0
		for _, bar := range current.bars {
			weight := assetWeights[bar.Symbol]
			totalAssetWeight += weight
			run.Result.Signals = append(run.Result.Signals, domain.Signal{Date: current.date, Strategy: EnsembleName, Symbol: bar.Symbol, Value: weight})
			target := domain.TargetWeight{Date: current.date, Strategy: EnsembleName, Symbol: bar.Symbol, Weight: weight}
			run.Result.TargetWeights = append(run.Result.TargetWeights, target)
			pending = append(pending, target)
		}
		if totalAssetWeight > 1+1e-12 {
			return EnsembleRun{}, fmt.Errorf("ensemble asset weights sum to %.16g", totalAssetWeight)
		}
		returnsForDate, ok := baseReturns[dateKey]
		if !ok {
			return EnsembleRun{}, fmt.Errorf("missing base returns for %s", dateKey)
		}
		for _, name := range strategies {
			value, ok := returnsForDate[name]
			if !ok {
				return EnsembleRun{}, fmt.Errorf("missing %s return for %s", name, dateKey)
			}
			history[name] = append(history[name], value)
		}
	}
	sortResult(&run.Result)
	sort.Slice(run.Allocations, func(i, j int) bool {
		return lessKey(run.Allocations[i].Date, run.Allocations[i].Strategy, "", run.Allocations[j].Date, run.Allocations[j].Strategy, "")
	})
	return run, nil
}

func indexBaseReturns(returns []ReturnRecord) (map[string]map[string]float64, []string, error) {
	indexed := make(map[string]map[string]float64)
	strategySet := make(map[string]struct{})
	for _, record := range returns {
		if record.Strategy == "" || record.Strategy == EnsembleName {
			return nil, nil, fmt.Errorf("invalid base strategy return")
		}
		date := record.Date.Format("2006-01-02")
		if indexed[date] == nil {
			indexed[date] = make(map[string]float64)
		}
		if _, exists := indexed[date][record.Strategy]; exists {
			return nil, nil, fmt.Errorf("duplicate %s return on %s", record.Strategy, date)
		}
		indexed[date][record.Strategy] = record.NetReturn
		strategySet[record.Strategy] = struct{}{}
	}
	strategies := make([]string, 0, len(strategySet))
	for name := range strategySet {
		strategies = append(strategies, name)
	}
	sort.Strings(strategies)
	if len(strategies) != 3 {
		return nil, nil, fmt.Errorf("ensemble requires exactly three base strategies")
	}
	return indexed, strategies, nil
}

func indexBaseWeights(weights []domain.TargetWeight) (map[string]map[string]map[string]float64, error) {
	indexed := make(map[string]map[string]map[string]float64)
	for _, weight := range weights {
		if weight.Strategy == "" || weight.Symbol == "" || weight.Weight < 0 || weight.Weight > 1 {
			return nil, fmt.Errorf("invalid base weight")
		}
		date := weight.Date.Format("2006-01-02")
		if indexed[date] == nil {
			indexed[date] = make(map[string]map[string]float64)
		}
		if indexed[date][weight.Strategy] == nil {
			indexed[date][weight.Strategy] = make(map[string]float64)
		}
		if _, exists := indexed[date][weight.Strategy][weight.Symbol]; exists {
			return nil, fmt.Errorf("duplicate base weight for %s/%s on %s", weight.Strategy, weight.Symbol, date)
		}
		indexed[date][weight.Strategy][weight.Symbol] = weight.Weight
	}
	return indexed, nil
}
