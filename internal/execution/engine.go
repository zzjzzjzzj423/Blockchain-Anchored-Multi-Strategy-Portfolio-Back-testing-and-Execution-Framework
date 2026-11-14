package execution

import (
	"fmt"
	"math"
	"sort"
	"time"

	"msbd5017/portfolio-proof/internal/domain"
)

// Config fixes all execution and accounting assumptions.
type Config struct {
	Strategy          string
	InitialCash       float64
	CommissionRate    float64
	SlippageRate      float64
	MinimumNotional   float64
	QuantityDecimals  int
	CashTolerance     float64
	ResidualTolerance float64
}

// DefaultConfig implements the frozen SPEC values.
func DefaultConfig(strategy string) Config {
	return Config{
		Strategy:          strategy,
		InitialCash:       1_000_000,
		CommissionRate:    0.0010,
		SlippageRate:      0.0005,
		MinimumNotional:   10,
		QuantityDecimals:  8,
		CashTolerance:     1e-8,
		ResidualTolerance: 1e-10,
	}
}

// DayInput contains prices and, optionally, a full target-weight rebalance.
// Rebalance=false marks existing positions without trading.
type DayInput struct {
	Date          time.Time
	Bars          []domain.MarketBar
	Rebalance     bool
	TargetWeights []domain.TargetWeight
}

// DayResult contains stable, symbol-sorted records for one date.
type DayResult struct {
	Orders    []domain.Order
	Trades    []domain.Trade
	Positions []domain.Position
	Ledger    domain.LedgerEntry
}

type positionState struct {
	quantity    float64
	averageCost float64
}

type candidateOrder struct {
	symbol         string
	side           domain.Side
	targetWeight   float64
	referencePrice float64
	quantity       float64
}

// Engine is a strategy-independent, deterministic long/cash execution ledger.
type Engine struct {
	config               Config
	cash                 float64
	positions            map[string]positionState
	lastPrices           map[string]float64
	lastDate             time.Time
	lastEquity           float64
	sequence             uint64
	cumulativeCommission float64
	cumulativeSlippage   float64
	ledger               []domain.LedgerEntry
}

// New validates a config and creates a pure-cash portfolio.
func New(config Config) (*Engine, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return &Engine{
		config:     config,
		cash:       config.InitialCash,
		positions:  make(map[string]positionState),
		lastPrices: make(map[string]float64),
		lastEquity: config.InitialCash,
	}, nil
}

