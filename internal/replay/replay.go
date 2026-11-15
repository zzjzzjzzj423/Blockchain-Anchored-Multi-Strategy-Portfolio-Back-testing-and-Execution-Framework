package replay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/evidence"
	"msbd5017/portfolio-proof/internal/experiment"
	"msbd5017/portfolio-proof/internal/merkle"
)

const SchemaVersion = "1.0.0"

type ReplayIteration struct {
	ArtifactByteIdentical bool   `json:"artifact_byte_identical"`
	BacktestMicroseconds  int64  `json:"backtest_microseconds"`
	EvidenceMicroseconds  int64  `json:"evidence_microseconds"`
	Index                 int    `json:"index"`
	MerkleRoot            string `json:"merkle_root"`
	TotalMicroseconds     int64  `json:"total_microseconds"`
}

type ReplayReport struct {
	ArtifactIdenticalRuns int               `json:"artifact_identical_runs"`
	BaselineBundleValid   bool              `json:"baseline_bundle_valid"`
	BundleSourceCommit    string            `json:"bundle_source_commit"`
	FirstDifference       *Difference       `json:"first_difference"`
	Iterations            []ReplayIteration `json:"iterations"`
	LeafCount             int               `json:"leaf_count"`
	NetworkCalls          int               `json:"network_calls"`
	AgentCalls            int               `json:"agent_calls"`
	ReplayCount           int               `json:"replay_count"`
	ReplayVerifierCommit  string            `json:"replay_verifier_commit"`
	RootIdenticalRuns     int               `json:"root_identical_runs"`
	RunID                 string            `json:"run_id"`
	SchemaVersion         string            `json:"schema_version"`
	SourceIdentityValid   bool              `json:"source_identity_valid"`
	Status                string            `json:"status"`
	ToolchainValid        bool              `json:"toolchain_valid"`
}

type MutationCase struct {
	ByteOffset      int    `json:"byte_offset"`
	Detected        bool   `json:"detected"`
	DetectedPath    string `json:"detected_path"`
	Index           int    `json:"index"`
	MutatedPath     string `json:"mutated_path"`
	OriginalByteHex string `json:"original_byte_hex"`
	MutatedByteHex  string `json:"mutated_byte_hex"`
}

type MutationReport struct {
	BaselineValid       bool           `json:"baseline_valid"`
	Count               int            `json:"count"`
	Detected            int            `json:"detected"`
	Localized           int            `json:"localized"`
	Mutations           []MutationCase `json:"mutations"`
	PostTestBundleValid bool           `json:"post_test_bundle_valid"`
	SchemaVersion       string         `json:"schema_version"`
	Seed                int64          `json:"seed"`
	Status              string         `json:"status"`
}

type OverheadReport struct {
	AverageBacktestMicroseconds int64            `json:"average_backtest_microseconds"`
	AverageEvidenceMicroseconds int64            `json:"average_evidence_microseconds"`
	AverageTotalMicroseconds    int64            `json:"average_total_microseconds"`
	EvidenceOverheadPercent     string           `json:"evidence_overhead_percent"`
	SchemaVersion               string           `json:"schema_version"`
	StorageBytes                map[string]int64 `json:"storage_bytes"`
}

type M8Result struct {
	Coverage CoverageReport
	Mutation MutationReport
	Overhead OverheadReport
	Replay   ReplayReport
}

type CoverageReport struct {
	CoreDefinition string            `json:"core_definition"`
	MinimumPercent string            `json:"minimum_percent"`
	Packages       map[string]string `json:"packages"`
	SchemaVersion  string            `json:"schema_version"`
	Status         string            `json:"status"`
	Threshold      string            `json:"threshold_percent"`
}

type Difference struct {
	ActualByteHex   string `json:"actual_byte_hex"`
	ByteOffset      int    `json:"byte_offset"`
	ExpectedByteHex string `json:"expected_byte_hex"`
	Path            string `json:"path"`
}

