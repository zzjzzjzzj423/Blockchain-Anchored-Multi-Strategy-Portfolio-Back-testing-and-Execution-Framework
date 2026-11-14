package experiment

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
	"msbd5017/portfolio-proof/internal/execution"
	"msbd5017/portfolio-proof/internal/strategy"
)

type ReturnRecord struct {
	Date        time.Time
	Strategy    string
	GrossReturn float64
	NetReturn   float64
	Equity      float64
}

type Result struct {
	Signals         []domain.Signal
	TargetWeights   []domain.TargetWeight
	ExecutedWeights []domain.TargetWeight
	Orders          []domain.Order
	Trades          []domain.Trade
	Ledger          []domain.LedgerEntry
	Returns         []ReturnRecord
}

type day struct {
	date time.Time
	bars []domain.MarketBar
}

type namedResult struct {
	name   string
	result Result
	err    error
}

// RunAll executes fresh strategy instances either serially or concurrently.
// Results are always merged with the same canonical ordering.
func RunAll(ctx context.Context, bars []domain.MarketBar, factories []strategy.Factory, parallel bool) (Result, error) {
	return runAll(ctx, bars, factories, parallel, func(name string) execution.Config {
		return execution.DefaultConfig(name)
	})
}

// RunAllWithCosts is used only by the frozen one-factor sensitivity runner.
func RunAllWithCosts(ctx context.Context, bars []domain.MarketBar, factories []strategy.Factory, parallel bool, commissionRate, slippageRate float64) (Result, error) {
	return runAll(ctx, bars, factories, parallel, func(name string) execution.Config {
		config := execution.DefaultConfig(name)
		config.CommissionRate = commissionRate
		config.SlippageRate = slippageRate
		return config
	})
}

func runAll(ctx context.Context, bars []domain.MarketBar, factories []strategy.Factory, parallel bool, configFor func(string) execution.Config) (Result, error) {
	days, err := groupDays(bars)
	if err != nil {
		return Result{}, err
	}
	if len(factories) == 0 {
		return Result{}, fmt.Errorf("at least one strategy factory is required")
	}

	results := make([]namedResult, 0, len(factories))
	if parallel {
		channel := make(chan namedResult, len(factories))
		var wait sync.WaitGroup
		for _, factory := range factories {
			factory := factory
			wait.Add(1)
			go func() {
				defer wait.Done()
				result, runErr := runOne(ctx, days, factory, configFor(factory.Name))
				channel <- namedResult{name: factory.Name, result: result, err: runErr}
			}()
		}
		wait.Wait()
		close(channel)
		for result := range channel {
			results = append(results, result)
		}
	} else {
		for _, factory := range factories {
			result, runErr := runOne(ctx, days, factory, configFor(factory.Name))
			results = append(results, namedResult{name: factory.Name, result: result, err: runErr})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })
	merged := Result{}
	for _, result := range results {
		if result.err != nil {
			return Result{}, fmt.Errorf("run %s: %w", result.name, result.err)
		}
		merged.Signals = append(merged.Signals, result.result.Signals...)
		merged.TargetWeights = append(merged.TargetWeights, result.result.TargetWeights...)
		merged.ExecutedWeights = append(merged.ExecutedWeights, result.result.ExecutedWeights...)
		merged.Orders = append(merged.Orders, result.result.Orders...)
		merged.Trades = append(merged.Trades, result.result.Trades...)
		merged.Ledger = append(merged.Ledger, result.result.Ledger...)
		merged.Returns = append(merged.Returns, result.result.Returns...)
	}
	sortResult(&merged)
	return merged, nil
}

func runOne(ctx context.Context, days []day, factory strategy.Factory, config execution.Config) (Result, error) {
	instance := factory.New()
	if instance == nil || instance.Name() != factory.Name {
		return Result{}, fmt.Errorf("factory name mismatch")
	}
	if config.Strategy != factory.Name {
		return Result{}, fmt.Errorf("execution config strategy mismatch")
	}
	engine, err := execution.New(config)
	if err != nil {
		return Result{}, err
	}
	history := strategy.NewMemoryHistory()
	var pending *strategy.Decision
	previousEquity := config.InitialCash
	result := Result{}

	for _, current := range days {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		input := execution.DayInput{Date: current.date, Bars: current.bars}
		if pending != nil && pending.Rebalance {
			input.Rebalance = true
			input.TargetWeights = pending.TargetWeights
		}
		dayResult, err := engine.Step(input)
		if err != nil {
			return Result{}, err
		}
		result.Orders = append(result.Orders, dayResult.Orders...)
		result.Trades = append(result.Trades, dayResult.Trades...)
		result.Ledger = append(result.Ledger, dayResult.Ledger)
		result.Returns = append(result.Returns, ReturnRecord{
			Date: current.date, Strategy: factory.Name,
			GrossReturn: dayResult.Ledger.GrossPnL / previousEquity,
			NetReturn:   dayResult.Ledger.NetPnL / previousEquity,
			Equity:      dayResult.Ledger.Equity,
		})
		previousEquity = dayResult.Ledger.Equity
		weights := make(map[string]float64, len(dayResult.Positions))
		for _, position := range dayResult.Positions {
			weights[position.Symbol] = position.Weight
		}
		for _, bar := range current.bars {
			result.ExecutedWeights = append(result.ExecutedWeights, domain.TargetWeight{
				Date: current.date, Strategy: factory.Name, Symbol: bar.Symbol, Weight: weights[bar.Symbol],
			})
		}

		if err := history.Add(current.bars); err != nil {
			return Result{}, err
		}
		decision, err := instance.Decide(strategy.Context{Date: current.date, Bars: current.bars, History: history})
		if err != nil {
			return Result{}, err
		}
		if err := validateDecision(factory.Name, current, decision); err != nil {
			return Result{}, err
		}
		result.Signals = append(result.Signals, decision.Signals...)
		result.TargetWeights = append(result.TargetWeights, decision.TargetWeights...)
		copyDecision := decision
		pending = &copyDecision
	}
	return result, nil
}

