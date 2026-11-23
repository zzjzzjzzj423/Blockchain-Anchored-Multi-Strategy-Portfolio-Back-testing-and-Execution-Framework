# Historical market snapshots

Research interval: **2021-01-01 through 2025-12-31**, daily UTC candles, BTC/ETH/SOL. These are historical observations, not live market data.

- `binance_daily.csv`: 5,478 records (1,826 per asset). Exact bytes match the frozen Binance Vision normalized SHA-256 in `configs/v2_policy.json`. Original monthly archive URLs, official checksums and data validation are recorded in `data/data_manifest.json` and the existing data pipeline.
- `coinbase_daily.csv`: 5,311 actual Coinbase Exchange records. BTC and ETH each cover all 1,826 days; SOL has 1,659 observed days and 167 missing early days. Missing reference candles are not synthesized or forward-filled.
- `*-USD_START_END.json`: the 21 original Coinbase API response snapshots. Each query window is at most 280 daily candles; out-of-window response rows are excluded during normalization.
- `coverage.json`: source URLs, raw/normalized SHA-256 hashes, exact missing dates, row counts, quote-basis comparison and observed discrepancies.

The primary source is USDT-quoted and the reference is USD-quoted. Exchange and quote differences are explicitly retained. Source consistency does not prove economic truth. Provider terms apply; no claim is made that the feeds are public domain. Binance attribution is also recorded in `data/LICENSE_DATA.md`.

Official Coinbase endpoint documentation: https://docs.cdp.coinbase.com/api-reference/exchange-api/rest-api/products/get-product-candles

`make reference-data` rebuilds the normalized reference and coverage from cached raw responses, fetching only absent snapshots. `verify_inventory()` independently checks normalized hashes, all raw response hashes, reconstructed OHLCV values, primary completeness and recorded secondary gaps without network access.