// Step marks the portfolio and optionally executes one complete target rebalance.
func (engine *Engine) Step(input DayInput) (DayResult, error) {
	prices, err := engine.validateInput(input)
	if err != nil {
		return DayResult{}, err
	}
	weights, err := engine.targetWeights(input, prices)
	if err != nil {
		return DayResult{}, err
	}

	pretradePositionValue := engine.positionValue(prices)
	pretradeEquity := engine.cash + pretradePositionValue
	if !finitePositive(pretradeEquity) {
		return DayResult{}, fmt.Errorf("pretrade equity must be finite and positive")
	}
	grossPnL, err := engine.grossPnL(prices)
	if err != nil {
		return DayResult{}, err
	}

	result := DayResult{}
	if input.Rebalance {
		candidates := engine.candidates(prices, weights, pretradeEquity)
		sells, buys := splitCandidates(candidates)
		for _, candidate := range sells {
			order, trade, traded, err := engine.executeSell(input.Date, candidate)
			if err != nil {
				return DayResult{}, err
			}
			result.Orders = append(result.Orders, order)
			if traded {
				result.Trades = append(result.Trades, trade)
			}
		}
		buyScale := engine.buyScale(buys)
		for _, candidate := range buys {
			order, trade, traded, err := engine.executeBuy(input.Date, candidate, buyScale)
			if err != nil {
				return DayResult{}, err
			}
			result.Orders = append(result.Orders, order)
			if traded {
				result.Trades = append(result.Trades, trade)
			}
		}
	}

	dayCommission, daySlippage, executedNotional := tradeTotals(result.Trades)
	cashNormalized := false
	if engine.cash < 0 {
		if engine.cash < -engine.config.CashTolerance {
			return DayResult{}, fmt.Errorf("cash constraint violated: %.12f", engine.cash)
		}
		engine.cash = 0
		cashNormalized = true
	}

	positionValue := engine.positionValue(prices)
	equity := engine.cash + positionValue
	if !finitePositive(equity) {
		return DayResult{}, fmt.Errorf("end-of-day equity must be finite and positive")
	}
	netPnL := equity - engine.lastEquity
	accountingResidual := math.Abs(equity-engine.cash-positionValue) / math.Max(1, equity)
	pnlResidual := math.Abs(netPnL-(grossPnL-dayCommission-daySlippage)) / math.Max(1, equity)
	if accountingResidual >= engine.config.ResidualTolerance {
		return DayResult{}, fmt.Errorf("accounting residual %.16g exceeds tolerance", accountingResidual)
	}
	if pnlResidual >= engine.config.ResidualTolerance {
		return DayResult{}, fmt.Errorf("PnL identity residual %.16g exceeds tolerance", pnlResidual)
	}

	engine.cumulativeCommission += dayCommission
	engine.cumulativeSlippage += daySlippage
	exposure := positionValue / equity
	if exposure < -engine.config.ResidualTolerance || exposure > 1+engine.config.ResidualTolerance {
		return DayResult{}, fmt.Errorf("exposure %.16g outside [0,1]", exposure)
	}
	turnover := executedNotional / pretradeEquity
	engine.sequence++
	ledger := domain.LedgerEntry{
		Date:                   input.Date,
		Strategy:               engine.config.Strategy,
		PretradeEquity:         pretradeEquity,
		Cash:                   engine.cash,
		PositionValue:          positionValue,
		GrossPnL:               grossPnL,
		Commission:             dayCommission,
		SlippageCost:           daySlippage,
		NetPnL:                 netPnL,
		Equity:                 equity,
		ExecutedNotional:       executedNotional,
		Turnover:               turnover,
		Exposure:               exposure,
		AccountingResidual:     accountingResidual,
		PnLIdentityResidual:    pnlResidual,
		CumulativeCommission:   engine.cumulativeCommission,
		CumulativeSlippageCost: engine.cumulativeSlippage,
		CashNormalized:         cashNormalized,
		PhaseOrder:             domain.PhaseAccounting,
		Sequence:               engine.sequence,
	}
	result.Ledger = ledger
	result.Positions = engine.markedPositions(input.Date, prices, equity)
	engine.ledger = append(engine.ledger, ledger)
	engine.lastDate = input.Date
	engine.lastEquity = equity
	for symbol, price := range prices {
		engine.lastPrices[symbol] = price
	}
	return result, nil
}

// Ledger returns a copy of all completed accounting entries.
func (engine *Engine) Ledger() []domain.LedgerEntry {
	return append([]domain.LedgerEntry(nil), engine.ledger...)
}

// Cash returns current uninvested cash.
func (engine *Engine) Cash() float64 {
	return engine.cash
}

func validateConfig(config Config) error {
	if config.Strategy == "" {
		return fmt.Errorf("strategy must not be empty")
	}
	if !finitePositive(config.InitialCash) {
		return fmt.Errorf("initial cash must be finite and positive")
	}
	if config.CommissionRate < 0 || config.SlippageRate < 0 || !finite(config.CommissionRate) || !finite(config.SlippageRate) || config.SlippageRate >= 1 {
		return fmt.Errorf("cost rates must be finite and non-negative")
	}
	if config.MinimumNotional < 0 || !finite(config.MinimumNotional) {
		return fmt.Errorf("minimum notional must be finite and non-negative")
	}
	if config.QuantityDecimals < 0 || config.QuantityDecimals > 12 {
		return fmt.Errorf("quantity decimals must be between 0 and 12")
	}
	if config.CashTolerance <= 0 || config.ResidualTolerance <= 0 {
		return fmt.Errorf("tolerances must be positive")
	}
	return nil
}

