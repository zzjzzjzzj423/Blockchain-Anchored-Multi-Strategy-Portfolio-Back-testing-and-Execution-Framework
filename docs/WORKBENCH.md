# ProofLab research workbench

Start with `make serve`, then open **http://127.0.0.1:8765**. On macOS, `启动研究工作台.command` prepares the pinned environment and opens the browser. If the workbench is already running, the launcher opens that instance.

## What is interactive

| Page | Working actions |
| --- | --- |
| Overview | Switch completed sessions, open results, start research |
| Agent console | Choose a natural-language preset, set candidate budget, start a real LangGraph session, inspect live logs, resume a saved session |
| Strategy/results | Inspect parameters, fold metrics, candidate comparisons, ledger-based equity and actual trades |
| Counterfactual lab | Re-execute baseline, doubled commission, doubled slippage, zero-cost diagnostic and adjacent trend parameter in the Go engine |
| Real data | Inspect complete primary history and actual secondary coverage, source responses/hashes, missing periods, offline inventory verification |
| Execution/evidence | Preview every JSON/JSONL/CSV, download exact bytes, verify one proof, inject a one-byte mutation into an isolated copy, verify full bundle and replay |
| Multi-party review | Verify public-key role policy and signatures, import separately signed public JSON statements without handling private keys |
| Fault campaign | Re-run all twelve isolated data/config defect cases and see detected outcomes |

## Research contribution

The agent must register an evidence-based risk hypothesis **before** requesting counterfactual tests. Interventions are evaluated by the deterministic execution engine under the existing timing and cost model. A fixed rule flags a Sharpe drop greater than 0.3 or a negative median under a non-diagnostic intervention. The model cannot tune this threshold after observing outcomes. The hypothesis, measured interventions, verdict, tool trace, inputs and ledger travel together in a verifiable evidence bundle.

This is an implementation contribution combining falsifiable agent decisions, measured counterfactuals and attributable evidence. It is not a claim that parameter search or stress testing are newly invented financial algorithms. Counterfactuals remain development-data experiments, not future-profit forecasts or causal estimates about an uncontrolled real market.

## Authentic data and limits

Full primary history contains 5,478 actual daily records from the fixed Binance snapshot. Independent reference history contains 5,311 actual Coinbase records. The 167 missing early SOL reference days are shown explicitly; primary SOL observations remain complete. The historical window is disclosed throughout the UI. Old v2 evidence retains its original 21-close signed source-validation scope. Newly generated runs can additionally commit the full-history reference inventory and raw snapshots.

## Repeatable checks

```bash
make ci
make agent-test
npm install --prefix .cache/ui-test playwright
node scripts/verify-workbench.cjs  # with make serve running; uses installed Google Chrome
```

The browser check navigates all eight pages, previews and downloads each committed evidence leaf, compares every download SHA-256 to its manifest, executes proof/tamper/replay/review/fault actions, runs five real counterfactual scenarios, and checks mobile overflow and browser errors.

The server binds only to `127.0.0.1`, exposes an allowlist of committed files, rejects cross-origin mutation requests and never accepts model-supplied shell commands or private signing keys. Agent runs are local subprocess jobs with captured logs; model authorization/usage failure appears as a failure and leaves the checkpoint for later resume.
