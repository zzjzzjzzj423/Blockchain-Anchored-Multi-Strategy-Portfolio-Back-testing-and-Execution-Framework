package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"msbd5017/portfolio-proof/internal/canonical"
	"msbd5017/portfolio-proof/internal/evidence"
	"msbd5017/portfolio-proof/internal/merkle"
)

const (
	anvilPrivateKey    = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	anvilAddress       = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	rpcURL             = "http://127.0.0.1:8545"
	verifySignature    = "verifyStoredProof(bytes32,bytes32,bytes32[],bool[])(bool)"
	rootSubmittedTopic = "0x7e2311e14a0d54158523500a45fe10fcb41470543f892eea00da3531e8c22fc7"
)

type GasMeasurement struct {
	CalldataBytes int    `json:"calldata_bytes"`
	Gas           uint64 `json:"gas"`
	ProofDepth    int    `json:"proof_depth"`
}

type M7Report struct {
	ArtifactSHA256        map[string]string `json:"artifact_sha256"`
	ChainID               int               `json:"chain_id"`
	ContractAddress       string            `json:"contract_address"`
	DeploymentTransaction string            `json:"deployment_transaction"`
	DuplicateRootRejected bool              `json:"duplicate_root_rejected"`
	EventMatchesStorage   bool              `json:"event_matches_storage"`
	FoundryVersion        string            `json:"foundry_version"`
	LeafPath              string            `json:"leaf_path"`
	MerkleRoot            string            `json:"merkle_root"`
	MetadataHash          string            `json:"metadata_hash"`
	MutationsRejected     bool              `json:"mutations_rejected"`
	OfflineProofValid     bool              `json:"offline_proof_valid"`
	OnchainProofValid     bool              `json:"onchain_proof_valid"`
	ProofDepth            int               `json:"proof_depth"`
	RunID                 string            `json:"run_id"`
	SchemaVersion         string            `json:"schema_version"`
	SolidityVersion       string            `json:"solidity_version"`
	SourceCommit          string            `json:"source_commit"`
	Status                string            `json:"status"`
	SubmissionTransaction string            `json:"submission_transaction"`
	ZeroRootRejected      bool              `json:"zero_root_rejected"`
}

type commandRunner struct {
	root       string
	foundryBin string
	env        []string
}

func RunM7(ctx context.Context, root string) (M7Report, error) {
	return runM7(ctx, root, true)
}

// RehearseM7 repeats the deployment and proof gates on an ephemeral Anvil
// without overwriting the frozen M7 evidence files.
func RehearseM7(ctx context.Context, root string) (M7Report, error) {
	return runM7(ctx, root, false)
}

