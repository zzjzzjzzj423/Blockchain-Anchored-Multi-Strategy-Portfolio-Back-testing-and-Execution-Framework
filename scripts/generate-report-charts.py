#!/usr/bin/env python3
"""Generate deterministic SVG figures directly from the frozen evidence bundle."""

import csv
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BUNDLE = ROOT / "artifacts" / "098376de7f855b9e"
OUT = ROOT / "docs" / "figures"
COLORS = {"buy_and_hold": "#2563eb", "sma_trend": "#16a34a", "zscore_mean_reversion": "#dc2626", "ensemble": "#7c3aed"}


def rows(path):
    with path.open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def svg_line_chart(title, series, y_label, target, zero=False):
    width, height, left, top, right, bottom = 960, 520, 78, 54, 28, 64
    values = [value for points in series.values() for value in points]
    low, high = min(values), max(values)
    if zero:
        low, high = min(low, 0), max(high, 0)
    padding = max((high - low) * 0.08, 0.01)
    low, high = low - padding, high + padding
    plot_w, plot_h = width - left - right, height - top - bottom
    count = max(len(points) for points in series.values())
    x = lambda index: left + plot_w * index / max(count - 1, 1)
    y = lambda value: top + plot_h * (high - value) / (high - low)
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">',
             '<rect width="100%" height="100%" fill="#ffffff"/>', f'<text x="{left}" y="30" font-family="sans-serif" font-size="22" font-weight="700">{title}</text>']
    for tick in range(6):
        value = low + (high - low) * tick / 5
        yy = y(value)
        parts += [f'<line x1="{left}" y1="{yy:.2f}" x2="{width-right}" y2="{yy:.2f}" stroke="#e5e7eb"/>',
                  f'<text x="{left-10}" y="{yy+4:.2f}" text-anchor="end" font-family="monospace" font-size="12" fill="#4b5563">{value:.2f}</text>']
    if low <= 0 <= high:
        parts.append(f'<line x1="{left}" y1="{y(0):.2f}" x2="{width-right}" y2="{y(0):.2f}" stroke="#9ca3af"/>')
    parts += [f'<line x1="{left}" y1="{top}" x2="{left}" y2="{height-bottom}" stroke="#111827"/>',
              f'<line x1="{left}" y1="{height-bottom}" x2="{width-right}" y2="{height-bottom}" stroke="#111827"/>',
              f'<text transform="translate(18 {height/2}) rotate(-90)" text-anchor="middle" font-family="sans-serif" font-size="13">{y_label}</text>',
              f'<text x="{left}" y="{height-28}" font-family="sans-serif" font-size="12" fill="#4b5563">2025-01-01</text>',
              f'<text x="{width-right}" y="{height-28}" text-anchor="end" font-family="sans-serif" font-size="12" fill="#4b5563">2025-12-31</text>']
    legend_x = left
    for name, points in series.items():
        path = " ".join(("M" if index == 0 else "L") + f"{x(index):.2f},{y(value):.2f}" for index, value in enumerate(points))
        color = COLORS[name]
        parts.append(f'<path d="{path}" fill="none" stroke="{color}" stroke-width="2"/>')
        parts += [f'<line x1="{legend_x}" y1="{height-12}" x2="{legend_x+22}" y2="{height-12}" stroke="{color}" stroke-width="3"/>',
                  f'<text x="{legend_x+27}" y="{height-8}" font-family="sans-serif" font-size="12">{name.replace("_", " ")}</text>']
        legend_x += 190
    parts.append('</svg>\n')
    target.write_text("".join(parts), encoding="utf-8")


