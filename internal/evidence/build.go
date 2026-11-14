package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/merkle"
)

const SchemaVersion = "1.0.0"

type LeafRecord struct {
	ContentLength int    `json:"content_length"`
	ContentSHA256 string `json:"content_sha256"`
	Index         int    `json:"index"`
	LeafHash      string `json:"leaf_hash"`
	Path          string `json:"path"`
	ProofPath     string `json:"proof_path"`
}

type MerkleManifest struct {
	HashAlgorithm string       `json:"hash_algorithm"`
	LeafCount     int          `json:"leaf_count"`
	LeafDomain    string       `json:"leaf_domain"`
	Leaves        []LeafRecord `json:"leaves"`
	NodeDomain    string       `json:"node_domain"`
	OddNodePolicy string       `json:"odd_node_policy"`
	Ordering      string       `json:"ordering"`
	Root          string       `json:"root"`
	SchemaVersion string       `json:"schema_version"`
}

type BuildReport struct {
	AllProofsValid      bool     `json:"all_proofs_valid"`
	BundleRelativePath  string   `json:"bundle_relative_path"`
	DeterministicBuilds int      `json:"deterministic_builds"`
	LeafCount           int      `json:"leaf_count"`
	MerkleRoot          string   `json:"merkle_root"`
	RebuildRoots        []string `json:"rebuild_roots"`
	RunID               string   `json:"run_id"`
	SchemaVersion       string   `json:"schema_version"`
	SourceCommit        string   `json:"source_commit"`
	Status              string   `json:"status"`
}

func Build(ctx context.Context, root string, rebuilds int) (BuildReport, error) {
	if rebuilds != 10 {
		return BuildReport{}, fmt.Errorf("M6 gate requires exactly 10 deterministic builds")
	}
	clean, err := gitClean(root)
	if err != nil || !clean {
		return BuildReport{}, fmt.Errorf("M6 build requires a clean Git tree")
	}
	if _, err := os.Stat(filepath.Join(root, "reports", "m6_evidence_bundle.json")); err == nil {
		return BuildReport{}, fmt.Errorf("M6 report already exists")
	} else if !os.IsNotExist(err) {
		return BuildReport{}, err
	}
	roots := make([]string, 0, rebuilds)
	var files map[string][]byte
	var runID, sourceCommit string
	var tree *merkle.Tree
	for index := 0; index < rebuilds; index++ {
		builtFiles, builtRunID, builtSourceCommit, buildErr := assembleFiles(ctx, root)
		if buildErr != nil {
			return BuildReport{}, buildErr
		}
		if index == 0 {
			files, runID, sourceCommit = builtFiles, builtRunID, builtSourceCommit
		} else if builtRunID != runID || builtSourceCommit != sourceCommit {
			return BuildReport{}, fmt.Errorf("canonical identity changed at build %d", index+1)
		} else {
			files = builtFiles
		}
		tree, err = merkle.New(files)
		if err != nil {
			return BuildReport{}, err
		}
		roots = append(roots, tree.RootHex())
		if roots[index] != roots[0] {
			return BuildReport{}, fmt.Errorf("Merkle root changed at build %d", index+1)
		}
	}
	bundleRelative := filepath.ToSlash(filepath.Join("artifacts", runID))
	bundlePath := filepath.Join(root, filepath.FromSlash(bundleRelative))
	if _, err := os.Stat(bundlePath); err == nil {
		return BuildReport{}, fmt.Errorf("bundle already exists: %s", bundleRelative)
	} else if !os.IsNotExist(err) {
		return BuildReport{}, err
	}
	if err := writeBundle(bundlePath, files, tree); err != nil {
		return BuildReport{}, err
	}
	if _, err := VerifyBundle(bundlePath); err != nil {
		return BuildReport{}, fmt.Errorf("verify built bundle: %w", err)
	}
	report := BuildReport{
		AllProofsValid: true, BundleRelativePath: bundleRelative, DeterministicBuilds: rebuilds,
		LeafCount: len(tree.Leaves), MerkleRoot: tree.RootHex(), RebuildRoots: roots,
		RunID: runID, SchemaVersion: SchemaVersion, SourceCommit: sourceCommit, Status: "passed",
	}
	content, err := canonical.JSON(report)
	if err != nil {
		return BuildReport{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "reports", "m6_evidence_bundle.json"), content, 0o644); err != nil {
		return BuildReport{}, err
	}
	return report, nil
}