func runM7(ctx context.Context, root string, persist bool) (M7Report, error) {
	reportPath := filepath.Join(root, "reports", "m7_chain_validation.json")
	if persist {
		clean, err := gitClean(root)
		if err != nil || !clean {
			return M7Report{}, fmt.Errorf("M7 deployment requires a clean Git tree")
		}
		if _, err := os.Stat(reportPath); err == nil {
			return M7Report{}, fmt.Errorf("M7 report already exists")
		} else if !os.IsNotExist(err) {
			return M7Report{}, err
		}
	}
	foundryBin := filepath.Join(root, ".tools", "foundry-v1.9.0", "bin")
	runner := commandRunner{
		root: root, foundryBin: foundryBin,
		env: append(os.Environ(), "FOUNDRY_DIR="+filepath.Join(root, ".cache", "foundry"), "SVM_HOME="+filepath.Join(root, ".cache", "svm")),
	}
	forgeVersion, err := runner.output(ctx, "forge", "--version")
	if err != nil || !strings.Contains(forgeVersion, "1.9.0") {
		return M7Report{}, fmt.Errorf("Foundry v1.9.0 is required")
	}
	sourceCommit, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return M7Report{}, err
	}
	m6, err := loadM6Report(root)
	if err != nil {
		return M7Report{}, err
	}
	bundlePath := filepath.Join(root, filepath.FromSlash(m6.BundleRelativePath))
	if _, err := evidence.VerifyBundle(bundlePath); err != nil {
		return M7Report{}, fmt.Errorf("offline bundle verification: %w", err)
	}
	proofPath, err := evidence.FindProof(bundlePath, "results/trades.csv")
	if err != nil {
		return M7Report{}, err
	}
	proof, err := evidence.LoadProof(proofPath)
	if err != nil {
		return M7Report{}, err
	}
	rootHash, err := merkle.ParseHash(m6.MerkleRoot)
	if err != nil {
		return M7Report{}, err
	}
	leafContent, err := os.ReadFile(filepath.Join(bundlePath, filepath.FromSlash(proof.Path)))
	if err != nil {
		return M7Report{}, err
	}
	if err := merkle.VerifyProof(leafContent, proof, rootHash); err != nil {
		return M7Report{}, err
	}

	anvil, err := runner.startAnvil(ctx)
	if err != nil {
		return M7Report{}, err
	}
	defer stopAnvil(anvil)
	if err := runner.waitForRPC(ctx); err != nil {
		return M7Report{}, err
	}

	deploymentOutput, err := runner.output(ctx, "forge", "create", "--root", "contracts", "src/BacktestRegistry.sol:BacktestRegistry", "--rpc-url", rpcURL, "--private-key", anvilPrivateKey, "--broadcast", "--json")
	if err != nil {
		return M7Report{}, err
	}
	var deployment struct {
		Deployer        string `json:"deployer"`
		DeployedTo      string `json:"deployedTo"`
		TransactionHash string `json:"transactionHash"`
	}
	if err := json.Unmarshal([]byte(deploymentOutput), &deployment); err != nil || deployment.DeployedTo == "" {
		return M7Report{}, fmt.Errorf("decode deployment: %w", err)
	}
	deploymentReceipt, err := runner.receipt(ctx, deployment.TransactionHash)
	if err != nil {
		return M7Report{}, err
	}

	metadataBytes, metadataHash, err := anchorMetadata(m6)
	if err != nil {
		return M7Report{}, err
	}
	rootArgument := "0x" + m6.MerkleRoot
	metadataArgument := "0x" + metadataHash
	submitEstimate, err := runner.estimate(ctx, deployment.DeployedTo, "submitRoot(bytes32,bytes32)", rootArgument, metadataArgument)
	if err != nil {
		return M7Report{}, err
	}
	submissionOutput, err := runner.output(ctx, "cast", "send", deployment.DeployedTo, "submitRoot(bytes32,bytes32)", rootArgument, metadataArgument, "--rpc-url", rpcURL, "--private-key", anvilPrivateKey, "--json")
	if err != nil {
		return M7Report{}, err
	}
	var submissionReceipt map[string]any
	decoder := json.NewDecoder(strings.NewReader(submissionOutput))
	decoder.UseNumber()
	if err := decoder.Decode(&submissionReceipt); err != nil {
		return M7Report{}, err
	}
	submissionTx, _ := submissionReceipt["transactionHash"].(string)
	status, _ := submissionReceipt["status"].(string)
	if submissionTx == "" || status != "0x1" {
		return M7Report{}, fmt.Errorf("root submission failed")
	}

	siblings, directions := proofArguments(proof)
	valid, err := runner.verifyCall(ctx, deployment.DeployedTo, rootArgument, "0x"+proof.LeafHash, siblings, directions)
	if err != nil || !valid {
		return M7Report{}, fmt.Errorf("real artifact proof failed on chain: %w", err)
	}
	mutationsRejected, err := runner.verifyMutations(ctx, deployment.DeployedTo, rootArgument, proof)
	if err != nil || !mutationsRejected {
		return M7Report{}, fmt.Errorf("mutated proof was not rejected")
	}
	lengthMismatchRejected := runner.lengthMismatchRejected(ctx, deployment.DeployedTo, rootArgument, proof)
	duplicateRejected := runner.submitRejected(ctx, deployment.DeployedTo, rootArgument, metadataArgument)
	zeroRejected := runner.submitRejected(ctx, deployment.DeployedTo, "0x"+strings.Repeat("0", 64), metadataArgument)
	storage, err := runner.storageRecord(ctx, deployment.DeployedTo, rootArgument)
	if err != nil {
		return M7Report{}, err
	}
	eventMatches := validateSubmissionEvent(submissionReceipt, deployment.DeployedTo, rootArgument, metadataArgument, storage)
	if !lengthMismatchRejected || !duplicateRejected || !zeroRejected || !eventMatches {
		return M7Report{}, fmt.Errorf("on-chain acceptance gate failed")
	}

	gasMeasurements, err := runner.measureProofGas(ctx, deployment.DeployedTo, rootArgument, proof)
	if err != nil {
		return M7Report{}, err
	}
	actualSubmissionGas, err := receiptGas(submissionReceipt)
	if err != nil {
		return M7Report{}, err
	}
	contractCode, err := runner.output(ctx, "cast", "code", deployment.DeployedTo, "--rpc-url", rpcURL)
	if err != nil {
		return M7Report{}, err
	}
	contractSource, _ := os.ReadFile(filepath.Join(root, "contracts", "src", "BacktestRegistry.sol"))

	artifacts := make(map[string][]byte)
	artifacts["anchor_metadata.json"] = metadataBytes
	artifacts["deployment.json"], err = canonical.JSON(map[string]any{
		"chain_id": 31337, "contract_address": strings.ToLower(deployment.DeployedTo),
		"contract_code_sha256": digest([]byte(strings.TrimSpace(contractCode))), "contract_source_sha256": digest(contractSource),
		"deployer": strings.ToLower(deployment.Deployer), "deployment_receipt": deploymentReceipt,
		"deployment_transaction": deployment.TransactionHash, "foundry_version": firstLine(forgeVersion),
		"schema_version": "1.0.0", "solidity_version": "0.8.28", "source_commit": sourceCommit,
	})
	if err != nil {
		return M7Report{}, err
	}
	artifacts["submission_receipt.json"], err = canonical.JSON(submissionReceipt)
	if err != nil {
		return M7Report{}, err
	}
	artifacts["gas_report.json"], err = canonical.JSON(map[string]any{
		"deployment_gas": receiptGasValue(deploymentReceipt), "proof_verification": gasMeasurements,
		"root_submission_actual_gas": actualSubmissionGas, "root_submission_estimated_gas": submitEstimate,
		"schema_version": "1.0.0",
	})
	if err != nil {
		return M7Report{}, err
	}
	artifacts["proof_verification.json"], err = canonical.JSON(map[string]any{
		"direction_mutation_rejected": true, "leaf_mutation_rejected": true, "length_mismatch_rejected": true,
		"offline_valid": true, "onchain_valid": true, "path": proof.Path, "proof_depth": len(proof.Siblings),
		"proof_file":     filepath.ToSlash(strings.TrimPrefix(proofPath, bundlePath+string(filepath.Separator))),
		"schema_version": "1.0.0", "sibling_mutation_rejected": true,
	})
	if err != nil {
		return M7Report{}, err
	}
	hashes := make(map[string]string, len(artifacts))
	for name, content := range artifacts {
		if content == nil {
			return M7Report{}, fmt.Errorf("encode chain artifact %s", name)
		}
		if persist {
			chainDirectory := filepath.Join(bundlePath, "chain")
			if err := os.MkdirAll(chainDirectory, 0o755); err != nil {
				return M7Report{}, err
			}
			if err := os.WriteFile(filepath.Join(chainDirectory, name), content, 0o644); err != nil {
				return M7Report{}, err
			}
		}
		hashes[name] = digest(content)
	}
	report := M7Report{
		ArtifactSHA256: hashes, ChainID: 31337, ContractAddress: strings.ToLower(deployment.DeployedTo),
		DeploymentTransaction: deployment.TransactionHash, DuplicateRootRejected: duplicateRejected,
		EventMatchesStorage: eventMatches, FoundryVersion: firstLine(forgeVersion), LeafPath: proof.Path,
		MerkleRoot: m6.MerkleRoot, MetadataHash: metadataHash, MutationsRejected: mutationsRejected,
		OfflineProofValid: true, OnchainProofValid: valid, ProofDepth: len(proof.Siblings), RunID: m6.RunID,
		SchemaVersion: "1.0.0", SolidityVersion: "0.8.28", SourceCommit: sourceCommit, Status: "passed",
		SubmissionTransaction: submissionTx, ZeroRootRejected: zeroRejected,
	}
	reportBytes, err := canonical.JSON(report)
	if err != nil {
		return M7Report{}, err
	}
	if persist {
		if err := os.WriteFile(reportPath, reportBytes, 0o644); err != nil {
			return M7Report{}, err
		}
	}
	return report, nil
}

