"""Run/resume the LangGraph portfolio agent with a persistent SQLite thread."""
import argparse
import json
import os
from pathlib import Path
import sys
import tempfile
from langchain_core.messages import HumanMessage
from langgraph.checkpoint.sqlite import SqliteSaver
from .runtime import AgentRuntime
from .provider import CodexProvider
from .tools import PortfolioContext,portfolio_tools
from .market_data import fault_campaign
from .portal import render
import hashlib
import shutil
import subprocess

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'scripts'))
from agent_workflow import seal,verify,write_json


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--goal',default='Research the portfolio using the full candidate budget, inspect the selected candidate and baseline risks, verify execution by replay, and deliver a visual evidence-based report.')
    parser.add_argument('--budget',type=int,default=20)
    parser.add_argument('--model',default='gpt-6.1-sol')
    parser.add_argument('--codex-bin',default=os.environ.get('CODEX_BIN','codex'))
    parser.add_argument('--timeout',type=int,default=120)
    parser.add_argument('--max-steps',type=int,default=32)
    parser.add_argument('--resume',type=Path)
    parser.add_argument('--stop-after-tools',type=int,default=0,help='checkpoint and stop after N tool nodes, for controlled handoff/resume')
    args=parser.parse_args()
    if not 2<=args.budget<=20:parser.error('budget must be 2–20')
    if args.resume:
        directory=args.resume.resolve()
        config=json.loads((directory/'session.json').read_text())
        # Resume uses the original model and objective, avoiding silent state drift.
        args.model,args.budget,args.goal=config['model'],config['budget'],config['goal']
    else:
        directory=Path(tempfile.mkdtemp(prefix='langgraph_',dir=ROOT/'artifacts'))
        write_json(directory/'session.json',{'model':args.model,'budget':args.budget,'goal':args.goal,'framework':'LangGraph'})
    context=PortfolioContext(ROOT,directory,args.codex_bin,args.model,args.timeout)
    runtime=AgentRuntime(CodexProvider(args.codex_bin,args.model,args.timeout),portfolio_tools(context),context,directory,args.max_steps,can_finish=lambda state: bool(state["facts"].get("replayed") and state["facts"].get("report") and state["facts"].get("data_audited") and state["facts"].get("stress_tested") and state["facts"].get("remaining_budget")==0))
    run_config={'configurable':{'thread_id':'portfolio'},'recursion_limit':args.max_steps*3+5}
    with SqliteSaver.from_conn_string(str(directory/'checkpoints.sqlite')) as saver:
        graph=runtime.compile(saver)
        (directory/'graph.mmd').write_text(graph.get_graph().draw_mermaid())
        initial=None if args.resume else {'goal':args.goal,'facts':{'remaining_budget':args.budget},'steps':0,'done':False,'final_answer':'','messages':[HumanMessage(content=args.goal)]}
        completed_tools=0
        try:
            for update in graph.stream(initial,run_config,stream_mode='updates',durability='sync'):
                if 'tools' in update:completed_tools+=1
                if args.stop_after_tools and completed_tools>=args.stop_after_tools:
                    print('AGENT_CHECKPOINTED '+str(directory),flush=True);return
            snapshot=graph.get_state(run_config)
            state=snapshot.values
            if not state.get('done'):raise RuntimeError('graph ended without successful finish')
            output=directory/'outputs'
            # Checkpoint DB is kept separately because it changes on every state update.
            (output/'agent_events.jsonl').write_bytes((directory/'events.jsonl').read_bytes())
            (output/'graph.mmd').write_bytes((directory/'graph.mmd').read_bytes())
            write_json(output/'workflow.json',{'framework':'LangGraph','status':'completed','steps':state['steps'],'facts':state['facts'],'final_answer':state['final_answer']})
            identity={}
            for source in sorted((ROOT/'agents/portfolio_agent').glob('*.py')):
                identity[str(source.relative_to(ROOT))]=hashlib.sha256(source.read_bytes()).hexdigest()
            for name in ('internal/evidence/v2.go','internal/experiment/counterfactual.go','internal/experiment/agent_search.go','internal/experiment/agent_replay.go','scripts/codex_propose.py','configs/v2_policy.json','requirements-agent.lock','web/index.html','web/app.js','web/style.css'):
                identity[name]=hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
            write_json(output/'source_identity.json',{'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'source_dirty':bool(subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True).strip()),'sha256':identity})
            faults=fault_campaign(ROOT,output/'fault_campaign.json')
            if faults['status']!='passed':raise RuntimeError('fault campaign failed')
            write_json(output/'v2_metadata.json',{'schema_version':'2.0.0','framework':'LangGraph','replay_verified':True,'market_data_scope':'21 historical daily closes; corroboration is not proof of truth','source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'review_state':'awaiting_independent_signatures','trace_sha256':hashlib.sha256((output/'agent_events.jsonl').read_bytes()).hexdigest()})
            seal(output);count=verify(output)
            bundle=directory/('bundle_v2_'+hashlib.sha256((output/'evidence_manifest.json').read_bytes()).hexdigest()[:16])
            if bundle.exists():
                subprocess.run([str(ROOT/'bin/project'),'evidence','verify-v2','--bundle',str(bundle)],check=True)
            else:
                subprocess.run([str(ROOT/'bin/project'),'evidence','pack-v2','--source',str(output),'--bundle',str(bundle)],check=True)
            render(directory,bundle)
            print('V2_BUNDLE '+str(bundle),flush=True)
            print(f'AGENT_COMPLETED {directory} evidence_files={count}',flush=True)
        except Exception as error:
            print(f'Agent stopped: {error}\nResume: --resume {directory}',file=sys.stderr)
            raise


if __name__=='__main__':main()
