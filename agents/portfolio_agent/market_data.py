"""Fail-closed data checks and independently sourced price corroboration."""
import csv
from datetime import date,datetime,timedelta,timezone
import hashlib
import json
import math
from pathlib import Path
import urllib.request

SYMBOLS=('BTCUSDT','ETHUSDT','SOLUSDT')
START=date(2021,1,1)
END=date(2024,12,31)
CROSS_START=date(2024,12,1)
CROSS_END=date(2024,12,7)


def digest(path):return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def check_rows(rows,start=START,end=END,symbols=SYMBOLS):
    issues=[];seen=set();previous=None
    for number,row in enumerate(rows,2):
        try:
            day=date.fromisoformat(row['date'])
            if not start<=day<=end:continue
            key=(day,row['symbol'])
            if row['symbol'] not in symbols:raise ValueError('unknown symbol')
            if key in seen:raise ValueError('duplicate date/symbol')
            if previous is not None and key<previous:raise ValueError('out-of-order timestamp/symbol')
            values={key:float(row[key]) for key in ('open','high','low','close','volume')}
            if not all(math.isfinite(v) for v in values.values()):raise ValueError('non-finite price or volume')
            if min(values[k] for k in ('open','high','low','close'))<=0 or values['volume']<0:raise ValueError('nonpositive price or negative volume')
            if values['low']>min(values['open'],values['close']) or values['high']<max(values['open'],values['close']) or values['high']<values['low']:raise ValueError('invalid OHLC range')
            seen.add(key);previous=key
        except (ValueError,KeyError,TypeError) as error:issues.append({'row':number,'reason':str(error)})
    expected={(start+timedelta(days=i),symbol) for i in range((end-start).days+1) for symbol in symbols}
    missing=sorted(expected-seen)
    if missing:issues.append({'reason':'missing or stale development bars','count':len(missing),'first':str(missing[0])})
    return {'status':'passed' if not issues else 'blocked','checked_rows':len(seen),'expected_rows':len(expected),'issues':issues[:100],'issue_count':len(issues),'interval':[str(start),str(end)]}


def quality(root,output):
    root=Path(root);output=Path(output);output.mkdir(parents=True,exist_ok=True)
    path=root/'data/normalized/daily_ohlcv.csv'
    with path.open() as stream:report=check_rows(list(csv.DictReader(stream)))
    report['normalized_sha256']=digest(path)
    policy=json.loads((root/'configs/v2_policy.json').read_text())
    report['config_issues']=[]
    if report['normalized_sha256']!=policy['normalized_sha256']:report['config_issues'].append('data/normalized/daily_ohlcv.csv: frozen snapshot hash differs')
    for name,expected in policy['frozen_config_sha256'].items():
        if digest(root/name)!=expected:report['config_issues'].append(name)
    if report['config_issues']:report['status']='blocked'
    (output/'quality.json').write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
    return report


def fetch_coinbase(product):
    url=f'https://api.exchange.coinbase.com/products/{product}/candles?granularity=86400&start=2024-12-01T00%3A00%3A00Z&end=2024-12-08T00%3A00%3A00Z'
    request=urllib.request.Request(url,headers={'User-Agent':'MSBD5017-PortfolioProof/2.0'})
    with urllib.request.urlopen(request,timeout=30) as response:
        if not response.url.startswith('https://api.exchange.coinbase.com/'):raise ValueError('unexpected source redirect')
        return url,response.read()


def cross_reference(normalized,output,fetcher=fetch_coinbase,tolerance_bps=200):
    output=Path(output);output.mkdir(parents=True,exist_ok=True)
    primary={}
    with Path(normalized).open() as stream:
        for row in csv.DictReader(stream):
            if str(CROSS_START)<=row['date']<=str(CROSS_END):primary[(row['date'],row['symbol'])]=float(row['close'])
    rows=[];sources=[];errors=[]
    for symbol in SYMBOLS:
        product=symbol[:-4]+'-USD'
        try:
            url,raw=fetcher(product)
            (output/(product+'.json')).write_bytes(raw)
            candles=json.loads(raw)
            if not isinstance(candles,list):raise ValueError('feed response is not a candle array')
            secondary={}
            for candle in candles:
                if len(candle)<5:raise ValueError('truncated candle')
                stamp=int(candle[0])
                if stamp%86400:raise ValueError('misaligned daily candle')
                day=datetime.fromtimestamp(stamp,timezone.utc).date()
                if not CROSS_START<=day<=CROSS_END:continue
                if str(day) in secondary:raise ValueError('duplicate candle')
                close=float(candle[4])
                low,high,opened=map(float,(candle[1],candle[2],candle[3]))
                if not all(math.isfinite(v) and v>0 for v in (close,low,high,opened)) or low>min(opened,close) or high<max(opened,close):raise ValueError('invalid external OHLC')
                secondary[str(day)]=close
            for i in range(7):
                day=str(CROSS_START+timedelta(days=i))
                if (day,symbol) not in primary or day not in secondary:raise ValueError('incomplete cross-source coverage')
                price=primary[(day,symbol)];other=secondary[day]
                deviation=abs(price-other)/price*10000
                rows.append({'date':day,'symbol':symbol,'primary_close':price,'secondary_close':other,'deviation_bps':round(deviation,6),'within_tolerance':deviation<=tolerance_bps})
            sources.append({'source_id':'coinbase_exchange','product':product,'url':url,'raw_file':product+'.json','raw_sha256':hashlib.sha256(raw).hexdigest()})
        except Exception as error:errors.append({'product':product,'reason':str(error)})
    primary_sample=output/'binance_sample.json'
    primary_sample.write_text(json.dumps([{'date':day,'symbol':symbol,'close':value} for (day,symbol),value in sorted(primary.items())],sort_keys=True)+'\n')
    report={'schema_version':'2.0.0','status':'unavailable' if errors else 'corroborated' if len(rows)==21 and all(r['within_tolerance'] for r in rows) else 'disputed','primary_source':'binance_vision','primary_sample_sha256':digest(primary_sample),'primary_normalized_sha256':digest(normalized),'secondary_sources':sources,'comparison_interval':[str(CROSS_START),str(CROSS_END)],'compared_rows':len(rows),'tolerance_bps':tolerance_bps,'quote_basis':'Binance USDT versus Coinbase USD; tolerance includes venue and quote differences','rows':rows,'errors':errors,'truth_claim':'Cross-source consistency for 21 historical daily closes only; hashes commit to snapshots, not market truth or source independence guarantees.'}
    (output/'cross_reference.json').write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
    return report


