package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	projectdata "msbd5017/portfolio-proof/internal/data"
)

// CodexProposer sees only aggregate development-fold assessments. The frozen
// evaluator remains the authority for validation, costs, and ranking.
type CodexProposer struct {
	Context             context.Context
	Bridge              string
	CodexBin            string
	Model               string
	Timeout             time.Duration
	AllowManualFallback bool
	Calls               int
	Fallbacks           int
	Rejections          []string
}

type codexProposal struct {
	Parameters struct {
		Trend         TrendParameters `json:"trend"`
		MeanReversion struct {
			Lookback int     `json:"lookback"`
			EntryZ   float64 `json:"entry_z"`
			ExitZ    float64 `json:"exit_z"`
		} `json:"mean_reversion"`
		Allocator AllocatorParameters `json:"allocator"`
	} `json:"parameters"`
	Rationale string `json:"rationale"`
}

func (p *CodexProposer) Propose(history []CandidateAssessment) (Proposal, error) {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		proposal, err := p.proposeOnce(history)
		if err == nil {
			fmt.Fprintf(os.Stderr, "agent proposal %d accepted after %d attempt(s)\n", len(history)+1, attempt+1)
			return proposal, nil
		}
		last = err
		p.Rejections = append(p.Rejections, err.Error())
		if p.Context != nil && p.Context.Err() != nil {
			return Proposal{}, p.Context.Err()
		}
	}
	return Proposal{}, fmt.Errorf("agent proposal rejected after 5 attempts: %w", last)
}

func (p *CodexProposer) proposeOnce(history []CandidateAssessment) (Proposal, error) {
	if len(history) == 0 {
		return ManualProposer{}.Propose(history)
	}
	feedback := make([]any, 0, len(history))
	for _, assessment := range history {
		parameters, err := candidateDocument(assessment.Candidate)
		if err != nil {
			return Proposal{}, err
		}
		sharpe, err := canonical.Decimal12(assessment.MedianValidationSharpe)
		if err != nil {
			return Proposal{}, err
		}
		dd, err := canonical.Decimal12(assessment.WorstValidationDrawdown)
		if err != nil {
			return Proposal{}, err
		}
		turnover, err := canonical.Decimal12(assessment.MedianAnnualizedTurnover)
		if err != nil {
			return Proposal{}, err
		}
		feedback = append(feedback, map[string]any{
			"candidate_id": assessment.CandidateID, "parameters": parameters,
			"median_validation_sharpe":          sharpe,
			"worst_validation_maximum_drawdown": dd,
			"median_annualized_turnover":        turnover,
		})
	}
	seen := map[string]bool{}
	for _, assessment := range history {
		seen[assessment.CandidateID] = true
	}
	available := []Candidate{}
	for _, candidate := range CandidateSpace() {
		id, _ := candidate.ID()
		if !seen[id] {
			available = append(available, candidate)
		}
	}
	if len(available) == 0 {
		return Proposal{}, fmt.Errorf("candidate space exhausted")
	}
	count := 32
	if len(available) < count {
		count = len(available)
	}
	options := []any{}
	for index := 0; index < count; index++ {
		document, err := candidateDocument(available[index*len(available)/count])
		if err != nil {
			return Proposal{}, err
		}
		options = append(options, document)
	}
	input, err := canonical.JSON(map[string]any{"schema_version": "1.0.0", "history": feedback, "rejections": p.Rejections, "options": options})
	if err != nil {
		return Proposal{}, err
	}
	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	bridge := p.Bridge
	if bridge == "" {
		bridge = filepath.Join("scripts", "codex_propose.py")
	}
	args := []string{bridge, "--timeout", fmt.Sprint(int(timeout.Seconds()))}
	if p.CodexBin != "" {
		args = append(args, "--codex-bin", p.CodexBin)
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	command := exec.CommandContext(ctx, "python3", args...)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	p.Calls++
	if err := command.Run(); err != nil {
		return p.fallback(history, fmt.Errorf("Codex bridge: %w: %s", err, strings.TrimSpace(stderr.String())))
	}
	var answer codexProposal
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return p.fallback(history, fmt.Errorf("decode Codex proposal: %w", err))
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return p.fallback(history, fmt.Errorf("Codex proposal has trailing content"))
	}
	candidate := Candidate{Trend: answer.Parameters.Trend,
		MeanReversion: MeanReversionParameters{Lookback: answer.Parameters.MeanReversion.Lookback, EntryZ: answer.Parameters.MeanReversion.EntryZ, ExitZ: answer.Parameters.MeanReversion.ExitZ},
		Allocator:     answer.Parameters.Allocator}
	if err := candidate.Validate(); err != nil {
		return p.fallback(history, err)
	}
	if strings.TrimSpace(answer.Rationale) == "" || len(answer.Rationale) > 500 {
		return p.fallback(history, fmt.Errorf("Codex rationale is empty or too long"))
	}
	id, _ := candidate.ID()
	for _, previous := range history {
		if previous.CandidateID == id {
			return p.fallback(history, fmt.Errorf("Codex repeated an evaluated candidate"))
		}
	}
	return Proposal{Candidate: candidate, Proposer: "codex_app_server", Rationale: answer.Rationale}, nil
}

