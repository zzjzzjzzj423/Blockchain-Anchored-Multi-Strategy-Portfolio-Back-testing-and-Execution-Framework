package domain

import "time"

// PhaseOrder values implement SPEC 13.5 event ordering.
const (
	PhaseSignal     = 10
	PhaseAllocation = 20
	PhaseSell       = 30
	PhaseBuy        = 40
	PhaseAccounting = 50
	PhaseMetric     = 60
)

// Side is the direction of an order or trade.
type Side string

const (
	SideSell Side = "sell"
	SideBuy  Side = "buy"
)

// OrderStatus records whether a deterministic order became a trade.
type OrderStatus string

const (
	OrderExecuted OrderStatus = "executed"
	OrderSkipped  OrderStatus = "skipped"
)

// MarketBar is one validated UTC daily OHLCV observation.
type MarketBar struct {
	Date   time.Time
	Symbol string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Signal is a strategy observation produced after a daily close.
type Signal struct {
	Date     time.Time
	Strategy string
	Symbol   string
	Value    float64
}

// TargetWeight is a long/cash target executed only on a later valid date.
type TargetWeight struct {
	Date     time.Time
	Strategy string
	Symbol   string
	Weight   float64
}

// Order records both requested and actually executable quantities.
type Order struct {
	Date              time.Time
	Strategy          string
	Symbol            string
	Side              Side
	TargetWeight      float64
	ReferencePrice    float64
	RequestedQuantity float64
	ExecutedQuantity  float64
	ScaleFactor       float64
	Status            OrderStatus
	SkipReason        string
	PhaseOrder        int
	Sequence          uint64
}

// Trade is one cost-aware fill at the daily close reference price.
type Trade struct {
	Date             time.Time
	Strategy         string
	Symbol           string
	Side             Side
	Quantity         float64
	ReferencePrice   float64
	ExecutionPrice   float64
	ExecutedNotional float64
	Commission       float64
	SlippageCost     float64
	PhaseOrder       int
	Sequence         uint64
}

// Position is the marked end-of-day state of one held asset.
type Position struct {
	Date        time.Time
	Strategy    string
	Symbol      string
	Quantity    float64
	AverageCost float64
	MarketPrice float64
	MarketValue float64
	Weight      float64
}

// LedgerEntry is one deterministic end-of-day accounting record.
type LedgerEntry struct {
	Date                   time.Time
	Strategy               string
	PretradeEquity         float64
	Cash                   float64
	PositionValue          float64
	GrossPnL               float64
	Commission             float64
	SlippageCost           float64
	NetPnL                 float64
	Equity                 float64
	ExecutedNotional       float64
	Turnover               float64
	Exposure               float64
	AccountingResidual     float64
	PnLIdentityResidual    float64
	CumulativeCommission   float64
	CumulativeSlippageCost float64
	CashNormalized         bool
	PhaseOrder             int
	Sequence               uint64
}
