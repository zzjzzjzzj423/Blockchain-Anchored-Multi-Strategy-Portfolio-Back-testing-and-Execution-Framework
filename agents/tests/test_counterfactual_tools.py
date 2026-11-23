from pathlib import Path
import json,shutil,tempfile,unittest
from portfolio_agent.tools import PortfolioContext,portfolio_tools
ROOT=Path(__file__).resolve().parents[2]


class HypothesisToolsTests(unittest.TestCase):
    def test_hypothesis_precedes_real_stress_and_requires_fresh_replay(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory=Path(temporary);study=directory/'study';study.mkdir()
            for name in ('selected_parameters.json','fold_metrics.json'):
                shutil.copy2(ROOT/'docs/v2_demo/bundle'/name,study/name)
            context=PortfolioContext(ROOT,directory,'codex','gpt-6.1-sol',120)
            context.state={'facts':{'research':'study','replayed':True,'report':True}}
            tools=portfolio_tools(context)
            with self.assertRaisesRegex(ValueError,'propose_hypothesis'):
                tools['stress_test_strategy'].invoke({})
            registration=tools['propose_hypothesis'].invoke({'concern':'cost_fragility','statement':'Doubling costs will reduce validation Sharpe by more than the fixed 0.3 threshold.'})
            context.state['facts']=registration['facts']
            result=tools['stress_test_strategy'].invoke({})
            verdict=result['observation']['hypothesis_verdict']
            self.assertEqual(verdict['result'],'risk_supported')
            self.assertGreater(verdict['max_sharpe_drop'],0.3)
            self.assertFalse(result['facts']['replayed'])
            self.assertFalse(result['facts']['report'])
            self.assertTrue((study/'hypothesis.json').exists())
            self.assertTrue((study/'counterfactual.json').exists())
