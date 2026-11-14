package experiment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/metrics"
)

const MaximumCandidateBudget = 20

type SearchTraceEntry struct {
	ProposalIndex int
	Status        string
	Proposal      Proposal
	CandidateID   string
	Evaluation    *Evaluation
	Reason        string
}

type SearchResult struct {
	Trace       []SearchTraceEntry
	Evaluations []Evaluation
	Selected    Evaluation
}

func RunSearch(ctx context.Context, evaluator *CandidateEvaluator, proposer AgentProposer, budget int) (SearchResult, error) {
	if evaluator == nil || proposer == nil || budget < 1 || budget > MaximumCandidateBudget {
		return SearchResult{}, fmt.Errorf("search requires evaluator, proposer, and budget in [1,%d]", MaximumCandidateBudget)
	}
	result := SearchResult{}
	history := make([]CandidateAssessment, 0, budget)
	evaluated := make(map[string]struct{}, budget)
	for proposalIndex := 1; len(result.Evaluations) < budget; proposalIndex++ {
		if proposalIndex > budget*5 {
			return SearchResult{}, fmt.Errorf("too many invalid or duplicate proposals")
		}
		proposal, err := proposer.Propose(history)
		if err != nil {
			return SearchResult{}, err
		}
		if err := proposal.Candidate.Validate(); err != nil {
			result.Trace = append(result.Trace, SearchTraceEntry{ProposalIndex: proposalIndex, Status: "invalid", Proposal: proposal, Reason: err.Error()})
			continue
		}
		id, _ := proposal.Candidate.ID()
		if _, exists := evaluated[id]; exists {
			result.Trace = append(result.Trace, SearchTraceEntry{ProposalIndex: proposalIndex, Status: "duplicate", Proposal: proposal, CandidateID: id, Reason: "candidate already evaluated"})
			continue
		}
		evaluation, err := evaluator.Evaluate(ctx, proposal.Candidate)
		if err != nil {
			return SearchResult{}, err
		}
		evaluated[id] = struct{}{}
		history = append(history, evaluation.Assessment)
		result.Evaluations = append(result.Evaluations, evaluation)
		copyEvaluation := evaluation
		result.Trace = append(result.Trace, SearchTraceEntry{ProposalIndex: proposalIndex, Status: "evaluated", Proposal: proposal, CandidateID: id, Evaluation: &copyEvaluation})
	}
	result.Selected = result.Evaluations[0]
	for _, evaluation := range result.Evaluations[1:] {
		if BetterCandidate(evaluation.Assessment, result.Selected.Assessment) {
			result.Selected = evaluation
		}
	}
	return result, nil
}

func SearchTraceJSONL(entries []SearchTraceEntry) ([]byte, error) {
	var output bytes.Buffer
	for _, entry := range entries {
		document, err := traceDocument(entry)
		if err != nil {
			return nil, err
		}
		line, err := canonical.JSON(document)
		if err != nil {
			return nil, err
		}
		output.Write(line)
	}
	return output.Bytes(), nil
}

func traceDocument(entry SearchTraceEntry) (map[string]any, error) {
	document := map[string]any{
		"candidate_id":   entry.CandidateID,
		"parameters":     nil,
		"proposal_index": entry.ProposalIndex,
		"proposer":       entry.Proposal.Proposer,
		"rationale":      entry.Proposal.Rationale,
		"reason":         nil,
		"status":         entry.Status,
		"validation":     nil,
	}
	parameters, err := candidateDocument(entry.Proposal.Candidate)
	if err == nil {
		document["parameters"] = parameters
	} else if entry.Status != "invalid" {
		return nil, err
	}
	if entry.Reason != "" {
		document["reason"] = entry.Reason
	}
	if entry.Evaluation != nil {
		document["validation"], err = evaluationDocument(*entry.Evaluation)
	}
	return document, nil
}

func evaluationDocument(evaluation Evaluation) (map[string]any, error) {
	folds := make([]any, 0, len(evaluation.Folds))
	for _, fold := range evaluation.Folds {
		folds = append(folds, map[string]any{
			"fold":             fold.Fold.ID,
			"train_start":      fold.Fold.TrainStart.Format("2006-01-02"),
			"train_end":        fold.Fold.TrainEnd.Format("2006-01-02"),
			"validation_start": fold.Fold.ValidationStart.Format("2006-01-02"),
			"validation_end":   fold.Fold.ValidationEnd.Format("2006-01-02"),
			"metrics":          metricsDocument(fold.Metrics),
		})
	}
	medianSharpe, err := canonical.Decimal12(evaluation.Assessment.MedianValidationSharpe)
	if err != nil {
		return nil, err
	}
	worstDrawdown, _ := canonical.Decimal12(evaluation.Assessment.WorstValidationDrawdown)
	medianTurnover, _ := canonical.Decimal12(evaluation.Assessment.MedianAnnualizedTurnover)
	return map[string]any{
		"aggregate": map[string]any{
			"median_annualized_turnover":        medianTurnover,
			"median_validation_sharpe":          medianSharpe,
			"worst_validation_maximum_drawdown": worstDrawdown,
		},
		"folds": folds,
	}, nil
}

func metricsDocument(summary metrics.Summary) map[string]any {
	value := func(number float64) string {
		formatted, _ := canonical.Decimal12(number)
		return formatted
	}
	pointer := func(number *float64) any {
		if number == nil {
			return nil
		}
		return value(*number)
	}
	nullReason := func(number *float64, reason string) any {
		if number != nil {
			return nil
		}
		if reason == "" {
			return "metric denominator is undefined"
		}
		return reason
	}
	return map[string]any{
		"annualized_cost_drag":              value(summary.AnnualizedCostDrag),
		"annualized_return":                 value(summary.AnnualizedReturn),
		"annualized_turnover":               value(summary.AnnualizedTurnover),
		"annualized_volatility":             pointer(summary.AnnualizedVolatility),
		"annualized_volatility_null_reason": nullReason(summary.AnnualizedVolatility, summary.VolatilityNullReason),
		"average_exposure":                  value(summary.AverageExposure),
		"cash_ratio":                        value(summary.CashRatio),
		"days":                              summary.Days,
		"maximum_accounting_residual":       value(summary.MaximumAccountingResidual),
		"maximum_drawdown":                  value(summary.MaximumDrawdown),
		"maximum_exposure":                  value(summary.MaximumExposure),
		"sharpe":                            pointer(summary.Sharpe),
		"sharpe_null_reason":                nullReason(summary.Sharpe, summary.SharpeNullReason),
		"sortino":                           pointer(summary.Sortino),
		"sortino_null_reason":               nullReason(summary.Sortino, summary.SortinoNullReason),
		"total_cost_ratio":                  value(summary.TotalCostRatio),
		"total_return":                      value(summary.TotalReturn),
	}
}

func candidateDocument(candidate Candidate) (any, error) {
	content, err := candidate.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	return document, nil
}

func bytesSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