type m6Report struct {
	BundleRelativePath string `json:"bundle_relative_path"`
	LeafCount          int    `json:"leaf_count"`
	MerkleRoot         string `json:"merkle_root"`
	RunID              string `json:"run_id"`
}

func loadM6Report(root string) (m6Report, error) {
	content, err := os.ReadFile(filepath.Join(root, "reports", "m6_evidence_bundle.json"))
	if err != nil {
		return m6Report{}, err
	}
	var report m6Report
	if err := json.Unmarshal(content, &report); err != nil || report.RunID == "" {
		return m6Report{}, fmt.Errorf("invalid M6 report")
	}
	return report, nil
}

func anchorMetadata(report m6Report) ([]byte, string, error) {
	content, err := canonical.JSON(map[string]any{
		"bundle_uri": report.BundleRelativePath, "hash_algorithm": "SHA-256", "leaf_count": report.LeafCount,
		"merkle_root": report.MerkleRoot, "merkle_version": "ordered-domain-separated-v1",
		"run_id": report.RunID, "schema_version": "1.0.0", "spec_version": "1.0.0-frozen",
	})
	if err != nil {
		return nil, "", err
	}
	return content, digest(content), nil
}

func (runner commandRunner) startAnvil(ctx context.Context) (*exec.Cmd, error) {
	command := exec.CommandContext(ctx, filepath.Join(runner.foundryBin, "anvil"), "--port", "8545", "--chain-id", "31337", "--timestamp", "1735689600", "--silent")
	command.Dir = runner.root
	command.Env = runner.env
	command.Stdout = &bytes.Buffer{}
	command.Stderr = &bytes.Buffer{}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

func stopAnvil(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGINT)
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}

