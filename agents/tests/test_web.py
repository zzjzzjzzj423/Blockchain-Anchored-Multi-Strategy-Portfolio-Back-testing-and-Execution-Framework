import json,threading,unittest,urllib.request,urllib.error
from portfolio_agent.web import Handler,ThreadingHTTPServer


class WebTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server=ThreadingHTTPServer(('127.0.0.1',0),Handler);cls.server.codex='codex'
        cls.thread=threading.Thread(target=cls.server.serve_forever,daemon=True);cls.thread.start()
        cls.base='http://127.0.0.1:'+str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):cls.server.shutdown();cls.server.server_close()

    def get(self,path):
        with urllib.request.urlopen(self.base+path) as response:return response.read()

    def post(self,path,body,origin=None):
        headers={'Content-Type':'application/json'}
        if origin:headers['Origin']=origin
        request=urllib.request.Request(self.base+path,data=json.dumps(body).encode(),headers=headers)
        with urllib.request.urlopen(request) as response:return json.load(response)

    def test_pages_and_bundle_leaf_viewers(self):
        for path in ('/','/app.js','/style.css'):self.assertGreater(len(self.get(path)),100)
        overview=json.loads(self.get('/api/overview'))
        self.assertEqual(overview['leaf_count'],len(overview['files']))
        self.assertGreater(overview['leaf_count'],30)
        for name in ('ensemble_trades.csv','agent_events.jsonl','market_data/quality.json'):
            response=json.loads(self.get('/api/file?path='+name))
            self.assertIn(response['kind'],('csv','jsonl','json'))

    def test_real_proof_verification_and_mutation_rejection(self):
        self.assertTrue(self.post('/api/verify',{'path':'ensemble_trades.csv'})['ok'])
        self.assertTrue(self.post('/api/verify',{'path':'ensemble_trades.csv','tamper':True})['expected_rejection'])
        self.assertTrue(self.post('/api/verify-bundle',{})['ok'])

    def test_private_path_and_cross_origin_rejected(self):
        for path in ('/api/file?path=../../.env','/.env','/agents/portfolio_agent/web.py'):
            with self.assertRaises(urllib.error.HTTPError):self.get(path)
        with self.assertRaises(urllib.error.HTTPError) as error:self.post('/api/faults',{},'https://untrusted.example')
        self.assertEqual(error.exception.code,403)

    def test_counterfactual_is_measured_and_sensitive_to_cost(self):
        result=self.post('/api/stress',{})
        self.assertTrue(result['ok'])
        scenarios={row['scenario']:row for row in result['result']['scenarios']}
        self.assertEqual(len(scenarios),5)
        self.assertNotEqual(scenarios['baseline']['median_validation_sharpe'],scenarios['double_commission']['median_validation_sharpe'])
        self.assertFalse(result['result']['holdout_opened'])
