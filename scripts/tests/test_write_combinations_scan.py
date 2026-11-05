#!/usr/bin/env python3
"""写请求组合必须对应真实 HTML 基座，不维护退役 bulk/JSON 数组判据。"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCANNER = Path(__file__).resolve().parents[1] / 'write-combinations-scan.py'


class CombinationTest(unittest.TestCase):
    def scan(self, source, included=''):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'rows.html').write_text(included, encoding='utf-8')
            page = root / 'page.html'
            page.write_text(source, encoding='utf-8')
            return subprocess.run([sys.executable, str(SCANNER), str(page)], text=True, capture_output=True)

    def test_marker_value_does_not_select_array_shape(self):
        for marker in ('data-write', 'data-write="bulk"', 'data-write="save"'):
            result = self.scan('<form ' + marker + ' hx-post="/save"><input name="ids"></form>')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_navigation_is_jet_link_selector_not_url_template(self):
        for selector, expected in (('[data-next]', 0), ('a.next[href]', 0), ('/list', 1), ('/edit/{id}', 1)):
            result = self.scan('<form data-write hx-post="/save" data-write-nav="' + selector + '"></form>')
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)

    def test_each_action_is_checked_and_includes_are_scanned(self):
        result = self.scan('<form data-write-nav="[data-next]">{{include "rows.html"}}</form>',
                           '<button type="button" hx-post="/remove" data-write-nav="/edit/{id}">删除</button>')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('节点选择器', result.stdout)

    def test_json_and_multipart_cannot_be_combined(self):
        for encoding in ('hx-encoding', 'enctype'):
            result = self.scan('<form data-write-builder="items" ' + encoding + '="multipart/form-data"></form>')
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        result = self.scan('<form hx-encoding="multipart/form-data"><button type="button" hx-post="/save" data-write-builder="items">保存</button></form>')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_external_action_does_not_inherit_neighbor_form_encoding(self):
        result = self.scan('<form hx-encoding="multipart/form-data"><button form="json" type="button" hx-post="/save" data-write-builder="items">保存</button></form><form id="json"></form>')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_comments_and_quoted_angles_do_not_create_false_actions(self):
        result = self.scan('<!-- <form data-write-nav="/wrong"> -->{* <form data-write-nav="/wrong"> *}<form title="x > y" data-write-nav="[data-next]"></form>')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