func (p *CodexProposer) fallback(history []CandidateAssessment, reason error) (Proposal, error) {
	if !p.AllowManualFallback {
		return Proposal{}, reason
	}
	proposal, err := (ManualProposer{}).Propose(history)
	if err != nil {
		return Proposal{}, err
	}
	p.Fallbacks++
	proposal.Proposer = "manual_deterministic_fallback"
	proposal.Rationale = reason.Error()
	return proposal, nil
}

type AgentSearchReport struct {
	SchemaVersion       string `json:"schema_version"`
	Status              string `json:"status"`
	Mode                string `json:"mode"`
	CandidatesEvaluated int    `json:"candidates_evaluated"`
	CodexCalls          int    `json:"codex_calls"`
	ManualFallbacks     int    `json:"manual_fallbacks"`
	SelectedCandidateID string `json:"selected_candidate_id"`
	FinalHoldoutOpened  bool   `json:"final_holdout_opened"`
	TraceSHA256         string `json:"trace_sha256"`
	FoldMetricsSHA256   string `json:"fold_metrics_sha256"`
	OutputDirectory     string `json:"output_directory"`
}

func RunAgentSearch(ctx context.Context, root string, budget int, proposer *CodexProposer) (AgentSearchReport, error) {
	if proposer == nil {
		return AgentSearchReport{}, fmt.Errorf("Codex proposer is required")
	}
	bars, err := projectdata.LoadDevelopment(root)
	if err != nil {
		return AgentSearchReport{}, err
	}
	config, err := LoadM3Config(root)
	if err != nil {
		return AgentSearchReport{}, err
	}
	if err := validateDevelopmentCoverage(bars, config); err != nil {
		return AgentSearchReport{}, err
	}
	proposer.Context = ctx
	evaluator := NewCandidateEvaluator(bars)
	search, err := RunSearch(ctx, evaluator, proposer, budget)
	if err != nil {
		return AgentSearchReport{}, err
	}
	trace, err := SearchTraceJSONL(search.Trace)
	if err != nil {
		return AgentSearchReport{}, err
	}
	folds, err := foldMetricsJSON(search.Evaluations)
	if err != nil {
		return AgentSearchReport{}, err
	}
	parent := filepath.Join(root, "artifacts", "development")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return AgentSearchReport{}, err
	}
	directory, err := os.MkdirTemp(parent, "agent_search_v2_")
	if err != nil {
		return AgentSearchReport{}, err
	}
	artifacts, err := agentArtifacts(ctx, evaluator, search, config)
	if err != nil {
		return AgentSearchReport{}, err
	}
	rejections, err := canonical.JSON(proposer.Rejections)
	if err != nil {
		return AgentSearchReport{}, err
	}
	artifacts["proposal_rejections.json"] = rejections
	artifacts["search_trace.jsonl"] = trace
	artifacts["fold_metrics.json"] = folds
	for name, content := range artifacts {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
			return AgentSearchReport{}, err
		}
	}
	report := AgentSearchReport{SchemaVersion: "1.0.0", Status: "completed", Mode: "development_only",
		CandidatesEvaluated: len(search.Evaluations), CodexCalls: proposer.Calls, ManualFallbacks: proposer.Fallbacks,
		SelectedCandidateID: search.Selected.Assessment.CandidateID, FinalHoldoutOpened: false,
		TraceSHA256: bytesSHA256(trace), FoldMetricsSHA256: bytesSHA256(folds), OutputDirectory: directory}
	content, err := canonical.JSON(report)
	if err != nil {
		return AgentSearchReport{}, err
	}
	if err := os.WriteFile(filepath.Join(directory, "summary.json"), content, 0o644); err != nil {
		return AgentSearchReport{}, err
	}
	return report, nil
}
