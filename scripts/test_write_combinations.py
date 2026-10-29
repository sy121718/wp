"""写组合门禁：仅追踪当前表单实际引用的行模板。"""
import pathlib
import subprocess
import tempfile
import unittest

SCANNER = pathlib.Path(__file__).with_name('write-combinations-scan.py').resolve()

class WriteCombinationTests(unittest.TestCase):
    def scan(self, form, fragments):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / 'form.html').write_text(form, encoding='utf-8')
            for name, source in fragments.items():
                (root / name).write_text(source, encoding='utf-8')
            return subprocess.run(['python3', str(SCANNER), str(root / 'form.html')], text=True, capture_output=True)

    def test_nested_include_supplies_row_template(self):
        result = self.scan('<form data-write-json="values">{{include "a.html"}}</form>', {'a.html':'{{include "rows.html"}}', 'rows.html':'<template name="values" data-row></template>'})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unrelated_or_commented_template_cannot_hide_missing_rows(self):
        for form in ['<form data-write-json="values"></form>{{include "rows.html"}}', '<form data-write-json="values">{* {{include "rows.html"}} *}</form>']:
            result = self.scan(form, {'rows.html':'<template name="values" data-row></template>'})
            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
            self.assertIn('values', result.stdout)

    def test_wrong_name_is_rejected(self):
        result = self.scan('<form data-write-json="values">{{include "rows.html"}}</form>', {'rows.html':'<template name="other" data-row></template>'})
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)

    def test_row_attributes_can_be_in_either_order(self):
        result = self.scan('<form data-write-json="values"><template data-row name="values"></template></form>', {})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_include_cycle_does_not_invent_a_row(self):
        result = self.scan('<form data-write-json="values">{{include "rows.html"}}</form>', {'rows.html':'{{include "form.html"}}'})
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)

if __name__ == '__main__':
    unittest.main()
