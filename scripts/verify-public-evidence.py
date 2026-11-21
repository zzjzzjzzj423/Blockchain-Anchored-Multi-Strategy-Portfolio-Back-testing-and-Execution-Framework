#!/usr/bin/env python3
"""Check the evidence that is actually present in a fresh repository clone.

This is a consistency check, not a replacement for verifying the excluded
Merkle bundle or replaying the original market data.
"""

import hashlib
import json
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


def read_json(name):
    return json.loads((ROOT / name).read_text(encoding="utf-8"))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def main():
    selected = read_json("configs/selected_parameters.json")
    m4 = read_json("reports/m4_parameter_selection.json")
    m5 = read_json("reports/m5_final_holdout.json")
    m6 = read_json("reports/m6_evidence_bundle.json")
    m7 = read_json("reports/m7_chain_validation.json")
    m8 = read_json("reports/m8_replay.json")
    mutation = read_json("reports/m8_mutation.json")
    manifest = read_json("deliverables/manifest.json")

    require(selected["candidate_id"] == m4["selected_candidate_id"] == m5["candidate_id"],
            "selected candidate differs across config, M4 and M5")
    require(m6["run_id"] == m7["run_id"] == m8["run_id"],
            "run ID differs across M6, M7 and M8")
    require(m6["merkle_root"] == m7["merkle_root"], "M6 and M7 roots differ")
    require(all(root == m6["merkle_root"] for root in m6["rebuild_roots"]),
            "M6 rebuild roots differ")
    require(len(m6["rebuild_roots"]) == m6["deterministic_builds"],
            "M6 rebuild count differs")
    require(m6["leaf_count"] == m8["leaf_count"], "M6 and M8 leaf counts differ")
    require(m8["replay_count"] == m8["artifact_identical_runs"] == m8["root_identical_runs"],
            "M8 replay counts differ")
    require(len(m8["iterations"]) == m8["replay_count"], "M8 iteration count differs")
    require(all(item["artifact_byte_identical"] and item["merkle_root"] == m6["merkle_root"]
                for item in m8["iterations"]), "M8 iteration differs from reported root")
    require(mutation["count"] == mutation["detected"] == mutation["localized"]
            == len(mutation["mutations"]), "M8 mutation counts differ")
    require(m7["chain_id"] == 31337, "M7 report is not for the documented local chain")
    require(all(report["status"] == "passed" for report in (m4, m5, m6, m7, m8, mutation)),
            "a milestone report did not pass")

    checked = 0
    missing = []
    for entries in manifest["groups"].values():
        for entry in entries:
            path = ROOT / entry["path"]
            if not path.is_file():
                missing.append(entry["path"])
                continue
            content = path.read_bytes()
            require(len(content) == entry["bytes"] and
                    hashlib.sha256(content).hexdigest() == entry["sha256"],
                    f"deliverable hash mismatch: {entry['path']}")
            checked += 1

    require(all(path.startswith("artifacts/") for path in missing),
            f"committed deliverable missing: {missing}")
    print(f"Public report consistency passed; {checked} present deliverable hashes match.")
    if missing:
        print(f"{len(missing)} generated artifact files are absent from this clone.")
        print("Merkle proofs, raw data and independent replay require the separate evidence bundle.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
        print(f"Public evidence check failed: {exc}", file=sys.stderr)
        sys.exit(1)
