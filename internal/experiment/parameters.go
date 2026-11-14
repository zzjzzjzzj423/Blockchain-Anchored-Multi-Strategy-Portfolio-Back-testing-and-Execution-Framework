package experiment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"msbd5017/portfolio-proof/internal/canonical"
)

type TrendParameters struct {
	FastWindow int `json:"fast_window"`
	SlowWindow int `json:"slow_window"`
}

type MeanReversionParameters struct {
	Lookback int     `json:"lookback"`
	EntryZ   float64 `json:"-"`
	ExitZ    float64 `json:"-"`
}

type AllocatorParameters struct {
	ReliabilityWindow int `json:"reliability_window"`
	VolatilityWindow  int `json:"volatility_window"`
}

type Candidate struct {
	Trend         TrendParameters
	MeanReversion MeanReversionParameters
	Allocator     AllocatorParameters
}

type canonicalCandidate struct {
	Allocator struct {
		ReliabilityWindow int `json:"reliability_window"`
		VolatilityWindow  int `json:"volatility_window"`
	} `json:"allocator"`
	MeanReversion struct {
		EntryZ   string `json:"entry_z"`
		ExitZ    string `json:"exit_z"`
		Lookback int    `json:"lookback"`
	} `json:"mean_reversion"`
	Trend TrendParameters `json:"trend"`
}

func BaselineCandidate() Candidate {
	return Candidate{
		Trend:         TrendParameters{FastWindow: 20, SlowWindow: 100},
		MeanReversion: MeanReversionParameters{Lookback: 20, EntryZ: -1, ExitZ: 0},
		Allocator:     AllocatorParameters{ReliabilityWindow: 63, VolatilityWindow: 20},
	}
}

func (candidate Candidate) Validate() error {
	if !containsInt([]int{10, 20, 30}, candidate.Trend.FastWindow) || !containsInt([]int{60, 100, 150}, candidate.Trend.SlowWindow) || candidate.Trend.FastWindow >= candidate.Trend.SlowWindow {
		return fmt.Errorf("trend parameters are outside the frozen space")
	}
	if !containsInt([]int{10, 20, 30}, candidate.MeanReversion.Lookback) || !containsFloat([]float64{-0.5, -1, -1.5}, candidate.MeanReversion.EntryZ) || !containsFloat([]float64{0, -0.25}, candidate.MeanReversion.ExitZ) || candidate.MeanReversion.EntryZ >= candidate.MeanReversion.ExitZ {
		return fmt.Errorf("mean-reversion parameters are outside the frozen space")
	}
	if !containsInt([]int{30, 63, 90}, candidate.Allocator.ReliabilityWindow) || !containsInt([]int{20, 30, 60}, candidate.Allocator.VolatilityWindow) {
		return fmt.Errorf("allocator parameters are outside the frozen space")
	}
	return nil
}

func (candidate Candidate) CanonicalJSON() ([]byte, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	entry, err := canonical.Decimal12(candidate.MeanReversion.EntryZ)
	if err != nil {
		return nil, err
	}
	exit, err := canonical.Decimal12(candidate.MeanReversion.ExitZ)
	if err != nil {
		return nil, err
	}
	value := canonicalCandidate{Trend: candidate.Trend}
	value.Allocator.ReliabilityWindow = candidate.Allocator.ReliabilityWindow
	value.Allocator.VolatilityWindow = candidate.Allocator.VolatilityWindow
	value.MeanReversion.Lookback = candidate.MeanReversion.Lookback
	value.MeanReversion.EntryZ = entry
	value.MeanReversion.ExitZ = exit
	return canonical.JSON(value)
}

