"""Complete historical data inventory and full-window independent reference feeds."""
import csv,hashlib,json,time,urllib.request,math,shutil,io
from concurrent.futures import ThreadPoolExecutor
from datetime import date,datetime,timedelta,timezone
from pathlib import Path
START=date(2021,1,1);END=date(2025,12,31)
SYMBOLS=('BTCUSDT','ETHUSDT','SOLUSDT')


def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()


def ensure_primary(root):
    root=Path(root);target=root/'data/normalized/daily_ohlcv.csv'
    if not target.exists():
        source=root/'data/reference/binance_daily.csv'
        expected=json.loads((root/'configs/v2_policy.json').read_text())['normalized_sha256']
        if not source.exists() or sha(source)!=expected:raise ValueError('distributed primary snapshot is missing or differs from frozen identity')
        target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source,target)


def build(root):
    root=Path(root);ensure_primary(root);out=root/'data/reference';out.mkdir(exist_ok=True)
    tasks=[]
    for symbol in SYMBOLS:
        cursor=START
        while cursor<=END:
            stop=min(cursor+timedelta(days=279),END)
            tasks.append((symbol,cursor,stop));cursor=stop+timedelta(days=1)
    def fetch(task):
        symbol,start,end=task;product=symbol[:-4]+'-USD'
        url=f'https://api.exchange.coinbase.com/products/{product}/candles?granularity=86400&start={start}T00%3A00%3A00Z&end={end+timedelta(days=1)}T00%3A00%3A00Z'
        path=out/f'{product}_{start}_{end}.json'
        error=None
        for attempt in range(3):
            try:
                if path.exists():raw=path.read_bytes()
                else:
                    request=urllib.request.Request(url,headers={'User-Agent':'PortfolioProof/3.0'})
                    with urllib.request.urlopen(request,timeout=30) as response:raw=response.read()
                    parsed=json.loads(raw)
                    if not isinstance(parsed,list):raise ValueError('invalid source response')
                    path.write_bytes(raw)
                candles=json.loads(raw);records=[];seen=set()
                for candle in candles:
                    stamp=int(candle[0]);day=datetime.fromtimestamp(stamp,timezone.utc).date()
                    if not start<=day<=end:continue
                    if stamp%86400 or day in seen:raise ValueError('misaligned or duplicate candle')
                    seen.add(day)
                    values=[float(candle[i]) for i in (1,2,3,4,5)]
                    low,high,opened,close,volume=values
                    if not all(math.isfinite(v) for v in values) or min(values[:4])<=0 or volume<0 or low>min(opened,close) or high<max(opened,close):raise ValueError('invalid source OHLCV')
                    records.append({'date':str(day),'symbol':symbol,'open':candle[3],'high':candle[2],'low':candle[1],'close':candle[4],'volume':candle[5]})
                return {'source':'Coinbase Exchange','symbol':symbol,'product':product,'url':url,'sha256':sha(path),'file':str(path.relative_to(root)),'rows':len(records)},records
            except Exception as exc:error=str(exc);time.sleep(attempt+1)
        return {'symbol':symbol,'error':error,'url':url},[]
    with ThreadPoolExecutor(max_workers=3) as pool:results=list(pool.map(fetch,tasks))
    rows=sorted([row for source,values in results for row in values],key=lambda row:(row['date'],row['symbol']))
    normalized=out/'coinbase_daily.csv'
    with normalized.open('w') as stream:
        writer=csv.DictWriter(stream,fieldnames=['date','symbol','open','high','low','close','volume']);writer.writeheader();writer.writerows(rows)
    shutil.copy2(root/'data/normalized/daily_ohlcv.csv',out/'binance_daily.csv')
    primary=list(csv.DictReader(io.StringIO((root/'data/normalized/daily_ohlcv.csv').read_text())))
    secondary={(r['date'],r['symbol']):r for r in rows}
    coverage=[];disagreements=[]
    for symbol in SYMBOLS:
        base=[r for r in primary if r['symbol']==symbol]
        present=[r for r in rows if r['symbol']==symbol]
        missing=[r['date'] for r in base if (r['date'],symbol) not in secondary]
        differences=[]
        for row in base:
            other=secondary.get((row['date'],symbol))
            if other:
                delta=abs(float(row['close'])-float(other['close']))/float(row['close'])*10000
                differences.append(delta)
                if delta>200:disagreements.append({'date':row['date'],'symbol':symbol,'deviation_bps':round(delta,6)})
        coverage.append({'symbol':symbol,'primary_rows':len(base),'reference_rows':len(present),'expected_rows':1826,'primary_complete':len(base)==1826,'reference_missing_days':missing,'reference_coverage_pct':round(len(present)/1826*100,3),'max_deviation_bps':round(max(differences),6) if differences else None})
    result={'schema_version':'3.0.0','interval':[str(START),str(END)],'primary_source':'Binance Vision','reference_source':'Coinbase Exchange','primary_rows':len(primary),'reference_rows':len(rows),'primary_sha256':sha(root/'data/normalized/daily_ohlcv.csv'),'reference_sha256':sha(normalized),'coverage':coverage,'sources':[source for source,values in results],'source_errors':[source for source,values in results if 'error' in source],'disagreements':disagreements,'quote_basis':'USDT versus USD; 200 bps comparison threshold','missing_data_policy':'No fabrication or forward fill; missing reference periods are explicitly recorded. Full primary coverage does not imply full secondary coverage.','live_data':False}
    (out/'coverage.json').write_text(json.dumps(result,sort_keys=True,indent=2)+'\n')
    print(json.dumps({'primary_rows':len(primary),'reference_rows':len(rows),'source_errors':len(result['source_errors']),'coverage':coverage},ensure_ascii=False),flush=True)
    return result