func RunM8(ctx context.Context, root, bundlePath string, replayCount, mutationCount int, seed int64) (M8Result, error) {
	if replayCount != 10 || mutationCount != 100 || seed != 5017 {
		return M8Result{}, fmt.Errorf("M8 gate requires replays=10 mutations=100 seed=5017")
	}
	clean, err := gitClean(root)
	if err != nil || !clean {
		return M8Result{}, fmt.Errorf("M8 formal run requires a clean Git tree")
	}
	replayCommit, err := gitRevision(root)
	if err != nil {
		return M8Result{}, err
	}
	for _, name := range []string{"m8_replay.json", "m8_mutation.json", "m8_overhead.json", "m8_coverage.json"} {
		if _, err := os.Stat(filepath.Join(root, "reports", name)); err == nil {
			return M8Result{}, fmt.Errorf("M8 report already exists: %s", name)
		} else if !os.IsNotExist(err) {
			return M8Result{}, err
		}
	}
	verified, err := evidence.VerifyBundle(bundlePath)
	if err != nil {
		return M8Result{}, err
	}
	expected, err := evidence.ReadLeafFiles(bundlePath)
	if err != nil {
		return M8Result{}, err
	}
	runID, sourceCommit, err := manifestIdentity(expected["run_manifest.json"])
	if err != nil || runID != verified.RunID {
		return M8Result{}, fmt.Errorf("invalid replay identity: %w", err)
	}
	sourceValid, toolchainValid := validateSourceIdentity(expected["manifests/source_manifest.json"])
	if !sourceValid || !toolchainValid {
		return M8Result{}, fmt.Errorf("source identity or Go toolchain mismatch")
	}

	replayReport := ReplayReport{
		BaselineBundleValid: true, BundleSourceCommit: sourceCommit, Iterations: make([]ReplayIteration, 0, replayCount), LeafCount: verified.LeafCount,
		ReplayCount: replayCount, ReplayVerifierCommit: replayCommit, RunID: runID, SchemaVersion: SchemaVersion, SourceIdentityValid: true,
		Status: "passed", ToolchainValid: true,
	}
	var backtestTotal, evidenceTotal, total int64
	for index := 0; index < replayCount; index++ {
		started := time.Now()
		workspace, err := prepareWorkspace(expected)
		if err != nil {
			return M8Result{}, err
		}
		backtestStarted := time.Now()
		artifacts, runErr := experiment.ReplayFinalArtifacts(ctx, workspace)
		backtestElapsed := time.Since(backtestStarted).Microseconds()
		_ = os.RemoveAll(workspace)
		if runErr != nil {
			return M8Result{}, runErr
		}
		evidenceStarted := time.Now()
		actual, err := replayLeafSet(expected, artifacts, runID, sourceCommit)
		if err != nil {
			return M8Result{}, err
		}
		difference := FirstDifference(expected, actual)
		tree, err := merkle.New(actual)
		if err != nil {
			return M8Result{}, err
		}
		evidenceElapsed := time.Since(evidenceStarted).Microseconds()
		iteration := ReplayIteration{ArtifactByteIdentical: difference == nil, BacktestMicroseconds: backtestElapsed,
			EvidenceMicroseconds: evidenceElapsed, Index: index + 1, MerkleRoot: tree.RootHex(), TotalMicroseconds: time.Since(started).Microseconds()}
		replayReport.Iterations = append(replayReport.Iterations, iteration)
		if difference != nil && replayReport.FirstDifference == nil {
			replayReport.FirstDifference = difference
		}
		if difference == nil {
			replayReport.ArtifactIdenticalRuns++
		}
		if tree.RootHex() == verified.MerkleRoot {
			replayReport.RootIdenticalRuns++
		}
		backtestTotal += backtestElapsed
		evidenceTotal += evidenceElapsed
		total += iteration.TotalMicroseconds
	}
	if replayReport.ArtifactIdenticalRuns != replayCount || replayReport.RootIdenticalRuns != replayCount {
		replayReport.Status = "failed"
		return M8Result{}, fmt.Errorf("replay byte/root identity gate failed")
	}
	mutationReport, err := RunMutations(bundlePath, expected, mutationCount, seed)
	if err != nil {
		return M8Result{}, err
	}
	storage, err := storageBytes(bundlePath)
	if err != nil {
		return M8Result{}, err
	}
	averageBacktest := backtestTotal / int64(replayCount)
	averageEvidence := evidenceTotal / int64(replayCount)
	overheadPercent := 0.0
	if averageBacktest > 0 {
		overheadPercent = 100 * float64(averageEvidence) / float64(averageBacktest)
	}
	overheadText, _ := canonical.Decimal12(overheadPercent)
	overhead := OverheadReport{AverageBacktestMicroseconds: averageBacktest, AverageEvidenceMicroseconds: averageEvidence,
		AverageTotalMicroseconds: total / int64(replayCount), EvidenceOverheadPercent: overheadText,
		SchemaVersion: SchemaVersion, StorageBytes: storage}
	coverage, err := MeasureCoreCoverage(ctx, root)
	if err != nil {
		return M8Result{}, err
	}
	result := M8Result{Coverage: coverage, Mutation: mutationReport, Overhead: overhead, Replay: replayReport}
	for name, value := range map[string]any{"m8_replay.json": replayReport, "m8_mutation.json": mutationReport, "m8_overhead.json": overhead, "m8_coverage.json": coverage} {
		content, err := canonical.JSON(value)
		if err != nil {
			return M8Result{}, err
		}
		if err := os.WriteFile(filepath.Join(root, "reports", name), content, 0o644); err != nil {
			return M8Result{}, err
		}
	}
	return result, nil
}

