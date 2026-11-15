#!/usr/bin/env python3
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GROUPS = {
    "D1_backtest_engine": ["SPEC.md", "configs/costs.json", "configs/selected_parameters.json", "data/data_manifest.json", "reports/m2_execution_validation.json", "reports/m3_strategy_validation.json", "reports/m4_parameter_selection.json", "reports/m5_final_holdout.json", "reports/m5_regime_metrics.json", "reports/m5_sensitivity.json"],
    "D2_evm_registry": ["contracts/src/BacktestRegistry.sol", "contracts/test/BacktestRegistry.t.sol", "reports/m7_chain_validation.json", "reports/m7_contract_coverage.json", "artifacts/098376de7f855b9e/chain/deployment.json", "artifacts/098376de7f855b9e/chain/submission_receipt.json", "artifacts/098376de7f855b9e/chain/gas_report.json"],
    "D3_verification_package": ["artifacts/098376de7f855b9e/run_manifest.json", "artifacts/098376de7f855b9e/merkle/merkle_manifest.json", "artifacts/098376de7f855b9e/merkle/proofs/0565.json", "internal/replay/replay.go", "reports/m8_replay.json", "reports/m8_mutation.json", "reports/m8_coverage.json", "reports/m8_overhead.json", "docs/TECHNICAL_REPORT.md", "docs/DEMO.md"],
}


def main():
    groups = {}
    for group, names in GROUPS.items():
        entries = []
        for name in names:
            path = ROOT / name
            if not path.is_file():
                raise SystemExit(f"missing deliverable: {name}")
            content = path.read_bytes()
            entries.append({"bytes": len(content), "path": name, "sha256": hashlib.sha256(content).hexdigest()})
        groups[group] = entries
    manifest = {"groups": groups, "schema_version": "1.0.0", "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(), "status": "passed"}
    output = ROOT / "deliverables" / "manifest.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
