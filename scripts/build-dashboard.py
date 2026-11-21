#!/usr/bin/env python3
"""Build a self-contained, read-only report viewer from committed evidence."""

import hashlib
import html
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
REPORTS = ROOT / "reports"
FIGURES = ROOT / "docs" / "figures"
OUTPUT = ROOT / "docs" / "dashboard" / "index.html"


def read(name):
    return json.loads((REPORTS / name).read_text())


def main():
    manifest = read("m9_chart_manifest.json")
    charts = [
        ("equity_curves.svg", "Equity", "2025 净值 / Equity curves"),
        ("drawdowns.svg", "Drawdown", "回撤 / Drawdowns"),
        ("ensemble_weights.svg", "Allocation", "策略权重 / Strategy weights"),
        ("cost_drag.svg", "Costs", "交易成本 / Cost drag"),
        ("regime_sharpe.svg", "Regimes", "市场状态 / Regime Sharpe"),
    ]
    for name, _, _ in charts:
        digest = hashlib.sha256((FIGURES / name).read_bytes()).hexdigest()
        if digest != manifest["chart_sha256"][name]:
            raise ValueError(f"committed chart hash mismatch: {name}")
    holdout = read("m5_final_holdout.json")["metrics"]["ensemble"]
    bundle = read("m6_evidence_bundle.json")
    replay = read("m8_replay.json")
    mutation = read("m8_mutation.json")
    chain = read("m7_chain_validation.json")
    pilot_dir = ROOT / "docs" / "agent_pilot"
    pilot = json.loads((pilot_dir / "summary.json").read_text())
    for name, key in (("search_trace.jsonl", "trace_sha256"), ("fold_metrics.json", "fold_metrics_sha256")):
        if hashlib.sha256((pilot_dir / name).read_bytes()).hexdigest() != pilot[key]:
            raise ValueError(f"agent pilot artifact hash mismatch: {name}")
    baseline = json.loads((pilot_dir / "search_trace.jsonl").read_text().splitlines()[0])
    if pilot["selected_candidate_id"] != baseline["candidate_id"]:
        raise ValueError("agent pilot winner no longer matches the baseline")
    pct = lambda value: f"{float(value) * 100:.2f}%"
    nav = "".join(
        f'<button class="tab{" active" if i == 0 else ""}" data-target="chart-{i}" aria-selected="{"true" if i == 0 else "false"}">{html.escape(label)}</button>'
        for i, (_, label, _) in enumerate(charts)
    )
    panels = "".join(
        f'<section id="chart-{i}" class="panel{" active" if i == 0 else ""}"><h2>{html.escape(title)}</h2><img src="../figures/{name}" alt="{html.escape(title)}" loading="lazy"><p>SHA-256 {manifest["chart_sha256"][name]}</p></section>'
        for i, (name, _, title) in enumerate(charts)
    )
    page = f'''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Portfolio Proof | Evidence Dashboard</title>
<style>
:root{{--bg:#08101a;--card:#111e2b;--line:#244056;--text:#eaf3f6;--muted:#9eb4c1;--accent:#60e6c5;--blue:#72b6ff}}
*{{box-sizing:border-box}}body{{margin:0;background:radial-gradient(circle at 85% 0,#173b48 0,transparent 32%),var(--bg);color:var(--text);font:16px/1.55 system-ui,-apple-system,sans-serif}}a{{color:var(--accent)}}.wrap{{max-width:1220px;margin:auto;padding:42px 24px 70px}}.eyebrow{{color:var(--accent);font-weight:700;letter-spacing:.16em;text-transform:uppercase;font-size:.75rem}}h1{{font-size:clamp(2rem,5vw,4.2rem);line-height:1.08;max-width:850px;margin:16px 0}}.lede{{color:var(--muted);max-width:760px;font-size:1.08rem}}.meta{{display:flex;gap:12px;flex-wrap:wrap;margin:26px 0 34px}}.pill{{border:1px solid var(--line);border-radius:30px;padding:6px 13px;color:var(--muted);font-size:.86rem}}.grid{{display:grid;grid-template-columns:repeat(4,1fr);gap:14px}}.card,.visual,.evidence{{background:var(--card);border:1px solid var(--line);border-radius:18px}}.card{{padding:22px}}.card span{{display:block;color:var(--muted);font-size:.82rem}}.card strong{{display:block;font-size:2rem;margin:8px 0 2px}}.card small{{color:var(--muted)}}.visual{{margin-top:20px;padding:22px}}.tabs{{display:flex;gap:8px;flex-wrap:wrap;margin:18px 0}}button{{font:inherit;cursor:pointer}}.tab{{border:1px solid var(--line);background:#0b1722;color:var(--muted);padding:9px 15px;border-radius:10px}}.tab.active,.tab:hover{{background:var(--accent);color:#08101a;border-color:var(--accent)}}.panel{{display:none}}.panel.active{{display:block}}.panel h2{{font-size:1.2rem}}.panel img{{display:block;width:100%;background:white;border-radius:10px}}.panel p{{color:var(--muted);font-size:.72rem;overflow-wrap:anywhere}}.evidence{{margin-top:20px;padding:24px}}.evidence-grid{{display:grid;grid-template-columns:repeat(3,1fr);gap:18px}}.evidence strong{{color:var(--accent);display:block;font-size:1.2rem}}.evidence span{{color:var(--muted);font-size:.86rem}}code{{font-size:.8rem;overflow-wrap:anywhere}}.note{{border-left:3px solid var(--blue);padding-left:16px;color:var(--muted);margin-top:22px}}footer{{margin-top:34px;color:var(--muted);font-size:.86rem}}@media(max-width:800px){{.grid{{grid-template-columns:repeat(2,1fr)}}.evidence-grid{{grid-template-columns:1fr}}}}@media(max-width:460px){{.grid{{grid-template-columns:1fr}}.wrap{{padding:28px 16px}}}}
</style></head><body><main class="wrap">
<div class="eyebrow">MSBD5017 · Verifiable backtesting</div>
<h1>Portfolio results, with a checkable trail.</h1>
<p class="lede">A read-only view of the repository's frozen 2025 holdout and evidence reports. Charts are verified against the committed SHA-256 manifest at build time.</p>
<div class="meta"><span class="pill">BTC · ETH · SOL</span><span class="pill">2025 holdout</span><span class="pill">Local EVM · chain 31337</span><span class="pill">Run {html.escape(bundle["run_id"])}</span></div>
<div class="grid"><div class="card"><span>Ensemble return</span><strong>{pct(holdout["total_return"])}</strong><small>After transaction costs</small></div><div class="card"><span>Maximum drawdown</span><strong>{pct(holdout["maximum_drawdown"])}</strong><small>2025 holdout</small></div><div class="card"><span>Sharpe ratio</span><strong>{float(holdout["sharpe"]):.3f}</strong><small>Historical, not a forecast</small></div><div class="card"><span>Total cost ratio</span><strong>{pct(holdout["total_cost_ratio"])}</strong><small>Commission + slippage</small></div></div>
<div class="visual"><div class="eyebrow">Explore the result</div><div class="tabs" role="tablist" aria-label="Result charts">{nav}</div>{panels}</div>
<div class="evidence"><div class="eyebrow">Evidence status</div><h2>From run to root</h2><div class="evidence-grid"><div><strong>{bundle["leaf_count"]} leaves</strong><span>Canonical evidence files committed to the Merkle root</span></div><div><strong>{replay["artifact_identical_runs"]}/{replay["replay_count"]} replays</strong><span>Byte-identical artifacts and roots in the recorded QA run</span></div><div><strong>{mutation["detected"]}/{mutation["count"]} mutations</strong><span>One-byte changes detected in the recorded QA run</span></div></div><p>Merkle root<br><code>{html.escape(bundle["merkle_root"])}</code></p><p>Local registry <code>{html.escape(chain["contract_address"])}</code> · chain {chain["chain_id"]}</p><p class="note">The root commits to exact bytes. It does not establish market-data truth or future returns. This dashboard displays committed reports; independent replay requires the separately distributed raw snapshot and full evidence bundle.</p></div>
<div class="evidence"><div class="eyebrow">Optional AI pilot · development only</div><h2>Codex proposes; the frozen evaluator decides.</h2><div class="evidence-grid"><div><strong>{pilot["codex_calls"]} AI proposals</strong><span>After a fixed baseline candidate</span></div><div><strong>{pilot["manual_fallbacks"]} fallbacks</strong><span>Every candidate validated in the frozen space</span></div><div><strong>Baseline retained</strong><span>The AI proposals did not improve the selected development score in this three-candidate pilot</span></div></div><p class="note">This pilot is separate from the formal M4 selection and 2025 holdout. Its <a href="../agent_pilot/search_trace.jsonl">proposal trace</a>, <a href="../agent_pilot/fold_metrics.json">fold metrics</a>, and <a href="../agent_pilot/summary.json">hash summary</a> are published for review.</p></div>
<footer>Source: <a href="../../reports/m5_final_holdout.json">holdout report</a> · <a href="../../reports/m6_evidence_bundle.json">evidence report</a> · <a href="../../reports/m9_chart_manifest.json">chart manifest</a>. No external scripts, network requests, or live trading.</footer>
</main><script>document.querySelectorAll('.tab').forEach(button=>button.addEventListener('click',()=>{{document.querySelectorAll('.tab').forEach(x=>{{x.classList.remove('active');x.setAttribute('aria-selected','false')}});document.querySelectorAll('.panel').forEach(x=>x.classList.remove('active'));button.classList.add('active');button.setAttribute('aria-selected','true');document.getElementById(button.dataset.target).classList.add('active')}}));</script></body></html>'''
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(page)
    print(f"dashboard built: {OUTPUT.relative_to(ROOT)}; {len(charts)} chart hashes verified")


if __name__ == "__main__":
    main()