func (runner commandRunner) waitForRPC(ctx context.Context) error {
	for attempt := 0; attempt < 50; attempt++ {
		if _, err := runner.output(ctx, "cast", "block-number", "--rpc-url", rpcURL); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Anvil RPC did not become ready")
}

func (runner commandRunner) output(ctx context.Context, tool string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, filepath.Join(runner.foundryBin, tool), arguments...)
	command.Dir = runner.root
	command.Env = runner.env
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %s", tool, strings.Join(argumentsWithoutSecret(arguments), " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func argumentsWithoutSecret(arguments []string) []string {
	result := append([]string(nil), arguments...)
	for index := range result {
		if result[index] == anvilPrivateKey {
			result[index] = "<anvil-dev-key>"
		}
	}
	return result
}

func (runner commandRunner) receipt(ctx context.Context, transaction string) (map[string]any, error) {
	output, err := runner.output(ctx, "cast", "receipt", transaction, "--rpc-url", rpcURL, "--json")
	if err != nil {
		return nil, err
	}
	var receipt map[string]any
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (runner commandRunner) estimate(ctx context.Context, contract, signature string, arguments ...string) (uint64, error) {
	commandArguments := append([]string{"estimate", contract, signature}, arguments...)
	commandArguments = append(commandArguments, "--rpc-url", rpcURL, "--from", anvilAddress)
	output, err := runner.output(ctx, "cast", commandArguments...)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.Fields(output)[0], 10, 64)
}

func proofArguments(proof merkle.Proof) (string, string) {
	siblings := make([]string, len(proof.Siblings))
	directions := make([]string, len(proof.Siblings))
	for index, step := range proof.Siblings {
		siblings[index] = "0x" + step.Hash
		directions[index] = strconv.FormatBool(step.SiblingOnLeft)
	}
	return "[" + strings.Join(siblings, ",") + "]", "[" + strings.Join(directions, ",") + "]"
}

func (runner commandRunner) verifyCall(ctx context.Context, contract, root, leaf, siblings, directions string) (bool, error) {
	output, err := runner.output(ctx, "cast", "call", contract, verifySignature, root, leaf, siblings, directions, "--rpc-url", rpcURL)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "true", nil
}

func flipHash(value string) string {
	decoded, _ := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	decoded[0] ^= 1
	return "0x" + hex.EncodeToString(decoded)
}

func (runner commandRunner) verifyMutations(ctx context.Context, contract, root string, proof merkle.Proof) (bool, error) {
	siblings, directions := proofArguments(proof)
	leafOK, err := runner.verifyCall(ctx, contract, root, flipHash("0x"+proof.LeafHash), siblings, directions)
	if err != nil {
		return false, err
	}
	mutatedProof := proof
	mutatedProof.Siblings = append([]merkle.ProofStep(nil), proof.Siblings...)
	mutatedProof.Siblings[0].Hash = strings.TrimPrefix(flipHash("0x"+proof.Siblings[0].Hash), "0x")
	mutatedSiblings, _ := proofArguments(mutatedProof)
	siblingOK, err := runner.verifyCall(ctx, contract, root, "0x"+proof.LeafHash, mutatedSiblings, directions)
	if err != nil {
		return false, err
	}
	mutatedProof = proof
	mutatedProof.Siblings = append([]merkle.ProofStep(nil), proof.Siblings...)
	mutatedProof.Siblings[0].SiblingOnLeft = !mutatedProof.Siblings[0].SiblingOnLeft
	_, mutatedDirections := proofArguments(mutatedProof)
	directionOK, err := runner.verifyCall(ctx, contract, root, "0x"+proof.LeafHash, siblings, mutatedDirections)
	return !leafOK && !siblingOK && !directionOK, err
}

func (runner commandRunner) lengthMismatchRejected(ctx context.Context, contract, root string, proof merkle.Proof) bool {
	siblings, _ := proofArguments(proof)
	directions := "[]"
	_, err := runner.verifyCall(ctx, contract, root, "0x"+proof.LeafHash, siblings, directions)
	return err != nil
}

func (runner commandRunner) submitRejected(ctx context.Context, contract, root, metadata string) bool {
	_, err := runner.estimate(ctx, contract, "submitRoot(bytes32,bytes32)", root, metadata)
	return err != nil
}

type storageRecord struct {
	Submitter    string
	Timestamp    uint64
	MetadataHash string
}

func (runner commandRunner) storageRecord(ctx context.Context, contract, root string) (storageRecord, error) {
	output, err := runner.output(ctx, "cast", "call", contract, "records(bytes32)(address,uint64,bytes32)", root, "--rpc-url", rpcURL)
	if err != nil {
		return storageRecord{}, err
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		return storageRecord{}, fmt.Errorf("unexpected record output")
	}
	timestamp, err := strconv.ParseUint(strings.Fields(lines[1])[0], 10, 64)
	if err != nil {
		return storageRecord{}, err
	}
	return storageRecord{Submitter: strings.ToLower(strings.TrimSpace(lines[0])), Timestamp: timestamp, MetadataHash: strings.ToLower(strings.TrimSpace(lines[2]))}, nil
}

func validateSubmissionEvent(receipt map[string]any, contract, root, metadata string, storage storageRecord) bool {
	logs, ok := receipt["logs"].([]any)
	if !ok || len(logs) != 1 || storage.Submitter != anvilAddress || storage.MetadataHash != strings.ToLower(metadata) {
		return false
	}
	log, ok := logs[0].(map[string]any)
	if !ok || strings.ToLower(fmt.Sprint(log["address"])) != strings.ToLower(contract) {
		return false
	}
	topics, ok := log["topics"].([]any)
	expectedSubmitter := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(anvilAddress, "0x")
	if !ok || len(topics) != 3 || strings.ToLower(fmt.Sprint(topics[0])) != rootSubmittedTopic ||
		strings.ToLower(fmt.Sprint(topics[1])) != strings.ToLower(root) ||
		strings.ToLower(fmt.Sprint(topics[2])) != expectedSubmitter {
		return false
	}
	data := strings.TrimPrefix(strings.ToLower(fmt.Sprint(log["data"])), "0x")
	if len(data) != 128 || "0x"+data[64:] != strings.ToLower(metadata) {
		return false
	}
	eventTimestamp, err := strconv.ParseUint(data[:64], 16, 64)
	return err == nil && eventTimestamp == storage.Timestamp
}

func (runner commandRunner) measureProofGas(ctx context.Context, contract, root string, proof merkle.Proof) ([]GasMeasurement, error) {
	depths := []int{1, 5, len(proof.Siblings)}
	measurements := make([]GasMeasurement, 0, len(depths))
	for _, depth := range depths {
		partial := proof
		partial.Siblings = proof.Siblings[:depth]
		siblings, directions := proofArguments(partial)
		arguments := []string{root, "0x" + proof.LeafHash, siblings, directions}
		gas, err := runner.estimate(ctx, contract, "verifyStoredProof(bytes32,bytes32,bytes32[],bool[])", arguments...)
		if err != nil {
			return nil, err
		}
		calldataArguments := append([]string{"calldata", "verifyStoredProof(bytes32,bytes32,bytes32[],bool[])"}, arguments...)
		calldata, err := runner.output(ctx, "cast", calldataArguments...)
		if err != nil {
			return nil, err
		}
		measurements = append(measurements, GasMeasurement{CalldataBytes: (len(strings.TrimSpace(calldata)) - 2) / 2, Gas: gas, ProofDepth: depth})
	}
	return measurements, nil
}

func receiptGas(receipt map[string]any) (uint64, error) {
	value, ok := receipt["gasUsed"].(string)
	if !ok {
		return 0, fmt.Errorf("receipt has no gasUsed")
	}
	return strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 64)
}

func receiptGasValue(receipt map[string]any) uint64 {
	value, _ := receiptGas(receipt)
	return value
}

func firstLine(value string) string { return strings.Split(strings.TrimSpace(value), "\n")[0] }

func digest(content []byte) string {
	value := sha256.Sum256(content)
	return hex.EncodeToString(value[:])
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