// MeasureCoreCoverage applies the proposal's >=85% gate to deterministic
// calculation and cryptographic primitives. Filesystem/network/report
// orchestration is covered by the full replay and mutation integration gates.
func MeasureCoreCoverage(ctx context.Context, root string) (CoverageReport, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return CoverageReport{}, err
	}
	root = absoluteRoot
	packages := []string{
		"./internal/canonical", "./internal/execution", "./internal/merkle", "./internal/metrics",
		"./internal/portfolio", "./internal/strategy", "./strategies/buyhold",
		"./strategies/meanreversion", "./strategies/trend",
	}
	goBinary := filepath.Join(root, ".tools", "go", "bin", "go")
	if _, err := os.Stat(goBinary); err != nil {
		goBinary = "go"
	}
	report := CoverageReport{CoreDefinition: "deterministic calculation, accounting, strategy, canonical serialization, and Merkle primitives",
		Packages: make(map[string]string, len(packages)), SchemaVersion: SchemaVersion, Status: "passed", Threshold: "85.000000000000"}
	minimum := 100.0
	for _, pkg := range packages {
		command := exec.CommandContext(ctx, goBinary, "test", "-count=1", "-cover", pkg)
		command.Dir = root
		command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".cache", "go-build"), "GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"), "GOTELEMETRY=off", "GOTOOLCHAIN=local")
		output, err := command.CombinedOutput()
		if err != nil {
			return CoverageReport{}, fmt.Errorf("coverage %s: %s", pkg, strings.TrimSpace(string(output)))
		}
		text := string(output)
		marker := "coverage: "
		position := strings.LastIndex(text, marker)
		if position < 0 {
			return CoverageReport{}, fmt.Errorf("coverage percentage missing for %s", pkg)
		}
		valueText := strings.Fields(text[position+len(marker):])[0]
		value, err := strconv.ParseFloat(strings.TrimSuffix(valueText, "%"), 64)
		if err != nil {
			return CoverageReport{}, err
		}
		if value < minimum {
			minimum = value
		}
		formatted, _ := canonical.Decimal12(value)
		report.Packages[strings.TrimPrefix(pkg, "./")] = formatted
	}
	report.MinimumPercent, _ = canonical.Decimal12(minimum)
	if minimum < 85 {
		report.Status = "failed"
		return report, fmt.Errorf("core coverage %.1f%% is below 85%%", minimum)
	}
	return report, nil
}

