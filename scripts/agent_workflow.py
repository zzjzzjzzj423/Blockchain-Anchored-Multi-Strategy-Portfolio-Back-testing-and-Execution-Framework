#!/usr/bin/env python3
"""End-to-end bounded research agent. No live orders or v1 holdout tuning."""
import argparse
import csv
import hashlib
import html
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / 'scripts'))
from codex_propose import propose


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')


def seal(directory):
    files = {str(p.relative_to(directory)): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(directory.rglob('*')) if p.is_file() and p != directory / 'evidence_manifest.json'}
    # A versioned SHA-256 content manifest, distinct from the frozen v1 Merkle tree.
    payload = json.dumps(files, sort_keys=True, separators=(',', ':')).encode()
    write_json(directory / 'evidence_manifest.json', {
        'schema_version': 'agent-workflow-1', 'files': files,
        'manifest_sha256': hashlib.sha256(payload).hexdigest(), 'onchain_anchored': False})


def verify(directory):
    manifest = json.loads((directory / 'evidence_manifest.json').read_text())
    payload = json.dumps(manifest['files'], sort_keys=True, separators=(',', ':')).encode()
    if hashlib.sha256(payload).hexdigest() != manifest['manifest_sha256']:
        raise ValueError('manifest digest differs')
    actual = {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p != directory / 'evidence_manifest.json'}
    if actual != set(manifest['files']):
        raise ValueError('evidence file set differs')
    for name, digest in manifest['files'].items():
        path = (directory / name).resolve()
        if directory.resolve() not in path.parents:
            raise ValueError('unsafe manifest path')
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError('evidence differs: ' + name)
    return len(actual)