def svg_bar_chart(title, labels, values, y_label, target, colors=None):
    width, height, left, top, right, bottom = 960, 520, 90, 54, 28, 105
    low, high = min(0, min(values)), max(0, max(values))
    padding = max((high - low) * 0.12, 0.01)
    low, high = low - padding, high + padding
    plot_w, plot_h = width - left - right, height - top - bottom
    y = lambda value: top + plot_h * (high - value) / (high - low)
    slot = plot_w / len(labels)
    bar_w = slot * 0.58
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">', '<rect width="100%" height="100%" fill="#fff"/>',
             f'<text x="{left}" y="30" font-family="sans-serif" font-size="22" font-weight="700">{title}</text>']
    for tick in range(6):
        value = low + (high-low)*tick/5; yy = y(value)
        parts += [f'<line x1="{left}" y1="{yy:.2f}" x2="{width-right}" y2="{yy:.2f}" stroke="#e5e7eb"/>', f'<text x="{left-10}" y="{yy+4:.2f}" text-anchor="end" font-family="monospace" font-size="12">{value:.2f}</text>']
    parts.append(f'<line x1="{left}" y1="{y(0):.2f}" x2="{width-right}" y2="{y(0):.2f}" stroke="#111827"/>')
    for index, (label, value) in enumerate(zip(labels, values)):
        xx = left + slot*index + (slot-bar_w)/2; yy = min(y(value), y(0)); hh = abs(y(value)-y(0)); color = colors[index] if colors else "#7c3aed"
        parts += [f'<rect x="{xx:.2f}" y="{yy:.2f}" width="{bar_w:.2f}" height="{max(hh,1):.2f}" fill="{color}" rx="3"/>',
                  f'<text x="{xx+bar_w/2:.2f}" y="{yy-7:.2f}" text-anchor="middle" font-family="monospace" font-size="12">{value:.3f}</text>',
                  f'<text transform="translate({xx+bar_w/2:.2f} {height-bottom+16}) rotate(25)" text-anchor="start" font-family="sans-serif" font-size="12">{label}</text>']
    parts += [f'<text transform="translate(20 {height/2}) rotate(-90)" text-anchor="middle" font-family="sans-serif" font-size="13">{y_label}</text>', '</svg>\n']
    target.write_text("".join(parts), encoding="utf-8")


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    base = rows(BUNDLE / "results" / "base_strategy_returns.csv")
    ensemble = rows(BUNDLE / "results" / "ensemble_returns.csv")
    grouped = {name: [] for name in COLORS}
    for row in base + ensemble:
        grouped[row["strategy"]].append(float(row["equity"]))
    normalized = {name: [value / values[0] for value in values] for name, values in grouped.items()}
    svg_line_chart("2025 Final Holdout: Normalized Equity", normalized, "Growth of 1.0", OUT / "equity_curves.svg")
    drawdowns = {}
    for name, values in normalized.items():
        peak = values[0]; points = []
        for value in values:
            peak = max(peak, value); points.append(value / peak - 1)
        drawdowns[name] = points
    svg_line_chart("2025 Final Holdout: Drawdown", drawdowns, "Drawdown", OUT / "drawdowns.svg", True)
    allocations = rows(BUNDLE / "results" / "ensemble_weights.csv")
    weights = {name: [] for name in COLORS if name != "ensemble"}
    for row in allocations:
        weights[row["strategy"]].append(float(row["strategy_weight"]))
    svg_line_chart("Lagged Ensemble Strategy Weights", weights, "Allocator weight", OUT / "ensemble_weights.svg", True)
    metrics = json.loads((BUNDLE / "results" / "overall_metrics.json").read_text())["metrics"]
    names = list(COLORS)
    svg_bar_chart("Annualized Transaction-Cost Drag", [name.replace("_", " ") for name in names], [float(metrics[name]["annualized_cost_drag"]) for name in names], "Annualized cost drag", OUT / "cost_drag.svg", [COLORS[name] for name in names])
    regimes = json.loads((BUNDLE / "results" / "regime_metrics.json").read_text())["regimes"]
    regime_names = ["Bull/LowVol", "Bull/HighVol", "Bear/LowVol", "Bear/HighVol"]
    svg_bar_chart("Ensemble Sharpe by Lagged Market Regime", regime_names, [float(regimes[name]["metrics"]["ensemble"]["sharpe"]) for name in regime_names], "Sharpe ratio", OUT / "regime_sharpe.svg", ["#60a5fa", "#2563eb", "#f59e0b", "#dc2626"])
    sources = ["results/base_strategy_returns.csv", "results/ensemble_returns.csv", "results/ensemble_weights.csv", "results/overall_metrics.json", "results/regime_metrics.json"]
    charts = sorted(OUT.glob("*.svg"))
    sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    manifest = {"bundle_run_id": "098376de7f855b9e", "chart_sha256": {path.name: sha(path) for path in charts}, "schema_version": "1.0.0", "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(), "source_sha256": {name: sha(BUNDLE / name) for name in sources}, "status": "passed"}
    (ROOT / "reports" / "m9_chart_manifest.json").write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