def verify_market_evidence(bundle,root):
    """Recompute cross-source comparison from committed snapshots without network."""
    import tempfile
    bundle=Path(bundle);root=Path(root)
    stored=json.loads((bundle/'market_data/cross_reference.json').read_text())
    checked=json.loads((bundle/'market_data/quality.json').read_text())
    policy=json.loads((root/'configs/v2_policy.json').read_text())
    if json.loads((bundle/'market_data/policy.json').read_text())!=policy or stored['tolerance_bps']!=policy['cross_source_tolerance_bps']:raise ValueError('cross-source policy differs from trusted local policy')
    if digest(bundle/'market_data/binance_sample.json')!=stored['primary_sample_sha256']:raise ValueError('primary sample hash differs')
    if checked['status']!='passed' or checked['normalized_sha256']!=digest(root/'data/normalized/daily_ohlcv.csv'):
        raise ValueError('reviewer local market snapshot differs from committed data identity')
    def snapshots(product):
        source=next(item for item in stored['secondary_sources'] if item['product']==product)
        raw=(bundle/'market_data'/(product+'.json')).read_bytes()
        if hashlib.sha256(raw).hexdigest()!=source['raw_sha256']:raise ValueError('source snapshot hash mismatch')
        expected=f'https://api.exchange.coinbase.com/products/{product}/candles?'
        if not source['url'].startswith(expected) or source['source_id']!='coinbase_exchange':raise ValueError('source provenance is not allowed')
        return source['url'],raw
    with tempfile.TemporaryDirectory() as temporary:
        rebuilt=cross_reference(root/'data/normalized/daily_ohlcv.csv',Path(temporary),snapshots,stored['tolerance_bps'])
        fresh=quality(root,Path(temporary))
    if rebuilt!=stored or rebuilt['status']!='corroborated' or fresh!=checked:raise ValueError('recomputed market evidence differs or is not corroborated')
    return {'status':'verified','rows':21,'network_calls':0}


def fault_campaign(root,output):
    """Mutate copies of real development observations; never modify live inputs."""
    import copy
    import tempfile
    with (Path(root)/'data/normalized/daily_ohlcv.csv').open() as stream:
        base=[row for row in csv.DictReader(stream) if str(START)<=row['date']<=str(END)]
    mutations={
        'nonfinite_price':lambda rows:rows[0].update(close='NaN'),
        'negative_volume':lambda rows:rows[0].update(volume='-1'),
        'invalid_ohlc':lambda rows:rows[0].update(high='1'),
        'zero_price':lambda rows:rows[0].update(close='0'),
        'invalid_timestamp':lambda rows:rows[0].update(date='invalid'),
        'unknown_asset':lambda rows:rows[0].update(symbol='UNKNOWN'),
        'missing_bar':lambda rows:rows.pop(3),
        'duplicate_bar':lambda rows:rows.insert(0,copy.deepcopy(rows[0])),
        'stale_feed_tail':lambda rows:rows.pop(),
        'out_of_order':lambda rows:rows.reverse(),
    }
    cases=[]
    for name,mutate in mutations.items():
        rows=copy.deepcopy(base);mutate(rows);result=check_rows(rows)
        cases.append({'fault':name,'detected':result['status']=='blocked','issue_count':result['issue_count']})
    frozen=json.loads((Path(root)/'configs/v2_policy.json').read_text())
    for name in ('configs/costs.json','configs/strategies.json'):
        content=(Path(root)/name).read_bytes()+b' '
        cases.append({'fault':'parameter_or_cost_config_drift:'+name,'detected':hashlib.sha256(content).hexdigest()!=frozen['frozen_config_sha256'][name]})
    report={'schema_version':'2.0.0','status':'passed' if all(c['detected'] for c in cases) else 'failed','scope':'isolated mutations of real development observations and frozen config bytes','cases':cases,'detected':sum(c['detected'] for c in cases),'total':len(cases)}
    Path(output).write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
    return report
