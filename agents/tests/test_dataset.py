from pathlib import Path
import json,shutil,tempfile,unittest
from portfolio_agent.dataset import verify_inventory

ROOT=Path(__file__).resolve().parents[2]


class DatasetTests(unittest.TestCase):
    def test_actual_full_history_and_explicit_reference_gap(self):
        result=verify_inventory(ROOT/'data/reference')
        self.assertEqual(result['primary_rows'],5478)
        self.assertEqual(result['reference_rows'],5311)
        report=json.loads((ROOT/'data/reference/coverage.json').read_text())
        self.assertTrue(all(row['primary_complete'] for row in report['coverage']))
        sol=next(row for row in report['coverage'] if row['symbol']=='SOLUSDT')
        self.assertEqual(len(sol['reference_missing_days']),167)

    def test_tampered_raw_source_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory=Path(temporary)/'reference';shutil.copytree(ROOT/'data/reference',directory)
            report=json.loads((directory/'coverage.json').read_text())
            source=directory/Path(report['sources'][0]['file']).name
            source.write_bytes(source.read_bytes()+b' ')
            with self.assertRaisesRegex(ValueError,'raw source hash differs'):verify_inventory(directory)
