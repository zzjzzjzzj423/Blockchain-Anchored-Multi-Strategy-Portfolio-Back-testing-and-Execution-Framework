package evidence

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/merkle"
)

type VerifyReport struct {
	LeafCount     int    `json:"leaf_count"`
	MerkleRoot    string `json:"merkle_root"`
	ProofsValid   int    `json:"proofs_valid"`
	RunID         string `json:"run_id"`
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
}

// IntegrityError identifies the first bundle leaf whose bytes disagree with the manifest.
type IntegrityError struct {
	Path   string
	Reason string
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("integrity failure at %s: %s", e.Path, e.Reason)
}

func writeBundle(bundlePath string, files map[string][]byte, tree *merkle.Tree) error {
	if err := os.MkdirAll(bundlePath, 0o755); err != nil {
		return err
	}
	for name, content := range files {
		target := filepath.Join(bundlePath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
	}
	manifest := MerkleManifest{
		HashAlgorithm: "SHA-256", LeafCount: len(tree.Leaves), LeafDomain: "0x00",
		NodeDomain: "0x01", OddNodePolicy: "duplicate_last", Ordering: "relative_path_utf8_byte_order",
		Root: tree.RootHex(), SchemaVersion: SchemaVersion, Leaves: make([]LeafRecord, 0, len(tree.Leaves)),
	}
	proofDirectory := filepath.Join(bundlePath, "merkle", "proofs")
	if err := os.MkdirAll(proofDirectory, 0o755); err != nil {
		return err
	}
	for index, leaf := range tree.Leaves {
		proof, err := tree.Proof(index)
		if err != nil {
			return err
		}
		proofName := fmt.Sprintf("%04d.json", index)
		proofContent, err := canonical.JSON(proof)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(proofDirectory, proofName), proofContent, 0o644); err != nil {
			return err
		}
		manifest.Leaves = append(manifest.Leaves, LeafRecord{
			ContentLength: len(leaf.Content), ContentSHA256: hex.EncodeToString(leaf.ContentHash[:]),
			Index: index, LeafHash: hex.EncodeToString(leaf.Hash[:]), Path: leaf.Path,
			ProofPath: "merkle/proofs/" + proofName,
		})
	}
	content, err := canonical.JSON(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundlePath, "merkle", "merkle_manifest.json"), content, 0o644)
}

func VerifyBundle(bundlePath string) (VerifyReport, error) {
	files, err := ReadLeafFiles(bundlePath)
	if err != nil {
		return VerifyReport{}, err
	}
	return VerifyLeafFiles(bundlePath, files)
}

// ReadLeafFiles reads exactly the paths governed by the Merkle leaf policy.
func ReadLeafFiles(bundlePath string) (map[string][]byte, error) {
	return scanLeafFiles(bundlePath)
}

// VerifyLeafFiles verifies an in-memory leaf set against a bundle's immutable
// manifest and proofs. It is used by mutation tests without copying the bundle.
func VerifyLeafFiles(bundlePath string, files map[string][]byte) (VerifyReport, error) {
	manifestContent, err := os.ReadFile(filepath.Join(bundlePath, "merkle", "merkle_manifest.json"))
	if err != nil {
		return VerifyReport{}, err
	}
	var manifest MerkleManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestContent))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return VerifyReport{}, err
	}
	if manifest.SchemaVersion != SchemaVersion || manifest.HashAlgorithm != "SHA-256" || manifest.LeafDomain != "0x00" || manifest.NodeDomain != "0x01" || manifest.OddNodePolicy != "duplicate_last" {
		return VerifyReport{}, fmt.Errorf("unsupported Merkle manifest")
	}
	root, err := merkle.ParseHash(manifest.Root)
	if err != nil {
		return VerifyReport{}, err
	}
	if err := validateLeafPolicy(files); err != nil {
		return VerifyReport{}, err
	}
	tree, err := merkle.New(files)
	if err != nil {
		return VerifyReport{}, err
	}
	if len(tree.Leaves) != manifest.LeafCount || len(manifest.Leaves) != manifest.LeafCount {
		return VerifyReport{}, fmt.Errorf("bundle root or leaf count mismatch")
	}
	for index, record := range manifest.Leaves {
		leaf := tree.Leaves[index]
		if record.Index != index || record.Path != leaf.Path {
			return VerifyReport{}, &IntegrityError{Path: leaf.Path, Reason: "path or index mismatch"}
		}
		if record.ContentLength != len(leaf.Content) || record.ContentSHA256 != hex.EncodeToString(leaf.ContentHash[:]) || record.LeafHash != hex.EncodeToString(leaf.Hash[:]) {
			return VerifyReport{}, &IntegrityError{Path: leaf.Path, Reason: "content bytes do not match leaf manifest"}
		}
		proofPath := filepath.Join(bundlePath, filepath.FromSlash(record.ProofPath))
		proof, err := LoadProof(proofPath)
		if err != nil {
			return VerifyReport{}, err
		}
		if err := merkle.VerifyProof(leaf.Content, proof, root); err != nil {
			return VerifyReport{}, fmt.Errorf("proof %s: %w", record.ProofPath, err)
		}
	}
	if tree.Root() != root {
		return VerifyReport{}, fmt.Errorf("bundle root mismatch")
	}
	runID, err := bundleRunID(files["run_manifest.json"])
	if err != nil {
		return VerifyReport{}, err
	}
	wantRunID := computeRunID(
		files["manifests/data_manifest.json"], files["config/experiment_protocol.json"],
		files["config/selected_parameters.json"], files["manifests/source_manifest.json"],
	)
	if runID != wantRunID {
		return VerifyReport{}, fmt.Errorf("run ID does not match canonical identity inputs")
	}
	return VerifyReport{LeafCount: len(tree.Leaves), MerkleRoot: manifest.Root, ProofsValid: len(tree.Leaves), RunID: runID, SchemaVersion: SchemaVersion, Status: "passed"}, nil
}

