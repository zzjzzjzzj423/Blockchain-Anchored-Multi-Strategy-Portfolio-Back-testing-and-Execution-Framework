package experiment

import (
	"bytes"
	"testing"

	"msbd5017/portfolio-proof/internal/metrics"
)

func TestSearchTraceIsCanonicalAndStable(t *testing.T) {
	candidate := BaselineCandidate()
	id, _ := candidate.ID()
	sharpe := 1.25
	evaluation := Evaluation{
		Assessment: CandidateAssessment{Candidate: candidate, CandidateID: id, MedianValidationSharpe: sharpe},
		Folds:      []FoldEvaluation{{Fold: WalkForwardFolds()[0], Metrics: metrics.Summary{Days: 181, Sharpe: &sharpe}}},
	}
	entry := SearchTraceEntry{ProposalIndex: 1, Status: "evaluated", Proposal: Proposal{Candidate: candidate, Proposer: "manual", Rationale: "baseline"}, CandidateID: id, Evaluation: &evaluation}
	first, err := SearchTraceJSONL([]SearchTraceEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := SearchTraceJSONL([]SearchTraceEntry{entry})
	if !bytes.Equal(first, second) || len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatal("search trace is not stable canonical JSONL")
	}
}