func assembleFiles(ctx context.Context, root string) (map[string][]byte, string, string, error) {
	read := func(relative string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	}
	dataManifest, err := read("data/data_manifest.json")
	if err != nil {
		return nil, "", "", err
	}
	selected, err := read("configs/selected_parameters.json")
	if err != nil {
		return nil, "", "", err
	}
	protocol, err := experimentProtocol(root)
	if err != nil {
		return nil, "", "", err
	}
	sourceManifest, sourceCommit, err := sourceManifest(ctx, root)
	if err != nil {
		return nil, "", "", err
	}
	runID := computeRunID(dataManifest, protocol, selected, sourceManifest)
	profile, err := reproducibilityProfile()
	if err != nil {
		return nil, "", "", err
	}
	files := map[string][]byte{
		"config/experiment_protocol.json": protocol, "config/selected_parameters.json": selected,
		"manifests/data_manifest.json": dataManifest, "manifests/source_manifest.json": sourceManifest,
		"manifests/reproducibility_profile.json": profile,
	}
	for target, source := range map[string]string{
		"spec/SPEC.md": "SPEC.md", "data/LICENSE_DATA.md": "data/LICENSE_DATA.md",
		"data/normalized/daily_ohlcv.csv": "data/normalized/daily_ohlcv.csv",
		"config/cost_model.json":          "configs/costs.json",
		"development/search_trace.jsonl":  "artifacts/development/walk_forward/search_trace.jsonl",
	} {
		content, err := read(source)
		if err != nil {
			return nil, "", "", err
		}
		files[target] = content
	}
	if err := addRawFiles(root, files); err != nil {
		return nil, "", "", err
	}
	if err := addResultFiles(root, files); err != nil {
		return nil, "", "", err
	}
	events, err := deterministicEvents(files)
	if err != nil {
		return nil, "", "", err
	}
	files["logs/deterministic_events.jsonl"] = events
	runManifest, err := runManifest(runID, sourceCommit, files)
	if err != nil {
		return nil, "", "", err
	}
	files["run_manifest.json"] = runManifest
	return files, runID, sourceCommit, nil
}

func experimentProtocol(root string) ([]byte, error) {
	documents := make(map[string]any)
	for name, relative := range map[string]string{
		"cost_model": "configs/costs.json", "data": "configs/data.json",
		"final_experiment": "configs/final_experiment.json", "strategies": "configs/strategies.json",
	} {
		value, err := readJSON(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return nil, err
		}
		documents[name] = value
	}
	return canonical.JSON(map[string]any{
		"annualization_days": 365, "documents": documents, "schema_version": SchemaVersion,
		"spec_version": "1.0.0-frozen", "time_semantics": "signal[d] uses data through close[d]; target[d] executes on d+1; realized return is weight[d-1] * return[d]",
	})
}

func sourceManifest(ctx context.Context, root string) ([]byte, string, error) {
	commit, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, "", err
	}
	m5, err := readJSON(filepath.Join(root, "reports", "m5_run_log.json"))
	if err != nil {
		return nil, "", err
	}
	m5Document := m5.(map[string]any)
	formalCommit, _ := m5Document["source_commit"].(string)
	wasm := make(map[string]string)
	for _, name := range []string{"buyhold", "meanreversion", "trend"} {
		content, err := os.ReadFile(filepath.Join(root, "bin", "strategies", name+".wasm"))
		if err != nil {
			return nil, "", fmt.Errorf("read %s WASM: %w", name, err)
		}
		wasm[name] = digest(content)
	}
	verifierHash, err := hashSourceTree(root, []string{"internal/canonical", "internal/evidence", "internal/merkle"})
	if err != nil {
		return nil, "", err
	}
	goSum := nullableFileHash(root, "go.sum", "not present")
	packageLock := nullableFileHash(root, "package-lock.json", "Node package lock not used")
	contractSource := map[string]any{"sha256": nil, "null_reason": "M7 contract source not yet implemented"}
	versions := map[string]any{
		"go": commandVersion(ctx, "go", "version"), "node": commandVersion(ctx, "node", "--version"),
		"solidity": map[string]any{"value": nil, "null_reason": "M7 Solidity toolchain not required for M6"},
	}
	content, err := canonical.JSON(map[string]any{
		"contract_source": contractSource, "evidence_builder_commit": commit,
		"formal_run_commit": formalCommit, "git_commit": commit, "git_tree_clean": true,
		"go_sum": goSum, "package_lock": packageLock, "schema_version": SchemaVersion,
		"strategy_wasm_sha256": wasm, "tool_versions": versions, "verifier_source_sha256": verifierHash,
	})
	return content, commit, err
}

func reproducibilityProfile() ([]byte, error) {
	return canonical.JSON(map[string]any{
		"architecture": runtime.GOARCH, "cgo_enabled": false, "float": "IEEE-754 binary64",
		"line_endings": "LF", "numeric_output": "12 decimal places, round-half-to-even",
		"operating_system": runtime.GOOS, "random_seed": 5017, "schema_version": SchemaVersion,
		"text_encoding": "UTF-8 NFC", "timezone": "UTC",
	})
}

func addRawFiles(root string, files map[string][]byte) error {
	rawRoot := filepath.Join(root, "data", "raw")
	return filepath.WalkDir(rawRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == ".gitkeep" {
			return nil
		}
		relative, err := filepath.Rel(rawRoot, name)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files["data/raw/"+filepath.ToSlash(relative)] = content
		return nil
	})
}