def dashboard(directory, review):
    metrics = json.loads((directory / 'development_metrics.json').read_text())['metrics']['ensemble']
    rows = list(csv.DictReader((directory / 'ensemble_returns.csv').open()))
    equity = [float(r['equity']) for r in rows]
    lo, hi = min(equity), max(equity)
    points = ' '.join(f'{i*1000/max(1,len(equity)-1):.2f},{260-(v-lo)*240/max(1,hi-lo):.2f}' for i,v in enumerate(equity))
    folds = json.loads((directory / 'fold_metrics.json').read_text())['candidates']
    table = ''.join('<tr><td>'+str(i+1)+'</td><td>'+html.escape(c['candidate_id'][:12])+'</td><td>'+c['validation']['aggregate']['median_validation_sharpe']+'</td><td>'+c['validation']['aggregate']['worst_validation_maximum_drawdown']+'</td></tr>' for i,c in enumerate(folds))
    sections = ''.join('<h2>'+html.escape(k.replace('_',' ').title())+'</h2><p>'+html.escape(v)+'</p>' for k,v in review.items())
    (directory / 'index.html').write_text('''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Agent research run</title><style>body{font:16px/1.6 system-ui;background:#091522;color:#e5f0f8;max-width:1100px;margin:auto;padding:32px}h1{font-size:40px}a{color:#65e3c1}svg{width:100%;background:#132539;border-radius:16px}table{width:100%;border-collapse:collapse}td,th{padding:9px;border-bottom:1px solid #395064;text-align:left}p{white-space:pre-wrap}summary{cursor:pointer}code{overflow-wrap:anywhere}</style><h1>Agent portfolio research</h1><p>Development interval: 2021–2024. Selected strategy execution under the frozen cost and accounting model. This is an in-sample research run, not an independent performance claim.</p>'''+f'<p>Return: {float(metrics["total_return"]):.2%} · Maximum drawdown: {float(metrics["maximum_drawdown"]):.2%} · Sharpe: {float(metrics["sharpe"]):.3f}</p><svg viewBox="0 0 1000 290" role="img" aria-label="Development equity curve"><polyline points="{points}" fill="none" stroke="#65e3c1" stroke-width="2"/></svg><p>{rows[0]["date"]} → {rows[-1]["date"]}</p>'+sections+'<details open><summary>Candidate comparison</summary><table><tr><th>#</th><th>Candidate</th><th>Median validation Sharpe</th><th>Worst fold drawdown</th></tr>'+table+'</table></details><p><a href="workflow.json">Workflow status</a> · <a href="search_trace.jsonl">Agent trace</a> · <a href="ensemble_trades.csv">Trades</a> · <a href="ensemble_ledger.csv">Ledger</a> · <a href="evidence_manifest.json">Evidence manifest</a></p></html>')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--budget', type=int, default=20)
    parser.add_argument('--codex-bin', default=os.environ.get('CODEX_BIN', 'codex'))
    parser.add_argument('--model', default=os.environ.get('AGENT_MODEL', 'gpt-6.1-sol'))
    parser.add_argument('--timeout', type=int, default=120)
    parser.add_argument('--verify', type=Path)
    args = parser.parse_args()
    if args.verify:
        print(f'evidence verified: {verify(args.verify)} files'); return
    if not 2 <= args.budget <= 20: parser.error('budget must be 2–20')
    directory = Path(tempfile.mkdtemp(prefix='workflow_', dir=ROOT / 'artifacts'))
    status = {'status': 'running', 'budget': args.budget, 'model': args.model or 'Codex account default', 'stages': [], 'development_only': True, 'model_calls_in_replay': 0}
    def stage(name, operation):
        print(f'[{name}] starting', flush=True)
        entry = {'stage': name, 'status': 'running'}
        status['stages'].append(entry); write_json(directory/'workflow.json', status)
        start = time.monotonic()
        result = operation()
        entry.update(status='completed', elapsed_seconds=round(time.monotonic()-start, 3))
        write_json(directory/'workflow.json', status)
        print(f'[{name}] completed', flush=True)
        return result
    def tool(arguments):
        lines = []
        with (directory/'tool_log.txt').open('a') as log:
            log.write(json.dumps(arguments)+'\n'); log.flush()
            process = subprocess.Popen(arguments, cwd=ROOT, stdout=subprocess.PIPE,
                                       stderr=subprocess.STDOUT, text=True)
            for line in process.stdout:
                lines.append(line); log.write(line); log.flush()
                print(line, end='', flush=True)
            code = process.wait()
        if code: raise RuntimeError(''.join(lines))
        return ''.join(lines)
    def agent(request):
        failures = []
        for attempt in range(3):
            try:
                answer = propose(request,args.codex_bin,args.model,args.timeout)
                if failures: write_json(directory/(request['task']+'_retries.json'),failures)
                return answer
            except (ValueError, RuntimeError, TimeoutError, OSError) as error:
                failures.append(str(error))
                write_json(directory/(request['task']+'_retries.json'),failures)
                print(f"Agent {request['task']} attempt {attempt+1} failed: {error}",flush=True)
        raise RuntimeError('Agent retries exhausted: '+failures[-1])
    try:
        stage('build', lambda: tool(['make','build']))
        stage('data_validation',lambda: tool(['make','data'] if not (ROOT/'data/normalized/daily_ohlcv.csv').exists() else ['make','data-validate']))
        inputs = {}
        for name in ('data/normalized/daily_ohlcv.csv', 'configs/strategies.json', 'configs/costs.json', 'scripts/agent_workflow.py', 'scripts/codex_propose.py', 'internal/experiment/agent_search.go', 'internal/experiment/agent_replay.go'):
            inputs[name] = hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
        write_json(directory/'input_identity.json', {'sha256':inputs,'source_commit':tool(['git','rev-parse','HEAD']).strip(),'development_interval':['2021-01-01','2024-12-31']})
        plan = stage('agent_plan',lambda: agent({'task':'plan','budget':args.budget,'development':'2021–2024','goal':'Explore trend, mean-reversion and adaptive allocator parameters; rank using four frozen walk-forward validation folds.'}))
        write_json(directory/'agent_plan.json',plan)
        output = stage('agent_search_and_execution',lambda: tool([str(ROOT/'bin/project'),'experiment','agent-search','--budget',str(args.budget),'--codex-bin',args.codex_bin,'--timeout',str(args.timeout),'--model',args.model]))
        run = ROOT / output.strip().split('output=')[-1]
        for path in run.iterdir():
            if path.is_file(): shutil.copy2(path,directory/path.name)
        stage('deterministic_replay',lambda: tool([str(ROOT/'bin/project'),'experiment','agent-replay','--bundle',str(directory)]))
        folds=json.loads((directory/'fold_metrics.json').read_text())
        summary=json.loads((directory/'summary.json').read_text())
        review=stage('agent_review',lambda: agent({'task':'review','selection':summary,'development_folds':folds}))
        write_json(directory/'agent_review.json',review)
        stage('visual_report',lambda: dashboard(directory,review))
        status['status']='completed'; write_json(directory/'workflow.json',status)
        seal(directory); verify(directory)
        print('WORKFLOW_COMPLETED '+str(directory),flush=True)
    except Exception as error:
        status['status']='failed';status['error']=str(error)
        status['stages'][-1]['status']='failed';write_json(directory/'workflow.json',status)
        print(f'Workflow failed; diagnostic artifacts: {directory}\n{error}',file=sys.stderr)
        raise SystemExit(1)


if __name__ == '__main__': main()