func validateDecision(name string, current day, decision strategy.Decision) error {
	if !decision.Date.Equal(current.date) || len(decision.Signals) != len(current.bars) || len(decision.TargetWeights) != len(current.bars) {
		return fmt.Errorf("%s returned incomplete decision for %s", name, current.date.Format("2006-01-02"))
	}
	want := make(map[string]struct{}, len(current.bars))
	for _, bar := range current.bars {
		want[bar.Symbol] = struct{}{}
	}
	total := 0.0
	for index, signal := range decision.Signals {
		if signal.Date != current.date || signal.Strategy != name || signal.Value < 0 || signal.Value > 1 {
			return fmt.Errorf("%s returned invalid signal", name)
		}
		weight := decision.TargetWeights[index]
		if weight.Date != current.date || weight.Strategy != name || weight.Symbol != signal.Symbol || weight.Weight < 0 || weight.Weight > 1 {
			return fmt.Errorf("%s returned invalid target weight", name)
		}
		if _, ok := want[signal.Symbol]; !ok {
			return fmt.Errorf("%s returned unknown symbol %s", name, signal.Symbol)
		}
		total += weight.Weight
	}
	if total > 1+1e-12 {
		return fmt.Errorf("%s target weights sum to %.16g", name, total)
	}
	return nil
}

func groupDays(bars []domain.MarketBar) ([]day, error) {
	if len(bars) == 0 {
		return nil, fmt.Errorf("market data is empty")
	}
	sorted := append([]domain.MarketBar(nil), bars...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Date.Equal(sorted[j].Date) {
			return sorted[i].Date.Before(sorted[j].Date)
		}
		return sorted[i].Symbol < sorted[j].Symbol
	})
	var days []day
	for _, bar := range sorted {
		if bar.Date.Location() != time.UTC || bar.Date.Hour() != 0 || bar.Symbol == "" {
			return nil, fmt.Errorf("invalid market bar")
		}
		if len(days) == 0 || !days[len(days)-1].date.Equal(bar.Date) {
			days = append(days, day{date: bar.Date})
		}
		current := &days[len(days)-1]
		if len(current.bars) > 0 && current.bars[len(current.bars)-1].Symbol == bar.Symbol {
			return nil, fmt.Errorf("duplicate bar for %s on %s", bar.Symbol, bar.Date.Format("2006-01-02"))
		}
		current.bars = append(current.bars, bar)
	}
	return days, nil
}

func sortResult(result *Result) {
	sort.Slice(result.Signals, func(i, j int) bool {
		return lessKey(result.Signals[i].Date, result.Signals[i].Strategy, result.Signals[i].Symbol, result.Signals[j].Date, result.Signals[j].Strategy, result.Signals[j].Symbol)
	})
	sort.Slice(result.TargetWeights, func(i, j int) bool {
		return lessKey(result.TargetWeights[i].Date, result.TargetWeights[i].Strategy, result.TargetWeights[i].Symbol, result.TargetWeights[j].Date, result.TargetWeights[j].Strategy, result.TargetWeights[j].Symbol)
	})
	sort.Slice(result.ExecutedWeights, func(i, j int) bool {
		return lessKey(result.ExecutedWeights[i].Date, result.ExecutedWeights[i].Strategy, result.ExecutedWeights[i].Symbol, result.ExecutedWeights[j].Date, result.ExecutedWeights[j].Strategy, result.ExecutedWeights[j].Symbol)
	})
	sort.Slice(result.Orders, func(i, j int) bool {
		return lessEvent(result.Orders[i].Date, result.Orders[i].Strategy, result.Orders[i].PhaseOrder, result.Orders[i].Sequence, result.Orders[j].Date, result.Orders[j].Strategy, result.Orders[j].PhaseOrder, result.Orders[j].Sequence)
	})
	sort.Slice(result.Trades, func(i, j int) bool {
		return lessEvent(result.Trades[i].Date, result.Trades[i].Strategy, result.Trades[i].PhaseOrder, result.Trades[i].Sequence, result.Trades[j].Date, result.Trades[j].Strategy, result.Trades[j].PhaseOrder, result.Trades[j].Sequence)
	})
	sort.Slice(result.Ledger, func(i, j int) bool {
		return lessKey(result.Ledger[i].Date, result.Ledger[i].Strategy, "", result.Ledger[j].Date, result.Ledger[j].Strategy, "")
	})
	sort.Slice(result.Returns, func(i, j int) bool {
		return lessKey(result.Returns[i].Date, result.Returns[i].Strategy, "", result.Returns[j].Date, result.Returns[j].Strategy, "")
	})
}

func lessKey(leftDate time.Time, leftStrategy, leftSymbol string, rightDate time.Time, rightStrategy, rightSymbol string) bool {
	if !leftDate.Equal(rightDate) {
		return leftDate.Before(rightDate)
	}
	if leftStrategy != rightStrategy {
		return leftStrategy < rightStrategy
	}
	return leftSymbol < rightSymbol
}

func lessEvent(leftDate time.Time, leftStrategy string, leftPhase int, leftSequence uint64, rightDate time.Time, rightStrategy string, rightPhase int, rightSequence uint64) bool {
	if !leftDate.Equal(rightDate) {
		return leftDate.Before(rightDate)
	}
	if leftStrategy != rightStrategy {
		return leftStrategy < rightStrategy
	}
	if leftPhase != rightPhase {
		return leftPhase < rightPhase
	}
	return leftSequence < rightSequence
}