func RunMutations(bundlePath string, baseline map[string][]byte, count int, seed int64) (MutationReport, error) {
	if _, err := evidence.VerifyLeafFiles(bundlePath, baseline); err != nil {
		return MutationReport{}, err
	}
	paths := make([]string, 0, len(baseline))
	for path, content := range baseline {
		if len(content) > 0 {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	random := rand.New(rand.NewSource(seed))
	report := MutationReport{BaselineValid: true, Count: count, Mutations: make([]MutationCase, 0, count), SchemaVersion: SchemaVersion, Seed: seed, Status: "passed"}
	for index := 0; index < count; index++ {
		path := paths[random.Intn(len(paths))]
		offset := random.Intn(len(baseline[path]))
		mutated := make(map[string][]byte, len(baseline))
		for name, content := range baseline {
			mutated[name] = content
		}
		changed := append([]byte(nil), baseline[path]...)
		original := changed[offset]
		changed[offset] ^= 0x01
		mutated[path] = changed
		_, verifyErr := evidence.VerifyLeafFiles(bundlePath, mutated)
		var integrity *evidence.IntegrityError
		detected := verifyErr != nil
		detectedPath := ""
		if errors.As(verifyErr, &integrity) {
			detectedPath = integrity.Path
		}
		localized := detectedPath == path
		if detected {
			report.Detected++
		}
		if localized {
			report.Localized++
		}
		report.Mutations = append(report.Mutations, MutationCase{ByteOffset: offset, Detected: detected, DetectedPath: detectedPath,
			Index: index + 1, MutatedPath: path, OriginalByteHex: fmt.Sprintf("%02x", original), MutatedByteHex: fmt.Sprintf("%02x", changed[offset])})
	}
	_, err := evidence.VerifyLeafFiles(bundlePath, baseline)
	report.PostTestBundleValid = err == nil
	if report.Detected != count || report.Localized != count || !report.PostTestBundleValid {
		report.Status = "failed"
		return report, fmt.Errorf("mutation gate failed: detected=%d localized=%d", report.Detected, report.Localized)
	}
	return report, nil
}

func FirstDifference(expected, actual map[string][]byte) *Difference {
	paths := make(map[string]struct{}, len(expected)+len(actual))
	for path := range expected {
		paths[path] = struct{}{}
	}
	for path := range actual {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		left, leftOK := expected[path]
		right, rightOK := actual[path]
		if !leftOK || !rightOK {
			return &Difference{Path: path, ByteOffset: 0, ExpectedByteHex: presenceByte(leftOK), ActualByteHex: presenceByte(rightOK)}
		}
		if bytes.Equal(left, right) {
			continue
		}
		offset := 0
		for offset < len(left) && offset < len(right) && left[offset] == right[offset] {
			offset++
		}
		return &Difference{Path: path, ByteOffset: offset, ExpectedByteHex: byteAt(left, offset), ActualByteHex: byteAt(right, offset)}
	}
	return nil
}

func prepareWorkspace(files map[string][]byte) (string, error) {
	workspace, err := os.MkdirTemp("", "portfolio-replay-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) { _ = os.RemoveAll(workspace); return "", err }
	var protocol struct {
		Documents map[string]json.RawMessage `json:"documents"`
	}
	if err := json.Unmarshal(files["config/experiment_protocol.json"], &protocol); err != nil {
		return fail(err)
	}
	writes := map[string][]byte{
		"SPEC.md": files["spec/SPEC.md"], "configs/selected_parameters.json": files["config/selected_parameters.json"],
		"configs/costs.json": files["config/cost_model.json"], "data/normalized/daily_ohlcv.csv": files["data/normalized/daily_ohlcv.csv"],
	}
	for key, target := range map[string]string{"data": "configs/data.json", "final_experiment": "configs/final_experiment.json", "strategies": "configs/strategies.json"} {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(protocol.Documents[key]))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return fail(err)
		}
		content, err := canonical.JSON(value)
		if err != nil {
			return fail(err)
		}
		writes[target] = content
	}
	for relative, content := range writes {
		target := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return fail(err)
		}
	}
	return workspace, nil
}

func replayLeafSet(expected map[string][]byte, artifacts map[string][]byte, runID, sourceCommit string) (map[string][]byte, error) {
	files := make(map[string][]byte, len(expected))
	for path, content := range expected {
		if !strings.HasPrefix(path, "results/") && path != "logs/deterministic_events.jsonl" && path != "run_manifest.json" {
			files[path] = content
		}
	}
	results, err := resultFiles(artifacts)
	if err != nil {
		return nil, err
	}
	for path, content := range results {
		files[path] = content
	}
	if err := evidence.RegenerateDerivedFiles(files, runID, sourceCommit); err != nil {
		return nil, err
	}
	return files, nil
}