func addResultFiles(root string, files map[string][]byte) error {
	directory := filepath.Join(root, "artifacts", "final_holdout")
	mapping := map[string]string{
		"base_signals.csv":              "results/base_strategy_signals.csv",
		"base_executed_weights.csv":     "results/base_strategy_weights.csv",
		"base_target_weights.csv":       "results/base_strategy_target_weights.csv",
		"base_returns.csv":              "results/base_strategy_returns.csv",
		"ensemble_signals.csv":          "results/ensemble_signals.csv",
		"ensemble_executed_weights.csv": "results/ensemble_asset_weights.csv",
		"ensemble_target_weights.csv":   "results/ensemble_asset_target_weights.csv",
		"ensemble_returns.csv":          "results/ensemble_returns.csv",
		"ensemble_strategy_weights.csv": "results/ensemble_weights.csv",
		"overall_metrics.json":          "results/overall_metrics.json", "regime_metrics.json": "results/regime_metrics.json",
		"regime_labels.csv": "results/regime_labels.csv", "sensitivity.json": "results/sensitivity.json",
	}
	for source, target := range mapping {
		content, err := os.ReadFile(filepath.Join(directory, source))
		if err != nil {
			return err
		}
		files[target] = content
	}
	for _, item := range []struct{ output, base, ensemble string }{
		{"results/orders.csv", "base_orders.csv", "ensemble_orders.csv"},
		{"results/trades.csv", "base_trades.csv", "ensemble_trades.csv"},
		{"results/ledger.csv", "base_ledger.csv", "ensemble_ledger.csv"},
	} {
		merged, err := mergeCSV(filepath.Join(directory, item.base), filepath.Join(directory, item.ensemble))
		if err != nil {
			return err
		}
		files[item.output] = merged
	}
	return nil
}

func mergeCSV(paths ...string) ([]byte, error) {
	var header []string
	var rows [][]string
	for _, name := range paths {
		file, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		records, err := csv.NewReader(file).ReadAll()
		_ = file.Close()
		if err != nil || len(records) == 0 {
			return nil, fmt.Errorf("read CSV %s: %w", name, err)
		}
		if header == nil {
			header = records[0]
		} else if strings.Join(header, "\x00") != strings.Join(records[0], "\x00") {
			return nil, fmt.Errorf("CSV header mismatch")
		}
		rows = append(rows, records[1:]...)
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[name] = index
	}
	get := func(row []string, name string) string {
		if index, ok := columns[name]; ok {
			return row[index]
		}
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		for _, name := range []string{"date", "phase_order", "strategy", "symbol", "side", "sequence"} {
			if get(left, name) != get(right, name) {
				return get(left, name) < get(right, name)
			}
		}
		return strings.Join(left, "\x00") < strings.Join(right, "\x00")
	})
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write(header)
	_ = writer.WriteAll(rows)
	writer.Flush()
	return output.Bytes(), writer.Error()
}

func runManifest(runID, sourceCommit string, files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	entries := make([]any, 0, len(paths))
	for _, name := range paths {
		entries = append(entries, map[string]any{"content_length": len(files[name]), "content_sha256": digest(files[name]), "path": name})
	}
	return canonical.JSON(map[string]any{
		"files_excluding_run_manifest": entries, "leaf_policy": "SPEC 15.1/15.2",
		"run_id": runID, "schema_version": SchemaVersion, "source_commit": sourceCommit,
	})
}

func readJSON(name string) (any, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func digest(content []byte) string {
	value := sha256.Sum256(content)
	return hex.EncodeToString(value[:])
}

func computeRunID(dataManifest, protocol, selected, sourceManifest []byte) string {
	hash := sha256.New()
	for _, content := range [][]byte{dataManifest, protocol, selected, sourceManifest} {
		_, _ = hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func nullableFileHash(root, relative, reason string) map[string]any {
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		return map[string]any{"sha256": nil, "null_reason": reason}
	}
	return map[string]any{"sha256": digest(content), "null_reason": nil}
}

func hashSourceTree(root string, directories []string) (string, error) {
	var names []string
	for _, directory := range directories {
		err := filepath.WalkDir(filepath.Join(root, directory), func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && strings.HasSuffix(name, ".go") {
				names = append(names, name)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		relative, _ := filepath.Rel(root, name)
		content, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(relative)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func commandVersion(ctx context.Context, name string, arguments ...string) map[string]any {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return map[string]any{"value": nil, "null_reason": name + " unavailable"}
	}
	return map[string]any{"value": strings.TrimSpace(string(output)), "null_reason": nil}
}

func gitClean(root string) (bool, error) {
	output, err := gitOutput(root, "status", "--porcelain")
	return output == "", err
}

func gitOutput(root string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
