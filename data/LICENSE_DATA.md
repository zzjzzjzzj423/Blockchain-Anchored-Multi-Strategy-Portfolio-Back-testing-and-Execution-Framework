# Market Data License and Attribution

## Dataset

- Provider: Binance Vision / Binance public market data.
- Dataset: Spot monthly `1d` klines for `BTCUSDT`, `ETHUSDT`, and `SOLUSDT`.
- Period: 2021-01 through 2025-12, UTC.
- URL pattern: `https://data.binance.vision/data/spot/monthly/klines/<SYMBOL>/1d/<SYMBOL>-1d-<YYYY-MM>.zip`.
- Integrity source: the corresponding provider-hosted `.zip.CHECKSUM` document.
- Frozen access date: 2026-11-14.

## Terms and attribution

The data is used only for non-commercial academic course research. The frozen specification records that Binance Vision Dataset Terms apply CC BY-NC-SA 4.0 requirements, including attribution and share-alike obligations for redistribution.

- Dataset terms: <https://github.com/binance/binance-public-data/blob/master/TERMS_AND_CONDITIONS.md>
- Public data repository: <https://github.com/binance/binance-public-data/>
- CC BY-NC-SA 4.0: <https://creativecommons.org/licenses/by-nc-sa/4.0/>

This project is independent and is not sponsored, endorsed by, or affiliated with Binance. Any redistribution of raw or derived data must retain the applicable attribution, non-commercial, and share-alike notices. Commercial use requires a separate license review.

Raw archives and extracted CSV files are retained under `data/raw/` for local reproducibility and are not committed to Git. Their provider URLs, official checksums, computed hashes, byte sizes, row counts, and timestamp units are recorded in `data/data_manifest.json`.