func (engine *Engine) validateInput(input DayInput) (map[string]float64, error) {
	date := input.Date
	if date.Location() != time.UTC || date.Hour() != 0 || date.Minute() != 0 || date.Second() != 0 || date.Nanosecond() != 0 {
		return nil, fmt.Errorf("date must be UTC midnight")
	}
	if !engine.lastDate.IsZero() && !date.After(engine.lastDate) {
		return nil, fmt.Errorf("dates must be strictly increasing")
	}
	if !input.Rebalance && len(input.TargetWeights) != 0 {
		return nil, fmt.Errorf("target weights require Rebalance=true")
	}
	prices := make(map[string]float64, len(input.Bars))
	for _, bar := range input.Bars {
		if bar.Date != date {
			return nil, fmt.Errorf("bar date for %s does not match input date", bar.Symbol)
		}
		if bar.Symbol == "" || !finitePositive(bar.Close) {
			return nil, fmt.Errorf("bar symbol and close must be valid")
		}
		if _, exists := prices[bar.Symbol]; exists {
			return nil, fmt.Errorf("duplicate bar for %s", bar.Symbol)
		}
		prices[bar.Symbol] = bar.Close
	}
	for symbol := range engine.positions {
		if _, ok := prices[symbol]; !ok {
			return nil, fmt.Errorf("missing price for held symbol %s", symbol)
		}
	}
	return prices, nil
}