func (candidate Candidate) ID() (string, error) {
	content, err := candidate.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func CandidateSpace() []Candidate {
	result := make([]Candidate, 0, 1458)
	for _, fast := range []int{10, 20, 30} {
		for _, slow := range []int{60, 100, 150} {
			if fast >= slow {
				continue
			}
			for _, lookback := range []int{10, 20, 30} {
				for _, entry := range []float64{-0.5, -1, -1.5} {
					for _, exit := range []float64{0, -0.25} {
						if entry >= exit {
							continue
						}
						for _, reliability := range []int{30, 63, 90} {
							for _, volatility := range []int{20, 30, 60} {
								result = append(result, Candidate{
									Trend:         TrendParameters{FastWindow: fast, SlowWindow: slow},
									MeanReversion: MeanReversionParameters{Lookback: lookback, EntryZ: entry, ExitZ: exit},
									Allocator:     AllocatorParameters{ReliabilityWindow: reliability, VolatilityWindow: volatility},
								})
							}
						}
					}
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := result[i].CanonicalJSON()
		right, _ := result[j].CanonicalJSON()
		return bytes.Compare(left, right) < 0
	})
	return result
}

type CandidateAssessment struct {
	Candidate                Candidate
	CandidateID              string
	MedianValidationSharpe   float64
	WorstValidationDrawdown  float64
	MedianAnnualizedTurnover float64
}

func BetterCandidate(left, right CandidateAssessment) bool {
	if math.Abs(left.MedianValidationSharpe-right.MedianValidationSharpe) >= 1e-6 {
		return left.MedianValidationSharpe > right.MedianValidationSharpe
	}
	if left.WorstValidationDrawdown != right.WorstValidationDrawdown {
		return left.WorstValidationDrawdown < right.WorstValidationDrawdown
	}
	if left.MedianAnnualizedTurnover != right.MedianAnnualizedTurnover {
		return left.MedianAnnualizedTurnover < right.MedianAnnualizedTurnover
	}
	leftJSON, _ := left.Candidate.CanonicalJSON()
	rightJSON, _ := right.Candidate.CanonicalJSON()
	return bytes.Compare(leftJSON, rightJSON) < 0
}

type Proposal struct {
	Candidate Candidate
	Proposer  string
	Rationale string
}

type AgentProposer interface {
	Propose(history []CandidateAssessment) (Proposal, error)
}

// ManualProposer is the no-agent fallback required by PLAN. After the fixed
// baseline it maximizes parameter-space distance from the current leader, then
// uses canonical JSON order as a deterministic tie-break.
type ManualProposer struct{}

func (ManualProposer) Propose(history []CandidateAssessment) (Proposal, error) {
	if len(history) == 0 {
		return Proposal{Candidate: BaselineCandidate(), Proposer: "manual_deterministic", Rationale: "frozen baseline candidate"}, nil
	}
	best := history[0]
	evaluated := make(map[string]struct{}, len(history))
	for _, assessment := range history {
		evaluated[assessment.CandidateID] = struct{}{}
		if BetterCandidate(assessment, best) {
			best = assessment
		}
	}
	maximumDistance := -1
	var selected Candidate
	found := false
	for _, candidate := range CandidateSpace() {
		id, _ := candidate.ID()
		if _, exists := evaluated[id]; exists {
			continue
		}
		distance := candidateDistance(candidate, best.Candidate)
		if !found || distance > maximumDistance {
			selected = candidate
			maximumDistance = distance
			found = true
		}
	}
	if !found {
		return Proposal{}, fmt.Errorf("candidate space exhausted")
	}
	return Proposal{Candidate: selected, Proposer: "manual_deterministic", Rationale: "maximize frozen-space diversity from current validation leader"}, nil
}

func candidateDistance(left, right Candidate) int {
	distance := 0
	if left.Trend.FastWindow != right.Trend.FastWindow {
		distance++
	}
	if left.Trend.SlowWindow != right.Trend.SlowWindow {
		distance++
	}
	if left.MeanReversion.Lookback != right.MeanReversion.Lookback {
		distance++
	}
	if left.MeanReversion.EntryZ != right.MeanReversion.EntryZ {
		distance++
	}
	if left.MeanReversion.ExitZ != right.MeanReversion.ExitZ {
		distance++
	}
	if left.Allocator.ReliabilityWindow != right.Allocator.ReliabilityWindow {
		distance++
	}
	if left.Allocator.VolatilityWindow != right.Allocator.VolatilityWindow {
		distance++
	}
	return distance
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFloat(values []float64, target float64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
