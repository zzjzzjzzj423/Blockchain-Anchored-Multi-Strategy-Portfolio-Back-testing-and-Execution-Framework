package experiment

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestCandidateSpaceAndBaseline(t *testing.T) {
	space := CandidateSpace()
	if len(space) != 1458 {
		t.Fatalf("candidate space = %d, want 1458", len(space))
	}
	baseline := BaselineCandidate()
	if err := baseline.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := (ManualProposer{}).Propose(nil)
	if err != nil || first.Candidate != baseline {
		t.Fatalf("first proposal = %#v, %v", first, err)
	}
}

func TestParameterSchemaIsValidJSON(t *testing.T) {
	content, err := os.ReadFile("../../configs/schemas/parameters.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("unexpected parameter schema: %#v", schema)
	}
}

func TestCandidateCanonicalIdentityIsStable(t *testing.T) {
	candidate := BaselineCandidate()
	first, _ := candidate.CanonicalJSON()
	second, _ := candidate.CanonicalJSON()
	if !bytes.Equal(first, second) {
		t.Fatal("canonical candidate changed")
	}
	firstID, _ := candidate.ID()
	secondID, _ := candidate.ID()
	if firstID != secondID || len(firstID) != 64 {
		t.Fatalf("candidate IDs = %q/%q", firstID, secondID)
	}
}

func TestCandidateValidationRejectsOutsideSpace(t *testing.T) {
	candidate := BaselineCandidate()
	candidate.Trend.FastWindow = 11
	if err := candidate.Validate(); err == nil {
		t.Fatal("expected trend validation error")
	}
	candidate = BaselineCandidate()
	candidate.MeanReversion.EntryZ = -0.7
	if err := candidate.Validate(); err == nil {
		t.Fatal("expected mean-reversion validation error")
	}
	candidate = BaselineCandidate()
	candidate.Allocator.VolatilityWindow = 21
	if err := candidate.Validate(); err == nil {
		t.Fatal("expected allocator validation error")
	}
}

func TestSelectionTieBreakOrder(t *testing.T) {
	left := CandidateAssessment{Candidate: BaselineCandidate(), MedianValidationSharpe: 1, WorstValidationDrawdown: 0.2, MedianAnnualizedTurnover: 2}
	right := left
	right.Candidate.Trend.FastWindow = 10
	right.MedianValidationSharpe = 1 + 5e-7
	right.WorstValidationDrawdown = 0.1
	if !BetterCandidate(right, left) {
		t.Fatal("smaller drawdown should break a Sharpe tolerance tie")
	}
	right.WorstValidationDrawdown = left.WorstValidationDrawdown
	right.MedianAnnualizedTurnover = 1
	if !BetterCandidate(right, left) {
		t.Fatal("turnover should be the next tie-break")
	}
}

func TestManualProposerReturnsUniqueCandidate(t *testing.T) {
	baseline := BaselineCandidate()
	id, _ := baseline.ID()
	history := []CandidateAssessment{{Candidate: baseline, CandidateID: id, MedianValidationSharpe: 1}}
	proposal, err := (ManualProposer{}).Propose(history)
	if err != nil {
		t.Fatal(err)
	}
	proposalID, _ := proposal.Candidate.ID()
	if proposalID == id {
		t.Fatal("manual proposer repeated an evaluated candidate")
	}
}