func (engine *Engine) targetWeights(input DayInput, prices map[string]float64) (map[string]float64, error) {
	weights := make(map[string]float64, len(input.TargetWeights))
	if !input.Rebalance {
		return weights, nil
	}
	total := 0.0
	for _, target := range input.TargetWeights {
		if target.Strategy != "" && target.Strategy != engine.config.Strategy {
			return nil, fmt.Errorf("target strategy %s does not match engine strategy %s", target.Strategy, engine.config.Strategy)
		}
		if target.Symbol == "" || !finite(target.Weight) || target.Weight < 0 || target.Weight > 1 {
			return nil, fmt.Errorf("target weight for %s must be in [0,1]", target.Symbol)
		}
		if _, exists := weights[target.Symbol]; exists {
			return nil, fmt.Errorf("duplicate target weight for %s", target.Symbol)
		}
		if _, ok := prices[target.Symbol]; !ok && target.Weight > 0 {
			return nil, fmt.Errorf("missing price for target symbol %s", target.Symbol)
		}
		weights[target.Symbol] = target.Weight
	}
	symbols := make([]string, 0, len(weights))
	for symbol := range weights {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	for _, symbol := range symbols {
		total += weights[symbol]
	}
	if total > 1+1e-12 {
		return nil, fmt.Errorf("target weights sum to %.16g, greater than 1", total)
	}
	return weights, nil
}

func (engine *Engine) candidates(prices, weights map[string]float64, equity float64) []candidateOrder {
	symbolSet := make(map[string]struct{}, len(engine.positions)+len(weights))
	for symbol := range engine.positions {
		symbolSet[symbol] = struct{}{}
	}
	for symbol := range weights {
		symbolSet[symbol] = struct{}{}
	}
	symbols := make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	candidates := make([]candidateOrder, 0, len(symbols))
	for _, symbol := range symbols {
		price, ok := prices[symbol]
		if !ok {
			continue
		}
		currentValue := engine.positions[symbol].quantity * price
		targetWeight := weights[symbol]
		delta := targetWeight*equity - currentValue
		if delta == 0 {
			continue
		}
		quantity := engine.roundQuantity(math.Abs(delta) / price)
		side := domain.SideBuy
		if delta < 0 {
			side = domain.SideSell
			if quantity > engine.positions[symbol].quantity {
				quantity = engine.positions[symbol].quantity
			}
		}
		candidates = append(candidates, candidateOrder{
			symbol:         symbol,
			side:           side,
			targetWeight:   targetWeight,
			referencePrice: price,
			quantity:       quantity,
		})
	}
	return candidates
}

func splitCandidates(candidates []candidateOrder) ([]candidateOrder, []candidateOrder) {
	var sells, buys []candidateOrder
	for _, candidate := range candidates {
		if candidate.side == domain.SideSell {
			sells = append(sells, candidate)
		} else {
			buys = append(buys, candidate)
		}
	}
	// candidates already follow symbol order; explicit sorts protect future callers.
	sort.Slice(sells, func(i, j int) bool { return sells[i].symbol < sells[j].symbol })
	sort.Slice(buys, func(i, j int) bool { return buys[i].symbol < buys[j].symbol })
	return sells, buys
}

func (engine *Engine) executeSell(date time.Time, candidate candidateOrder) (domain.Order, domain.Trade, bool, error) {
	order := engine.newOrder(date, candidate, 1)
	quantity := candidate.quantity
	executionPrice := candidate.referencePrice * (1 - engine.config.SlippageRate)
	if quantity <= 0 {
		return engine.skip(order, "quantity_rounds_to_zero"), domain.Trade{}, false, nil
	}
	notional := quantity * executionPrice
	if notional < engine.config.MinimumNotional {
		return engine.skip(order, "below_min_notional"), domain.Trade{}, false, nil
	}
	state := engine.positions[candidate.symbol]
	if quantity > state.quantity+engine.config.CashTolerance {
		return domain.Order{}, domain.Trade{}, false, fmt.Errorf("sell quantity exceeds position for %s", candidate.symbol)
	}
	commission := notional * engine.config.CommissionRate
	slippage := quantity * (candidate.referencePrice - executionPrice)
	engine.cash += notional - commission
	state.quantity = engine.roundQuantity(state.quantity - quantity)
	if state.quantity <= 0.5/math.Pow10(engine.config.QuantityDecimals) {
		delete(engine.positions, candidate.symbol)
	} else {
		engine.positions[candidate.symbol] = state
	}
	order = engine.execute(order, quantity)
	trade := engine.newTrade(date, candidate, quantity, executionPrice, notional, commission, slippage)
	return order, trade, true, nil
}

func (engine *Engine) buyScale(candidates []candidateOrder) float64 {
	totalRequired := 0.0
	for _, candidate := range candidates {
		executionPrice := candidate.referencePrice * (1 + engine.config.SlippageRate)
		notional := candidate.quantity * executionPrice
		if candidate.quantity <= 0 || notional < engine.config.MinimumNotional {
			continue
		}
		totalRequired += notional * (1 + engine.config.CommissionRate)
	}
	if totalRequired <= engine.cash || totalRequired == 0 {
		return 1
	}
	return engine.cash / totalRequired
}

func (engine *Engine) executeBuy(date time.Time, candidate candidateOrder, scale float64) (domain.Order, domain.Trade, bool, error) {
	order := engine.newOrder(date, candidate, scale)
	quantity := candidate.quantity
	if scale < 1 {
		quantity = engine.floorQuantity(quantity * scale)
	}
	executionPrice := candidate.referencePrice * (1 + engine.config.SlippageRate)
	if quantity <= 0 {
		return engine.skip(order, "quantity_rounds_to_zero"), domain.Trade{}, false, nil
	}
	notional := quantity * executionPrice
	if notional < engine.config.MinimumNotional {
		return engine.skip(order, "below_min_notional"), domain.Trade{}, false, nil
	}
	commission := notional * engine.config.CommissionRate
	requiredCash := notional + commission
	if requiredCash > engine.cash+engine.config.CashTolerance {
		return domain.Order{}, domain.Trade{}, false, fmt.Errorf("scaled buy for %s exceeds available cash", candidate.symbol)
	}
	slippage := quantity * (executionPrice - candidate.referencePrice)
	state := engine.positions[candidate.symbol]
	newQuantity := engine.roundQuantity(state.quantity + quantity)
	bookCost := state.quantity*state.averageCost + notional + commission
	state.quantity = newQuantity
	state.averageCost = bookCost / newQuantity
	engine.positions[candidate.symbol] = state
	engine.cash -= requiredCash
	order = engine.execute(order, quantity)
	trade := engine.newTrade(date, candidate, quantity, executionPrice, notional, commission, slippage)
	return order, trade, true, nil
}

func (engine *Engine) newOrder(date time.Time, candidate candidateOrder, scale float64) domain.Order {
	engine.sequence++
	phase := domain.PhaseBuy
	if candidate.side == domain.SideSell {
		phase = domain.PhaseSell
	}
	return domain.Order{
		Date:              date,
		Strategy:          engine.config.Strategy,
		Symbol:            candidate.symbol,
		Side:              candidate.side,
		TargetWeight:      candidate.targetWeight,
		ReferencePrice:    candidate.referencePrice,
		RequestedQuantity: candidate.quantity,
		ScaleFactor:       scale,
		Status:            domain.OrderSkipped,
		PhaseOrder:        phase,
		Sequence:          engine.sequence,
	}
}

func (engine *Engine) execute(order domain.Order, quantity float64) domain.Order {
	order.ExecutedQuantity = quantity
	order.Status = domain.OrderExecuted
	return order
}

func (engine *Engine) skip(order domain.Order, reason string) domain.Order {
	order.SkipReason = reason
	return order
}

func (engine *Engine) newTrade(date time.Time, candidate candidateOrder, quantity, executionPrice, notional, commission, slippage float64) domain.Trade {
	engine.sequence++
	phase := domain.PhaseBuy
	if candidate.side == domain.SideSell {
		phase = domain.PhaseSell
	}
	return domain.Trade{
		Date:             date,
		Strategy:         engine.config.Strategy,
		Symbol:           candidate.symbol,
		Side:             candidate.side,
		Quantity:         quantity,
		ReferencePrice:   candidate.referencePrice,
		ExecutionPrice:   executionPrice,
		ExecutedNotional: notional,
		Commission:       commission,
		SlippageCost:     slippage,
		PhaseOrder:       phase,
		Sequence:         engine.sequence,
	}
}

func (engine *Engine) grossPnL(prices map[string]float64) (float64, error) {
	if engine.lastDate.IsZero() {
		return 0, nil
	}
	gross := 0.0
	for _, symbol := range engine.positionSymbols() {
		state := engine.positions[symbol]
		previous, ok := engine.lastPrices[symbol]
		if !ok {
			return 0, fmt.Errorf("missing previous close for %s", symbol)
		}
		gross += state.quantity * (prices[symbol] - previous)
	}
	return gross, nil
}

func (engine *Engine) positionValue(prices map[string]float64) float64 {
	total := 0.0
	for _, symbol := range engine.positionSymbols() {
		state := engine.positions[symbol]
		total += state.quantity * prices[symbol]
	}
	return total
}

func (engine *Engine) markedPositions(date time.Time, prices map[string]float64, equity float64) []domain.Position {
	symbols := engine.positionSymbols()
	positions := make([]domain.Position, 0, len(symbols))
	for _, symbol := range symbols {
		state := engine.positions[symbol]
		value := state.quantity * prices[symbol]
		positions = append(positions, domain.Position{
			Date:        date,
			Strategy:    engine.config.Strategy,
			Symbol:      symbol,
			Quantity:    state.quantity,
			AverageCost: state.averageCost,
			MarketPrice: prices[symbol],
			MarketValue: value,
			Weight:      value / equity,
		})
	}
	return positions
}

func (engine *Engine) positionSymbols() []string {
	symbols := make([]string, 0, len(engine.positions))
	for symbol := range engine.positions {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func (engine *Engine) roundQuantity(value float64) float64 {
	scale := math.Pow10(engine.config.QuantityDecimals)
	return math.RoundToEven(value*scale) / scale
}

func (engine *Engine) floorQuantity(value float64) float64 {
	scale := math.Pow10(engine.config.QuantityDecimals)
	return math.Floor(value*scale) / scale
}

func tradeTotals(trades []domain.Trade) (commission, slippage, notional float64) {
	for _, trade := range trades {
		commission += trade.Commission
		slippage += trade.SlippageCost
		notional += math.Abs(trade.ExecutedNotional)
	}
	return commission, slippage, notional
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePositive(value float64) bool {
	return finite(value) && value > 0
}
