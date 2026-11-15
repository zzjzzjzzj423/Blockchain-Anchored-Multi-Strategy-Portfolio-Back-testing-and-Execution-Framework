# End-to-end demonstration

Run `make demo` from a clean checkout with the fixed data snapshot and evidence bundle available. The script performs the following sequence without changing frozen reports:

1. checks the pinned environment and rebuilds the CLI;
2. validates the fixed market snapshot and configuration;
3. displays the frozen final holdout result;
4. verifies all 568 bundle leaves and inclusion proofs;
5. verifies the real `results/trades.csv` proof offline;
6. starts a fresh Anvil instance, redeploys the Registry, submits the frozen root, and verifies the real proof on chain;
7. runs 100 deterministic one-byte mutations and shows detection/localization counts;
8. checks the frozen 10/10 replay result, gas, overhead, and limitations.

The local EVM rehearsal is ephemeral. It does not overwrite the frozen M7 deployment receipt or claim public-chain finality.
