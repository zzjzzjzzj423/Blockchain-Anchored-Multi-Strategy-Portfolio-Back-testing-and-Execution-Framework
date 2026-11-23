"""Loopback-only application server for the portfolio research workbench."""
from __future__ import annotations
import argparse,csv,io,json,mimetypes,os,re,subprocess,sys,tempfile,threading,time,uuid
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from urllib.parse import urlsplit,parse_qs
from .market_data import verify_market_evidence,fault_campaign
from .review import verify_reviews
from .dataset import verify_inventory,ensure_primary as ensure_dataset
ROOT=Path(__file__).resolve().parents[2]
JOBS={};LOCK=threading.Lock()


def context(run='demo'):
    if run=='v2':return ROOT/'docs/v2_demo/bundle',ROOT/'docs/v2_demo/reviews'
    if run=='demo':
        if (ROOT/'docs/research_demo/bundle').exists():
            custom=ROOT/'artifacts/web_reviews/demo'
            return ROOT/'docs/research_demo/bundle',custom if (custom/'policy.json').exists() else ROOT/'docs/research_demo/reviews'
        custom=ROOT/'artifacts/web_reviews/demo'
        return ROOT/'docs/v2_demo/bundle',custom if (custom/'policy.json').exists() else ROOT/'docs/v2_demo/reviews'
    if not re.fullmatch(r'langgraph_[A-Za-z0-9_]+',run):raise ValueError('invalid session')
    directory=ROOT/'artifacts'/run
    bundles=sorted(directory.glob('bundle_v2*'),key=lambda p:p.stat().st_mtime,reverse=True)
    if not bundles:raise ValueError('this session has not produced a v2 bundle yet')
    custom=ROOT/'artifacts/web_reviews'/run
    return bundles[0],custom if (custom/'policy.json').exists() else directory/'reviews'


def load(path):return json.loads(path.read_text())


def manifest(bundle):return load(bundle/'merkle/merkle_manifest.json')


def allowed_file(bundle,name):
    records=manifest(bundle)['leaves']
    allowed={item['path'] for item in records}|{item['proof_path'] for item in records}|{'merkle/merkle_manifest.json'}
    if name not in allowed:raise ValueError('file is not part of the selected evidence bundle')
    path=(bundle/name).resolve()
    if bundle.resolve() not in path.parents or path.is_symlink():raise ValueError('unsafe file path')
    return path


def command(args,timeout=120):
    result=subprocess.run([str(ROOT/'bin/project')]+args,cwd=ROOT,capture_output=True,text=True,timeout=timeout)
    return {'ok':result.returncode==0,'output':(result.stdout+result.stderr).strip()}


def overview(run):
    bundle,reviews=context(run)
    meta=manifest(bundle);summary=load(bundle/'summary.json')
    rows=list(csv.DictReader(io.StringIO((bundle/'ensemble_returns.csv').read_text())))
    status={'state':'awaiting_review','identity_scope':'No reviewer statements supplied for this session.'}
    if all((reviews/n).exists() for n in ('policy.json','publisher.json','reviewer.json')):
        status=verify_reviews(bundle,reviews/'policy.json',[load(reviews/'publisher.json'),load(reviews/'reviewer.json')])
    events=[json.loads(line) for line in (bundle/'agent_events.jsonl').read_text().splitlines() if line.strip()]
    return {'run':run,'root':meta['root'],'leaf_count':meta['leaf_count'],'summary':summary,
            'metrics':load(bundle/'development_metrics.json')['metrics']['ensemble'],
            'candidates':load(bundle/'fold_metrics.json')['candidates'],
            'equity':[{'date':r['date'],'equity':float(r['equity'])} for r in rows],
            'quality':load(bundle/'market_data/quality.json'),'cross':load(bundle/'market_data/cross_reference.json'),
            'faults':load(bundle/'fault_campaign.json'),'reviews':status,'events':events,
            'workflow':load(bundle/'workflow.json'),'files':meta['leaves'],
            'hypothesis':load(bundle/'hypothesis.json') if (bundle/'hypothesis.json').exists() else None,
            'stress':load(bundle/'counterfactual.json') if (bundle/'counterfactual.json').exists() else None,
            'verdict':load(bundle/'hypothesis_verdict.json') if (bundle/'hypothesis_verdict.json').exists() else None}


def sessions():
    values=[{'id':'demo','label':'反证 Agent · 完整演示','has_bundle':True},{'id':'v2','label':'此前 v2 签名演示','has_bundle':True}]
    for path in sorted((ROOT/'artifacts').glob('langgraph_*'),key=lambda p:p.stat().st_mtime,reverse=True):
        if (path/'session.json').exists():
            values.append({'id':path.name,'label':path.name,'has_bundle':bool(list(path.glob('bundle_v2*')))})
    return values