if __name__=='__main__':build(Path(__file__).resolve().parents[2])


def verify_inventory(directory):
    """Verify the distributed full-window inventory against every raw source hash."""
    from urllib.parse import urlsplit,parse_qs
    directory=Path(directory);report=json.loads((directory/'coverage.json').read_text())
    primary=directory/'binance_daily.csv';secondary=directory/'coinbase_daily.csv'
    if sha(primary)!=report['primary_sha256'] or sha(secondary)!=report['reference_sha256']:raise ValueError('normalized inventory hash differs')
    primary_rows=list(csv.DictReader(io.StringIO(primary.read_text())));secondary_rows=list(csv.DictReader(io.StringIO(secondary.read_text())))
    from .market_data import check_rows
    if check_rows(primary_rows,START,END)['status']!='passed':raise ValueError('primary history incomplete or invalid')
    secondary_check=check_rows(secondary_rows,START,END)
    if any(item['reason']!='missing or stale development bars' for item in secondary_check['issues']):raise ValueError('reference feed has invalid records')
    reconstructed={}
    for source in report['sources']:
        if 'error' in source:raise ValueError('source inventory contains unresolved downloads')
        path=directory/Path(source['file']).name
        if sha(path)!=source['sha256']:raise ValueError('raw source hash differs: '+path.name)
        url=urlsplit(source['url'])
        if url.scheme!='https' or url.netloc!='api.exchange.coinbase.com':raise ValueError('untrusted reference host')
        q=parse_qs(url.query);start=date.fromisoformat(q['start'][0][:10]);end=date.fromisoformat(q['end'][0][:10])-timedelta(days=1)
        for candle in json.loads(path.read_text()):
            day=datetime.fromtimestamp(int(candle[0]),timezone.utc).date()
            if not start<=day<=end:continue
            key=(str(day),source['symbol'])
            if key in reconstructed:raise ValueError('duplicate raw reference record')
            reconstructed[key]=tuple(float(candle[i]) for i in (3,2,1,4,5))
    if len(reconstructed)!=len(secondary_rows):raise ValueError('raw/normalized reference row counts differ')
    for row in secondary_rows:
        if reconstructed.get((row['date'],row['symbol']))!=tuple(float(row[k]) for k in ('open','high','low','close','volume')):raise ValueError('reference normalized values differ from raw response')
    for coverage in report['coverage']:
        actual=[r for r in secondary_rows if r['symbol']==coverage['symbol']]
        missing=[r['date'] for r in primary_rows if r['symbol']==coverage['symbol'] and (r['date'],r['symbol']) not in reconstructed]
        if len(actual)!=coverage['reference_rows'] or missing!=coverage['reference_missing_days']:raise ValueError('reported reference coverage differs')
    return {'status':'verified','primary_rows':len(primary_rows),'reference_rows':len(secondary_rows),'raw_responses':len(report['sources']),'network_calls':0}
