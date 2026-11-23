package evidence

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"msbd5017/portfolio-proof/internal/merkle"
)

// PackV2 uses the frozen v1 Merkle codec; only the evidence leaf policy changes.
// Proofs are still verifiable by the existing independent proof verifier.
func PackV2(source, destination string) (VerifyReport, error) {
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		return VerifyReport{}, fmt.Errorf("destination must not exist")
	}
	files := map[string][]byte{}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink rejected: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if strings.HasPrefix(name, "merkle/") || strings.HasPrefix(name, "chain/") {
			return fmt.Errorf("reserved v2 source path: %s", name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[name] = content
		return nil
	})
	if err != nil {
		return VerifyReport{}, err
	}
	if err := validateV2(files); err != nil {
		return VerifyReport{}, err
	}
	tree, err := merkle.New(files)
	if err != nil {
		return VerifyReport{}, err
	}
	if err := writeBundle(destination, files, tree); err != nil {
		return VerifyReport{}, err
	}
	return VerifyV2(destination)
}

func validateV2(files map[string][]byte) error {
	for _, name := range []string{"v2_metadata.json", "agent_events.jsonl", "search_trace.jsonl", "fold_metrics.json", "selected_parameters.json", "ensemble_trades.csv", "ensemble_ledger.csv", "market_data/quality.json", "market_data/cross_reference.json"} {
		if len(files[name]) == 0 {
			return fmt.Errorf("required v2 evidence missing: %s", name)
		}
	}
	var metadata struct {
		SchemaVersion  string `json:"schema_version"`
		ReplayVerified bool   `json:"replay_verified"`
	}
	if err := json.Unmarshal(files["v2_metadata.json"], &metadata); err != nil {
		return err
	}
	if metadata.SchemaVersion != "2.0.0" || !metadata.ReplayVerified {
		return fmt.Errorf("v2 requires schema 2.0.0 and verified replay")
	}
	return nil
}

func VerifyV2(directory string) (VerifyReport, error) {
	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("v2 symlink rejected: %s", path)
		}
		return nil
	}); err != nil {
		return VerifyReport{}, err
	}

	files, err := ReadLeafFiles(directory)
	if err != nil {
		return VerifyReport{}, err
	}
	if err := validateV2(files); err != nil {
		return VerifyReport{}, err
	}
	tree, err := merkle.New(files)
	if err != nil {
		return VerifyReport{}, err
	}
	raw, err := os.ReadFile(filepath.Join(directory, "merkle", "merkle_manifest.json"))
	if err != nil {
		return VerifyReport{}, err
	}
	var manifest MerkleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return VerifyReport{}, err
	}
	if manifest.Root != tree.RootHex() || manifest.LeafCount != len(tree.Leaves) || len(manifest.Leaves) != len(tree.Leaves) {
		return VerifyReport{}, fmt.Errorf("v2 root or file set changed")
	}
	for index, leaf := range tree.Leaves {
		record := manifest.Leaves[index]
		if record.Index != index || record.Path != leaf.Path {
			return VerifyReport{}, fmt.Errorf("v2 leaf ordering changed")
		}
		proofName := filepath.ToSlash(filepath.Join("merkle", "proofs", fmt.Sprintf("%04d.json", index)))
		if record.ProofPath != proofName {
			return VerifyReport{}, fmt.Errorf("noncanonical proof path")
		}
		if err := VerifyProofFile(directory, filepath.Join(directory, filepath.FromSlash(proofName))); err != nil {
			return VerifyReport{}, err
		}
	}
	return VerifyReport{SchemaVersion: "2.0.0", Status: "passed", LeafCount: len(tree.Leaves), ProofsValid: len(tree.Leaves), MerkleRoot: tree.RootHex(), RunID: tree.RootHex()[:16]}, nil
}