def launch(body,codex):
    with LOCK:
        if any(job['status']=='running' for job in JOBS.values()):raise ValueError('已有 Agent 正在运行，请先查看当前任务。')
        action=body.get('action','start')
        args=[str(ROOT/'.venv-agent/bin/python'),'-m','portfolio_agent.cli','--codex-bin',codex]
        if action=='resume':
            name=body.get('session','')
            if name not in {value['id'] for value in sessions() if value['id'] not in ('demo','v2')}:raise ValueError('unknown session')
            args+=['--resume',str(ROOT/'artifacts'/name)]
        elif action=='start':
            budget=body.get('budget',6)
            if type(budget) is not int or not 2<=budget<=20:raise ValueError('budget must be an integer from 2 to 20')
            goal=body.get('goal','')
            if not isinstance(goal,str) or len(goal)>2000:raise ValueError('invalid goal')
            args+=['--budget',str(budget)]
            if goal.strip():args+=['--goal',goal.strip()]
        else:raise ValueError('invalid action')
        identifier=uuid.uuid4().hex
        directory=ROOT/'artifacts/web_jobs';directory.mkdir(exist_ok=True)
        log=directory/(identifier+'.log')
        job={'id':identifier,'status':'running','session':None,'started_at':time.time(),'log':str(log)}
        JOBS[identifier]=job
        metadata=directory/(identifier+'.json')
        metadata.write_text(json.dumps(job))
        def execute():
            try:
                env={**os.environ,'PYTHONPATH':str(ROOT/'agents')}
                with log.open('w') as stream:
                    process=subprocess.Popen(args,cwd=ROOT,env=env,stdout=stream,stderr=subprocess.STDOUT,text=True)
                    code=process.wait()
                content=log.read_text()
                match=re.search(r'AGENT_COMPLETED .*?/(langgraph_[A-Za-z0-9_]+)',content)
                with LOCK:
                    job.update(status='completed' if code==0 else 'failed',returncode=code,session=match.group(1) if match else None)
                    metadata.write_text(json.dumps(job))
            except Exception as error:
                log.write_text(str(error))
                with LOCK:
                    job.update(status='failed',error=str(error))
                    metadata.write_text(json.dumps(job))
        threading.Thread(target=execute,daemon=True).start()
        return {'id':identifier,'status':'running'}


