package chain

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"msbd5017/portfolio-proof/internal/merkle"
)

func TestAnchorMetadataIsCanonicalAndDeterministic(t *testing.T) {
	report := m6Report{
		BundleRelativePath: "artifacts/run",
		LeafCount:          568,
		MerkleRoot:         strings.Repeat("ab", 32),
		RunID:              "run",
	}
	first, firstHash, err := anchorMetadata(report)
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := anchorMetadata(report)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || firstHash != secondHash || digest(first) != firstHash {
		t.Fatal("anchor metadata is not deterministic")
	}
	if strings.Count(string(first), "\n") != 1 || first[len(first)-1] != '\n' || !json.Valid(first) {
		t.Fatalf("metadata is not canonical JSON: %q", first)
	}
}

func TestProofArgumentsPreserveOrderAndDirection(t *testing.T) {
	proof := merkle.Proof{Siblings: []merkle.ProofStep{
		{Hash: strings.Repeat("11", 32), SiblingOnLeft: true},
		{Hash: strings.Repeat("22", 32), SiblingOnLeft: false},
	}}
	siblings, directions := proofArguments(proof)
	if siblings != "[0x"+strings.Repeat("11", 32)+",0x"+strings.Repeat("22", 32)+"]" {
		t.Fatalf("unexpected siblings: %s", siblings)
	}
	if directions != "[true,false]" {
		t.Fatalf("unexpected directions: %s", directions)
	}
}

func TestValidateSubmissionEvent(t *testing.T) {
	contract := "0x1111111111111111111111111111111111111111"
	root := "0x" + strings.Repeat("22", 32)
	metadata := "0x" + strings.Repeat("33", 32)
	timestamp := uint64(1_735_689_602)
	data := "0x" + leftPadHex(timestamp, 64) + strings.TrimPrefix(metadata, "0x")
	receipt := map[string]any{"logs": []any{map[string]any{
		"address": contract,
		"topics":  []any{rootSubmittedTopic, root, "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(anvilAddress, "0x")},
		"data":    data,
	}}}
	storage := storageRecord{Submitter: anvilAddress, Timestamp: timestamp, MetadataHash: metadata}
	if !validateSubmissionEvent(receipt, contract, root, metadata, storage) {
		t.Fatal("valid event did not match storage")
	}
	receipt["logs"].([]any)[0].(map[string]any)["address"] = "0x2222222222222222222222222222222222222222"
	if validateSubmissionEvent(receipt, contract, root, metadata, storage) {
		t.Fatal("wrong emitting contract was accepted")
	}
}

func TestReceiptGasAndSecretRedaction(t *testing.T) {
	gas, err := receiptGas(map[string]any{"gasUsed": "0x5208"})
	if err != nil || gas != 21_000 {
		t.Fatalf("unexpected gas result: %d, %v", gas, err)
	}
	if _, err := receiptGas(map[string]any{}); err == nil {
		t.Fatal("missing gasUsed was accepted")
	}
	arguments := argumentsWithoutSecret([]string{"--private-key", anvilPrivateKey})
	if arguments[1] != "<anvil-dev-key>" {
		t.Fatal("development key was not redacted")
	}
}

func TestFlipHash(t *testing.T) {
	value := "0x" + strings.Repeat("00", 32)
	flipped := flipHash(value)
	if flipped != "0x01"+strings.Repeat("00", 31) || flipHash(flipped) != value {
		t.Fatalf("unexpected bit flip: %s", flipped)
	}
}

func leftPadHex(value uint64, width int) string {
	encoded := strconv.FormatUint(value, 16)
	return strings.Repeat("0", width-len(encoded)) + encoded
}
