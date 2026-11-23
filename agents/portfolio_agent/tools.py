"""Registered, bounded portfolio tools; no model-supplied shell or file paths."""
from __future__ import annotations
import json
from pathlib import Path
import shutil
import subprocess
import sys
from langchain_core.tools import tool
from pydantic import Field
from typing import Annotated
sys.path.insert(0,str(Path(__file__).resolve().parents[2]/'scripts'))
from agent_workflow import dashboard,write_json
from .market_data import quality,cross_reference
from .runtime import ExternalCapacityError


class PortfolioContext:
    def __init__(self,root:Path,directory:Path,binary:str,model:str,timeout:int):
        self.root,self.directory,self.binary,self.model,self.timeout=root,directory,binary,model,timeout
        self.state={}

    def command(self,args):
        lines=[]
        with (self.directory/'tools.log').open('a') as log:
            log.write(json.dumps(args)+'\n');log.flush()
            process=subprocess.Popen(args,cwd=self.root,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
            for line in process.stdout:
                log.write(line);log.flush();lines.append(line)
                print(line,end='',flush=True)
            code=process.wait()
        if code:
            detail=''.join(lines)[-4000:]
            if 'usageLimitExceeded' in detail or 'usage limit' in detail:raise ExternalCapacityError(detail)
            raise RuntimeError(detail)
        return ''.join(lines)

    def result(self,observation,**updates):
        return {'observation':observation,'facts':{**self.state['facts'],**updates}}

    def research(self):
        path=self.state['facts'].get('research')
        if not path:raise ValueError('run_research must succeed first')
        return self.directory/path


def portfolio_tools(context:PortfolioContext):
    @tool
    def prepare_environment() -> dict:
        """Build Go backend and validate fixed data, downloading only when absent. Required before research."""
        context.command(['make','build'])
        context.command(['make','data-validate'] if (context.root/'data/normalized/daily_ohlcv.csv').exists() else ['make','data'])
        return context.result({'ready':True,'development_interval':['2021-01-01','2024-12-31']},ready=True)

    @tool
    def audit_market_data() -> dict:
        """Validate all development OHLCV and frozen configuration; cross-reference 21 historical closes against independent Coinbase snapshots. Block research on missing, non-finite, corrupted, disputed or unavailable feeds. Requires environment ready."""
        if not context.state['facts'].get('ready'):raise ValueError('prepare_environment must succeed first')
        output=context.directory/'market_data'
        checked=quality(context.root,output)
        if checked['status']!='passed':raise ValueError('market/config quality blocked: '+json.dumps(checked))
        compared=cross_reference(context.root/'data/normalized/daily_ohlcv.csv',output)
        if compared['status']!='corroborated':raise ValueError('cross-source validation blocked: '+json.dumps(compared))
        return context.result({'quality':checked,'cross_source_status':compared['status'],'compared_rows':compared['compared_rows'],'coverage':compared['comparison_interval'],'truth_boundary':compared['truth_claim']},data_audited=True)

    @tool
    def run_research(budget:Annotated[int,Field(ge=2,le=20)]) -> dict:
        """Run adaptive model-driven strategy parameter research and Go execution. Spend budget unique candidates (including fixed baseline). Returns metrics for decisions. Requires environment ready and enough remaining budget."""
        facts=context.state['facts']
        if not facts.get('ready'):raise ValueError('prepare_environment must succeed first')
        if not facts.get('data_audited'):raise ValueError('audit_market_data must succeed first')
        if budget>facts['remaining_budget']:raise ValueError('candidate budget exceeds remaining allowance')
        output=context.command([str(context.root/'bin/project'),'experiment','agent-search','--budget',str(budget),'--model',context.model,'--codex-bin',context.binary,'--timeout',str(context.timeout)])
        source=context.root/output.strip().split('output=')[-1]
        relative='research_'+source.name
        destination=context.directory/relative
        shutil.copytree(source,destination,dirs_exist_ok=True)
        summary=json.loads((destination/'summary.json').read_text())
        folds=json.loads((destination/'fold_metrics.json').read_text())['candidates']
        observations=[{'candidate_id':c['candidate_id'],'parameters':c['parameters'],**c['validation']['aggregate']} for c in folds]
        return context.result({'summary':summary,'candidate_comparison':observations},research=relative,remaining_budget=facts['remaining_budget']-budget,replayed=False,report=False,inspected=[])

    @tool
    def inspect_candidate(candidate_id:str) -> dict:
        """Inspect exact per-fold risk, cost, return and parameters for a candidate in the latest research. Use before interpreting or reporting results."""
        path=context.research()
        folds=json.loads((path/'fold_metrics.json').read_text())['candidates']
        candidate=next((c for c in folds if c['candidate_id']==candidate_id),None)
        if candidate is None:raise ValueError('unknown candidate_id; use a full ID from run_research')
        return context.result(candidate,inspected=list(set(context.state['facts'].get('inspected',[])+[candidate_id])))

    @tool
    def replay_research() -> dict:
        """Re-run recorded candidates and final execution with zero model calls. Compare all fold metrics and execution artifacts byte for byte."""
        path=context.research()
        output=context.command([str(context.root/'bin/project'),'experiment','agent-replay','--bundle',str(path)])
        return context.result({'replay':output.strip(),'model_calls':0},replayed=True)

    @tool
    def write_report(analysis:str,risks:str,next_actions:str) -> dict:
        """Write the agent's evidence-based interpretation and generate an equity/candidate report. Requires successful replay and inspection of selected candidate. Describe development selection bias; do not claim holdout or future profit."""
        path=context.research()
        facts=context.state['facts']
        selected=json.loads((path/'summary.json').read_text())['selected_candidate_id']
        if not facts.get('replayed'):raise ValueError('replay_research must succeed before report')
        if selected not in facts.get('inspected',[]):raise ValueError('inspect selected candidate before report')
        if not all(v.strip() for v in (analysis,risks,next_actions)):raise ValueError('report fields must be nonempty')
        output=context.directory/'outputs'
        output.mkdir(exist_ok=True)
        for source in path.iterdir():
            if source.is_file():shutil.copy2(source,output/source.name)
        shutil.copytree(context.directory/'market_data',output/'market_data',dirs_exist_ok=True)
        shutil.copy2(context.root/'configs/v2_policy.json',output/'market_data/policy.json')
        review={'analysis':analysis,'risks':risks,'next_actions':next_actions}
        write_json(output/'agent_review.json',review)
        dashboard(output,review)
        return context.result({'report':'outputs/index.html','candidate':selected},report=True)

    return {registered.name:registered for registered in (prepare_environment,audit_market_data,run_research,inspect_candidate,replay_research,write_report)}
