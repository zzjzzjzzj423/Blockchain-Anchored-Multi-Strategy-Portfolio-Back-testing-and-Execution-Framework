# V2 evidence and review protocol

V2 adds agent/tool execution evidence, input quality gates, source corroboration, and signed review to the development-only experiment. It preserves the frozen v1 Merkle encoding and does not replace or re-score the published 2025 holdout.

## Components and trust boundaries

| Requirement | Implementation | Boundary |
| --- | --- | --- |
| Agent framework | LangGraph `StateGraph`, model/tool loop, schema-validated registered tools, SQLite checkpoints, tool receipts | Resume occurs at node boundaries. An interrupted long-running search may need to be rerun. |
| V2 evidence | Go `evidence pack-v2` / `verify-v2`, per-file inclusion proofs | Exact-byte integrity, not correctness or market truth. |
| Execution traces | Agent decisions, tool requests/results, parameter search, folds, signals, orders, trades, ledger, replay observations | Private model reasoning is neither requested nor recorded. |
| Multi-party review | Ed25519 statements from publisher and reviewer, distinct authorized keys | Key ownership and organizational independence require out-of-band verification. |
| Data defects | Finite/positive OHLCV, range, duplicate, ordering, coverage, pinned snapshot/config hashes | Frozen hashes detect drift; they do not prove the original feed was truthful. |
| Market corroboration | Binance Vision versus Coinbase Exchange daily closes, committed raw responses and normalized sample | 21 closes across three assets, 2024-12-01 to 2024-12-07 only. |

## Agent and failure handling

`make agent` runs the LangGraph runtime. The model chooses registered tools and arguments using persisted messages and observations. Data audit must pass before `run_research`; replay and report must succeed before the model may finish. There is no signing tool available to the model.

Malformed tool arguments, unknown tools, out-of-policy research budgets, missing or disputed feeds return errors to the agent. Model quota exhaustion stops immediately and preserves the checkpoint for a later explicit resume. It is not retried repeatedly or converted to a manual search. Parameter proposals are selected from validated, unevaluated candidates. The host enforces finite candidate and graph-step budgets.

```bash
make agent-bootstrap
make agent-test
make agent
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.cli \
  --resume artifacts/langgraph_SESSION --codex-bin /path/to/codex
```

For a bounded end-to-end run, use `--budget 6`; the default is 20. `--stop-after-tools 1` checkpoints after the first completed tool for a controlled cross-process resume test.

## V2 leaf policy and partial-file verification

`v2_metadata.json` identifies schema `2.0.0`, replay status and review status. Required leaves include agent events, proposal trace, fold metrics, selected parameters, trades, ledger and market-data quality/cross-reference reports. The final package also contains the report, source identity hashes, full execution CSVs, raw secondary-source responses, source hashes and isolated fault-campaign results.

The Merkle codec remains v1: SHA-256 leaves bind `0x00 || uint32_be(path byte length) || UTF-8 NFC path || uint64_be(content length) || exact bytes`; internal nodes bind `0x01 || left || right`; UTF-8 path ordering and duplicate-last odd levels are preserved. `merkle/` and `chain/` are derived/external metadata and excluded from the leaf set. New bundles cannot overwrite an existing destination.

```bash
./bin/project evidence pack-v2 --source RUN/outputs --bundle NEW_BUNDLE
./bin/project evidence verify-v2 --bundle NEW_BUNDLE
./bin/project evidence prove --bundle NEW_BUNDLE --path ensemble_trades.csv
./bin/project evidence verify-proof --bundle NEW_BUNDLE --proof NEW_BUNDLE/merkle/proofs/NNNN.json
```

A partial-file reviewer can retain the selected file, its proof and Merkle manifest while other leaves are unavailable. The independently signed root is the trust anchor. Full verification additionally checks the entire file set and all proofs. Symlinks are rejected. Manifest/proof schema remains the established codec version; the committed `v2_metadata.json` declares the v2 application-level evidence policy.

## Market-data corroboration

Source URLs are fixed to the official Coinbase Exchange candle endpoint. Raw response bytes are committed with SHA-256 hashes. Source candles must be complete, unique, aligned UTC daily buckets with finite positive OHLC values. Only the common historical window is compared.

Binance pairs are USDT-quoted; Coinbase pairs are USD-quoted. A predeclared 200-basis-point deviation threshold allows for venue/quote differences. Values are compared, not expected to have identical hashes. Hashes establish which snapshots were compared. Missing coverage or an unavailable source produces `unavailable`; excessive disagreement produces `disputed`. Both block research rather than being imputed or silently accepted.

The reviewer recomputes the comparison from bundled raw snapshots with **zero network calls**, checks the local primary data identity against the committed hash, and replays the strategy. `verify_market_evidence()` also rejects a modified comparison report or secondary snapshot. This mechanism provides bounded cross-source corroboration. Two sources can share upstream errors or collude; historical endpoint data can be revised; a signature attests a participant's statement. None is a proof of market truth. The current v2 bundle is not automatically anchored on-chain. The existing v1 local EVM registry continues to verify commitments, not economic truth.

## Multi-party lifecycle

Lifecycle: research complete → immutable v2 bundle → publisher statement → reviewer independently verifies bytes, source snapshots and replay → approved / rejected / awaiting review.

Each statement includes the domain, exact root, trusted policy hash, role, decision, comment and public key. It is signed with Ed25519. The verifier requires one publisher approval and one reviewer approval from distinct authorized keys; any authorized rejection blocks approval. Duplicate keys, unauthorized roles, tampered statements, changed policy and different roots are rejected. A new evidence root requires fresh statements. Signatures and review status remain outside the immutable bundle to avoid a circular commitment.

Key setup (each participant keeps their own private key):

```bash
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.review keygen --key PRIVATE_KEY.pem
```

The command prints the public key. Create a trusted policy JSON through an authenticated exchange of participants' public keys:

```json
{"roles":{"publisher":["PUBLISHER_PUBLIC_HEX"],"reviewer":["REVIEWER_PUBLIC_HEX"]},"identity_scope":"Participant identities verified separately"}
```

Signing and verification:

```bash
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.review attest \
  --bundle BUNDLE --policy policy.json --key PRIVATE_KEY.pem \
  --role reviewer --decision approve --output reviewer.json
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.review verify \
  --bundle BUNDLE --policy policy.json --attestations publisher.json reviewer.json
```

Reviewer approval executes the Go full verifier, offline market-evidence recomputation and deterministic strategy replay before signing. A `reject` decision records unresolved objections. The model has no access to the approval keys. Local demo keys exercise the protocol only; demo approval must not be described as real independent team review.

## Verification gates

`make ci` exercises Go accounting, replay, Merkle and v2 packing. `make agent-test` checks LangGraph restart without repeating a completed tool, error observations and recovery, completion policy, market defects, source disagreement/unavailability, config/snapshot drift, and review-signature policy. The real-data fault campaign injects twelve isolated OHLCV/config defects into copies and records which were detected. Production inputs and frozen v1 reports are never modified by these tests.