class Handler(BaseHTTPRequestHandler):
    def send(self,value,status=200):
        content=json.dumps(value,ensure_ascii=False).encode()
        self.send_response(status);self.send_header('Content-Type','application/json; charset=utf-8');self.send_header('Content-Length',str(len(content)));self.send_header('Cache-Control','no-store');self.end_headers();self.wfile.write(content)

    def do_GET(self):
        try:
            parsed=urlsplit(self.path);query=parse_qs(parsed.query);run=query.get('run',['demo'])[0]
            if parsed.path=='/api/health':return self.send({'app':'ProofLab','api_version':'3.0.0','status':'ready'})
            if parsed.path=='/api/sessions':return self.send(sessions())
            if parsed.path=='/api/job':
                identifier=query.get('id',[''])[0]
                with LOCK:job=dict(JOBS.get(identifier,{}))
                if not job:return self.send({'error':'job not found'},404)
                job['lines']=Path(job.pop('log')).read_text().splitlines()[-180:]
                return self.send(job)
            if parsed.path=='/api/dataset':
                path=ROOT/'data/reference/coverage.json'
                return self.send(load(path) if path.exists() else {'status':'not_built','error':'请先执行 make reference-data 获取完整参考来源。'})
            if parsed.path=='/api/overview':return self.send(overview(run))
            if parsed.path in ('/api/file','/download'):
                bundle,_=context(run);name=query.get('path',[''])[0];path=allowed_file(bundle,name)
                if parsed.path=='/download':
                    raw=path.read_bytes();self.send_response(200);self.send_header('Content-Type','application/octet-stream');self.send_header('Content-Disposition','attachment; filename="'+path.name+'"');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw);return
                raw=path.read_text()
                if name.endswith('.csv'):
                    rows=list(csv.DictReader(io.StringIO(raw)));return self.send({'kind':'csv','rows':rows[:150],'total':len(rows),'columns':list(rows[0]) if rows else []})
                if name.endswith('.json'):return self.send({'kind':'json','value':json.loads(raw)})
                if name.endswith('.jsonl'):
                    rows=[json.loads(line) for line in raw.splitlines() if line.strip()];return self.send({'kind':'jsonl','rows':rows[:150],'total':len(rows)})
                return self.send({'kind':'text','value':raw[:200000]})
            assets={'/':'index.html','/index.html':'index.html','/app.js':'app.js','/style.css':'style.css'}
            if parsed.path not in assets:return self.send({'error':'page not found'},404)
            path=ROOT/'web'/assets[parsed.path];raw=path.read_bytes();self.send_response(200);self.send_header('Content-Type',mimetypes.guess_type(str(path))[0]+'; charset=utf-8');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
        except (ValueError,KeyError,FileNotFoundError) as error:self.send({'error':str(error)},400)
        except Exception as error:self.send({'error':str(error)},500)

    def do_POST(self):
        try:
            origin=self.headers.get('Origin')
            if origin and origin not in (f'http://127.0.0.1:{self.server.server_port}',f'http://localhost:{self.server.server_port}'):return self.send({'error':'cross-origin request rejected'},403)
            if self.headers.get('Content-Type','').split(';')[0]!='application/json':return self.send({'error':'JSON body required'},415)
            size=int(self.headers.get('Content-Length','0'))
            if not 0<size<=20000:raise ValueError('invalid body size')
            body=json.loads(self.rfile.read(size));path=urlsplit(self.path).path
            if not isinstance(body,dict):raise ValueError('JSON object required')
            if path=='/api/runs':return self.send(launch(body,self.server.codex))
            bundle,reviews=context(body.get('run','demo'))
            if path=='/api/import-reviews':
                if body.get('trusted_policy') is not True:raise ValueError('请先确认已通过可信渠道核对公钥身份。')
                policy=body.get('policy');publisher=body.get('publisher');reviewer=body.get('reviewer')
                if not all(isinstance(value,dict) for value in (policy,publisher,reviewer)):raise ValueError('需要三个公开 JSON 文件；不要上传私钥。')
                with tempfile.TemporaryDirectory() as temporary:
                    policy_path=Path(temporary)/'policy.json';policy_path.write_text(json.dumps(policy))
                    result=verify_reviews(bundle,policy_path,[publisher,reviewer])
                destination=ROOT/'artifacts/web_reviews'/body.get('run','demo');destination.mkdir(parents=True,exist_ok=True)
                for name,value in (('policy.json',policy),('publisher.json',publisher),('reviewer.json',reviewer)):
                    (destination/name).write_text(json.dumps(value,sort_keys=True,indent=2)+'\n')
                return self.send({'ok':result['state']=='approved','result':result})
            if path=='/api/verify-inventory':return self.send({'ok':True,'result':verify_inventory(ROOT/'data/reference')})
            if path=='/api/stress':
                result=command(['experiment','counterfactual','--root',str(ROOT),'--bundle',str(bundle)])
                if not result['ok']:return self.send(result)
                return self.send({'ok':True,'result':json.loads(result['output'])})
            if path=='/api/verify':
                leaf=next((value for value in manifest(bundle)['leaves'] if value['path']==body.get('path')),None)
                if not leaf:raise ValueError('select a manifest leaf')
                if body.get('tamper'):
                    with tempfile.TemporaryDirectory() as temporary:
                        directory=Path(temporary)
                        for name in (leaf['path'],'merkle/merkle_manifest.json',leaf['proof_path']):
                            target=directory/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes((bundle/name).read_bytes())
                        target=directory/leaf['path'];content=bytearray(target.read_bytes())
                        if content:content[0]^=1
                        else:content.extend(b'x')
                        target.write_bytes(content)
                        result=command(['evidence','verify-proof','--bundle',str(directory),'--proof',str(directory/leaf['proof_path'])]);result['expected_rejection']=not result['ok']
                        return self.send(result)
                return self.send(command(['evidence','verify-proof','--bundle',str(bundle),'--proof',str(bundle/leaf['proof_path'])]))
            if path=='/api/verify-bundle':return self.send(command(['evidence','verify-v2','--bundle',str(bundle)]))
            if path=='/api/replay':return self.send(command(['experiment','agent-replay','--root',str(ROOT),'--bundle',str(bundle)]))
            if path=='/api/verify-market':return self.send({'ok':True,'result':verify_market_evidence(bundle,ROOT)})
            if path=='/api/verify-review':
                if not all((reviews/name).exists() for name in ('policy.json','publisher.json','reviewer.json')):raise ValueError('尚无完整签名。请按审核说明由各方独立签署。')
                result=verify_reviews(bundle,reviews/'policy.json',[load(reviews/'publisher.json'),load(reviews/'reviewer.json')]);return self.send({'ok':result['state']=='approved','result':result})
            if path=='/api/faults':
                target=ROOT/'artifacts/web_fault_campaign.json';result=fault_campaign(ROOT,target);return self.send({'ok':result['status']=='passed','result':result})
            self.send({'error':'unknown operation'},404)
        except (ValueError,KeyError,FileNotFoundError) as error:self.send({'error':str(error)},400)
        except Exception as error:self.send({'error':str(error)},500)


def ensure_primary():
    ensure_dataset(ROOT)


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--port',type=int,default=8765);parser.add_argument('--codex-bin',default=os.environ.get('CODEX_BIN','codex'));parser.add_argument('--open',action='store_true');args=parser.parse_args()
    ensure_primary()
    for path in (ROOT/'artifacts/web_jobs').glob('*.json'):
        try:
            job=load(path)
            if job['status']=='running':job.update(status='interrupted',error='Server restarted. Resume the saved Agent session from checkpoint.')
            JOBS[job['id']]=job
        except (ValueError,KeyError):pass
    server=ThreadingHTTPServer(('127.0.0.1',args.port),Handler);server.codex=args.codex_bin
    print(f'Portfolio workbench: http://127.0.0.1:{args.port}',flush=True)
    if args.open:
        import webbrowser
        webbrowser.open(f'http://127.0.0.1:{args.port}')
    try:server.serve_forever()
    except KeyboardInterrupt:server.server_close()


if __name__=='__main__':main()
