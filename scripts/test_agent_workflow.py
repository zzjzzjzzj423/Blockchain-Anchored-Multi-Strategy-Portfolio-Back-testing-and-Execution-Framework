import tempfile
import unittest
from pathlib import Path
from agent_workflow import seal, verify


class EvidenceTests(unittest.TestCase):
    def test_changed_added_and_missing_files_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            leaf = root / 'ledger.csv'
            leaf.write_text('equity\n100\n')
            seal(root)
            self.assertEqual(verify(root), 1)
            leaf.write_text('equity\n101\n')
            with self.assertRaises(ValueError): verify(root)
            leaf.write_text('equity\n100\n')
            extra = root / 'extra.txt'
            extra.write_text('unexpected')
            with self.assertRaises(ValueError): verify(root)
            extra.unlink()
            leaf.unlink()
            with self.assertRaises(ValueError): verify(root)


if __name__ == '__main__': unittest.main()
