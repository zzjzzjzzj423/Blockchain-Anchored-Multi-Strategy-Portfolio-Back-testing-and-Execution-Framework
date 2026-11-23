import copy
from datetime import date,timedelta,datetime,timezone
import json
from pathlib import Path
import tempfile
import unittest
from portfolio_agent.market_data import check_rows,cross_reference,SYMBOLS,CROSS_START
from portfolio_agent.review import keygen,attest,verify_reviews


class MarketDefectTests(unittest.TestCase):
    def rows(self):
        return [{'date':str(date(2024,12,1)+timedelta(days=day)),'symbol':symbol,'open':'100','high':'110','low':'90','close':'100','volume':'1'} for day in range(2) for symbol in SYMBOLS]

    def test_corrupt_missing_duplicate_nonfinite_stale_and_order(self):
        original=self.rows()
        self.assertEqual(check_rows(original,date(2024,12,1),date(2024,12,2))['status'],'passed')
        mutations=[]
        for key,value in [('close','NaN'),('high','80'),('volume','-1'),('open','0'),('date','not-a-date'),('symbol','UNKNOWN')]:
            rows=copy.deepcopy(original);rows[0][key]=value;mutations.append(rows)
        mutations.extend([original[:-1],original+[original[-1]],list(reversed(original)),original[:3]])
        for rows in mutations:
            with self.subTest(rows=rows[:1]):self.assertEqual(check_rows(rows,date(2024,12,1),date(2024,12,2))['status'],'blocked')

    def test_cross_source_dispute_unavailable_and_missing_coverage(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);normalized=root/'feed.csv'
            normalized.write_text('date,symbol,close\n'+''.join(f'{CROSS_START+timedelta(days=i)},{symbol},100\n' for i in range(7) for symbol in SYMBOLS))
            def feed(close=100,missing=False):
                return lambda product:('https://api.exchange.coinbase.com/products/'+product+'/candles?',json.dumps([[int(datetime.combine(CROSS_START+timedelta(days=i),datetime.min.time(),tzinfo=timezone.utc).timestamp()),90,150,100,close,1] for i in range(6 if missing else 7)]).encode())
            self.assertEqual(cross_reference(normalized,root/'ok',feed())['status'],'corroborated')
            self.assertEqual(cross_reference(normalized,root/'dispute',feed(110))['status'],'disputed')
            self.assertEqual(cross_reference(normalized,root/'missing',feed(missing=True))['status'],'unavailable')
            def unavailable(product):raise TimeoutError('feed unavailable')
            self.assertEqual(cross_reference(normalized,root/'offline',unavailable)['status'],'unavailable')


class MultipartyTests(unittest.TestCase):
    def test_roles_signatures_root_binding_and_rejection(self):
        with tempfile.TemporaryDirectory() as directory:
            p=Path(directory);(p/'merkle').mkdir()
            (p/'merkle/merkle_manifest.json').write_text(json.dumps({'root':'a'*64}))
            publisher=keygen(p/'publisher.pem');reviewer=keygen(p/'reviewer.pem')
            policy=p/'policy.json';policy.write_text(json.dumps({'roles':{'publisher':[publisher],'reviewer':[reviewer]}}))
            pub=attest(p,policy,p/'publisher.pem','publisher','approve','test')
            rev=attest(p,policy,p/'reviewer.pem','reviewer','approve','test')
            self.assertEqual(verify_reviews(p,policy,[pub])['state'],'awaiting_review')
            self.assertEqual(verify_reviews(p,policy,[pub,rev])['state'],'approved')
            rejection=attest(p,policy,p/'reviewer.pem','reviewer','reject','risk unresolved')
            self.assertEqual(verify_reviews(p,policy,[pub,rejection])['state'],'rejected')
            with self.assertRaises(ValueError):verify_reviews(p,policy,[pub,pub])
            with self.assertRaises(ValueError):attest(p,policy,p/'publisher.pem','reviewer','approve','self-review')
            modified=copy.deepcopy(rev);modified['statement']['comment']='tampered'
            with self.assertRaises(Exception):verify_reviews(p,policy,[pub,modified])
            (p/'merkle/merkle_manifest.json').write_text(json.dumps({'root':'b'*64}))
            with self.assertRaises(ValueError):verify_reviews(p,policy,[pub,rev])


if __name__=='__main__':unittest.main()

class FrozenInputTests(unittest.TestCase):
    def test_plausible_price_change_and_parameter_drift_are_blocked(self):
        import hashlib
        from portfolio_agent.market_data import quality
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'data/normalized').mkdir(parents=True);(root/'configs').mkdir()
            feed=root/'data/normalized/daily_ohlcv.csv'
            feed.write_text('date,symbol,open,high,low,close,volume\n2024-12-01,BTCUSDT,100,110,90,100,1\n')
            config=root/'configs/costs.json';config.write_text('{"fee":0.001}')
            policy={'normalized_sha256':hashlib.sha256(feed.read_bytes()).hexdigest(),'frozen_config_sha256':{'configs/costs.json':hashlib.sha256(config.read_bytes()).hexdigest()}}
            (root/'configs/v2_policy.json').write_text(json.dumps(policy))
            config.write_text('{"fee":0}')
            result=quality(root,root/'report1')
            self.assertIn('configs/costs.json',result['config_issues'])
            feed.write_text(feed.read_text().replace(',100,1\n',',101,1\n'))
            result=quality(root,root/'report2')
            self.assertTrue(any('snapshot hash differs' in value for value in result['config_issues']))