func resultFiles(artifacts map[string][]byte) (map[string][]byte, error) {
	mapping := map[string]string{
		"base_signals.csv": "results/base_strategy_signals.csv", "base_executed_weights.csv": "results/base_strategy_weights.csv",
		"base_target_weights.csv": "results/base_strategy_target_weights.csv", "base_returns.csv": "results/base_strategy_returns.csv",
		"ensemble_signals.csv": "results/ensemble_signals.csv", "ensemble_executed_weights.csv": "results/ensemble_asset_weights.csv",
		"ensemble_target_weights.csv": "results/ensemble_asset_target_weights.csv", "ensemble_returns.csv": "results/ensemble_returns.csv",
		"ensemble_strategy_weights.csv": "results/ensemble_weights.csv", "overall_metrics.json": "results/overall_metrics.json",
		"regime_metrics.json": "results/regime_metrics.json", "regime_labels.csv": "results/regime_labels.csv", "sensitivity.json": "results/sensitivity.json",
	}
	result := make(map[string][]byte, len(mapping)+3)
	for source, target := range mapping {
		content, ok := artifacts[source]
		if !ok {
			return nil, fmt.Errorf("replay artifact missing: %s", source)
		}
		result[target] = content
	}
	for _, item := range []struct{ target, base, ensemble string }{{"results/orders.csv", "base_orders.csv", "ensemble_orders.csv"}, {"results/trades.csv", "base_trades.csv", "ensemble_trades.csv"}, {"results/ledger.csv", "base_ledger.csv", "ensemble_ledger.csv"}} {
		merged, err := mergeCSV(artifacts[item.base], artifacts[item.ensemble])
		if err != nil {
			return nil, err
		}
		result[item.target] = merged
	}
	return result, nil
}

func mergeCSV(contents ...[]byte) ([]byte, error) {
	var header []string
	var rows [][]string
	for _, content := range contents {
		records, err := csv.NewReader(bytes.NewReader(content)).ReadAll()
		if err != nil || len(records) == 0 {
			return nil, fmt.Errorf("read replay CSV: %w", err)
		}
		if header == nil {
			header = records[0]
		} else if strings.Join(header, "\x00") != strings.Join(records[0], "\x00") {
			return nil, fmt.Errorf("replay CSV header mismatch")
		}
		rows = append(rows, records[1:]...)
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[name] = i
	}
	get := func(row []string, name string) string {
		if i, ok := columns[name]; ok {
			return row[i]
		}
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		for _, name := range []string{"date", "phase_order", "strategy", "symbol", "side", "sequence"} {
			if get(rows[i], name) != get(rows[j], name) {
				return get(rows[i], name) < get(rows[j], name)
			}
		}
		return strings.Join(rows[i], "\x00") < strings.Join(rows[j], "\x00")
	})
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write(header)
	_ = writer.WriteAll(rows)
	writer.Flush()
	return output.Bytes(), writer.Error()
}

func manifestIdentity(content []byte) (string, string, error) {
	var document struct {
		RunID        string `json:"run_id"`
		SourceCommit string `json:"source_commit"`
	}
	if err := json.Unmarshal(content, &document); err != nil || document.RunID == "" || document.SourceCommit == "" {
		return "", "", fmt.Errorf("invalid run manifest")
	}
	return document.RunID, document.SourceCommit, nil
}

func validateSourceIdentity(content []byte) (bool, bool) {
	var document struct {
		GitTreeClean bool   `json:"git_tree_clean"`
		GitCommit    string `json:"git_commit"`
		Verifier     string `json:"verifier_source_sha256"`
		ToolVersions struct {
			Go struct {
				Value *string `json:"value"`
			} `json:"go"`
		} `json:"tool_versions"`
	}
	if json.Unmarshal(content, &document) != nil {
		return false, false
	}
	source := document.GitTreeClean && document.GitCommit != "" && document.Verifier != ""
	toolchain := document.ToolVersions.Go.Value != nil && strings.Contains(*document.ToolVersions.Go.Value, runtime.Version())
	return source, toolchain
}

func storageBytes(bundlePath string) (map[string]int64, error) {
	result := map[string]int64{"leaf_files": 0, "merkle": 0, "chain": 0, "total": 0}
	err := filepath.WalkDir(bundlePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(bundlePath, path)
		key := "leaf_files"
		if strings.HasPrefix(filepath.ToSlash(relative), "merkle/") {
			key = "merkle"
		} else if strings.HasPrefix(filepath.ToSlash(relative), "chain/") {
			key = "chain"
		}
		result[key] += info.Size()
		result["total"] += info.Size()
		return nil
	})
	return result, err
}

func byteAt(content []byte, offset int) string {
	if offset >= len(content) {
		return "<eof>"
	}
	return fmt.Sprintf("%02x", content[offset])
}
func presenceByte(present bool) string {
	if present {
		return "<present>"
	}
	return "<missing>"
}
func digest(content []byte) string { sum := sha256.Sum256(content); return hex.EncodeToString(sum[:]) }

func gitClean(root string) (bool, error) {
	command := exec.Command("git", "-C", root, "status", "--porcelain")
	output, err := command.Output()
	return strings.TrimSpace(string(output)) == "", err
}

func gitRevision(root string) (string, error) {
	command := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