// RegenerateDerivedFiles replaces the deterministic log and run manifest after
// result artifacts have been replayed.
func RegenerateDerivedFiles(files map[string][]byte, runID, sourceCommit string) error {
	delete(files, "logs/deterministic_events.jsonl")
	delete(files, "run_manifest.json")
	events, err := deterministicEvents(files)
	if err != nil {
		return err
	}
	files["logs/deterministic_events.jsonl"] = events
	manifest, err := runManifest(runID, sourceCommit, files)
	if err != nil {
		return err
	}
	files["run_manifest.json"] = manifest
	return nil
}

func validateLeafPolicy(files map[string][]byte) error {
	required := []string{
		"spec/SPEC.md", "data/LICENSE_DATA.md", "data/normalized/daily_ohlcv.csv",
		"config/experiment_protocol.json", "config/selected_parameters.json", "config/cost_model.json",
		"development/search_trace.jsonl", "manifests/data_manifest.json", "manifests/source_manifest.json",
		"manifests/reproducibility_profile.json", "results/base_strategy_signals.csv",
		"results/base_strategy_weights.csv", "results/base_strategy_returns.csv", "results/ensemble_weights.csv",
		"results/orders.csv", "results/trades.csv", "results/ledger.csv", "results/overall_metrics.json",
		"results/regime_metrics.json", "results/sensitivity.json", "logs/deterministic_events.jsonl", "run_manifest.json",
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			return fmt.Errorf("required Merkle leaf missing: %s", name)
		}
	}
	rawCount := 0
	for name := range files {
		if strings.HasPrefix(name, "data/raw/") {
			rawCount++
		}
		if strings.HasPrefix(name, "merkle/") || strings.HasPrefix(name, "chain/") {
			return fmt.Errorf("excluded path entered leaf set: %s", name)
		}
	}
	if rawCount == 0 {
		return fmt.Errorf("raw data leaves are missing")
	}
	return nil
}

func VerifyProofFile(bundlePath, proofPath string) error {
	manifestContent, err := os.ReadFile(filepath.Join(bundlePath, "merkle", "merkle_manifest.json"))
	if err != nil {
		return err
	}
	var manifest MerkleManifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		return err
	}
	root, err := merkle.ParseHash(manifest.Root)
	if err != nil {
		return err
	}
	proof, err := LoadProof(proofPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(filepath.Join(bundlePath, filepath.FromSlash(proof.Path)))
	if err != nil {
		return err
	}
	found := false
	for _, leaf := range manifest.Leaves {
		if leaf.Path == proof.Path {
			found = true
			if leaf.Index != proof.Index || leaf.ContentLength != int(proof.ContentLength) || leaf.ContentSHA256 != proof.ContentSHA256 || leaf.LeafHash != proof.LeafHash {
				return fmt.Errorf("proof metadata does not match Merkle manifest")
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("proof path is not a manifest leaf")
	}
	return merkle.VerifyProof(content, proof, root)
}

func FindProof(bundlePath, leafPath string) (string, error) {
	content, err := os.ReadFile(filepath.Join(bundlePath, "merkle", "merkle_manifest.json"))
	if err != nil {
		return "", err
	}
	var manifest MerkleManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return "", err
	}
	for _, leaf := range manifest.Leaves {
		if leaf.Path == leafPath {
			return filepath.Join(bundlePath, filepath.FromSlash(leaf.ProofPath)), nil
		}
	}
	return "", fmt.Errorf("leaf not found: %s", leafPath)
}

func LoadProof(name string) (merkle.Proof, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return merkle.Proof{}, err
	}
	var proof merkle.Proof
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return merkle.Proof{}, err
	}
	return proof, nil
}

func scanLeafFiles(bundlePath string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(bundlePath, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(bundlePath, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(relative, "merkle/") || strings.HasPrefix(relative, "chain/") || entry.Name() == ".DS_Store" {
			return nil
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files[relative] = content
		return nil
	})
	return files, err
}

func bundleRunID(content []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var document struct {
		RunID string `json:"run_id"`
	}
	if err := decoder.Decode(&document); err != nil || document.RunID == "" {
		return "", fmt.Errorf("invalid run manifest")
	}
	return document.RunID, nil
}
