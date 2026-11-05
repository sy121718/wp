#!/usr/bin/env python3
"""驱动真实 HTML 写属性门禁；夹具不复制扫描器判据。"""

import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GATE = ROOT / 'scripts/check-write-attributes.sh'
FORM = '<form data-write hx-post="/admin/x" hx-target="#result" hx-swap="innerHTML"'


def judge(kind, tag):
    return subprocess.run(['bash', str(GATE), 'judge'], input=kind + '\t' + tag + '\n',
                          text=True, capture_output=True, cwd=ROOT)


class AttributeTest(unittest.TestCase):
    def assert_ok(self, kind, tag):
        result = judge(kind, tag)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def assert_bad(self, kind, tag, code):
        result = judge(kind, tag)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(code + '\t', result.stdout, result.stdout + result.stderr)

    def test_html_form_needs_no_json_extension(self):
        self.assert_ok('form', FORM + '>')
        self.assert_ok('form', FORM + ' hx-confirm="a > b" hx-sync="this:drop" '
                       'hx-disable="findAll button:not(:disabled)">')
        for method in ('hx-put', 'hx-patch', 'hx-delete'):
            self.assert_ok('form', FORM.replace('hx-post', method) + '>')
        for swap in ('outerHTML', 'beforeend', 'none'):
            self.assert_ok('form', FORM.replace('innerHTML', swap) + '>')

    def test_missing_executor_target_or_swap_is_rejected(self):
        for declaration in (' hx-post="/admin/x"', ' hx-target="#result"', ' hx-swap="innerHTML"'):
            with self.subTest(declaration=declaration):
                self.assert_bad('form', FORM.replace(declaration, '') + '>', 'E2')
        self.assert_bad('form', FORM + ' hx-delete="/admin/x">', 'E2')

    def test_explicit_multipart_uses_native_encoding(self):
        self.assert_ok('form', FORM + ' hx-encoding="multipart/form-data">')
        self.assert_ok('form', FORM + ' enctype="multipart/form-data">')

    def test_marker_does_not_select_a_protocol_or_action(self):
        for marker in ('data-write', 'data-write=""', 'data-write="create"', 'data-write="update"'):
            self.assert_ok('form', FORM.replace('data-write', marker) + '>')
        self.assert_ok('form', FORM.replace('/admin/x', '/api/x') + '>')
        for endpoint in ('//remote.test/api/x', 'https://remote.test/api/x', '/\\remote.test/x', 'relative/x'):
            self.assert_bad('form', FORM.replace('/admin/x', endpoint) + '>', 'E3b')

    def test_removed_shaping_and_result_attributes_always_fail(self):
        for attr in ('url', 'confirm', 'extra', 'outcome', 'json', 'json-single', 'json-wrap',
                     'lines', 'keep-empty', 'ids-scope', 'rows', 'object', 'number', 'float',
                     'bool', 'array', 'skip-empty', 'api', 'mode', 'html', 'flash', 'flash-target',
                     'items', 'latest', 'handler'):
            for value in ('', '="old"'):
                with self.subTest(attr=attr, value=value):
                    self.assert_bad('button', '<button data-write-' + attr + value + '>', 'E1')
        self.assert_bad('input', '<input data-write-number="1">', 'E1')

    def test_unknown_names_fail_with_or_without_values(self):
        for attr in ('data-write-item', 'data-write-lastest', 'data-write-frobnicate'):
            for value in ('', '="1"'):
                self.assert_bad('button', '<button ' + attr + value + '>', 'E4')

    def test_flags_and_template_result_hook_are_bare(self):
        for attr in ('reload', 'host', 'status', 'result'):
            self.assert_ok('div', '<div data-write-' + attr + '>')
            for value in ('"1"', '"0"', '""'):
                self.assert_bad('div', '<div data-write-' + attr + '=' + value + '>', 'E5a')

    def test_value_attributes_require_values(self):
        for attr in ('nav', 'close', 'builder'):
            self.assert_bad('button', '<button data-write-' + attr + '>', 'E4')
            self.assert_bad('button', '<button data-write-' + attr + '="">', 'E4')
            self.assert_ok('button', '<button data-write-' + attr + '="value">')
        self.assert_ok('form', FORM + ' data-write-session="visitor">')
        for session in ('', 'admin', 'unknown'):
            self.assert_bad('form', FORM + ' data-write-session="' + session + '">', 'E4')

    def test_button_overrides_do_not_repeat_form_declarations(self):
        self.assert_ok('button', '<button type="submit" data-write>')
        self.assert_ok('button', '<button type="button" hx-post="/admin/other" data-write>')
        self.assert_bad('button', '<button type="submit" hx-post="/admin/other" data-write>', 'E2')
        self.assert_bad('button', '<button hx-post="/admin/other" data-write>', 'E2')

    def test_retired_form_json_is_rejected(self):
        for value in ('number', 'boolean', '', 'object'):
            self.assert_bad('input', '<input data-json-type="' + value + '">', 'E7')
        self.assert_bad('input', '<input data-json-type>', 'E7')
        for extensions in ('form-json', 'other, form-json', 'other form-json'):
            self.assert_bad('form', FORM + ' hx-ext="' + extensions + '">', 'E7')

    def test_quotes_spaces_and_non_write_forms(self):
        self.assert_ok('form', FORM.replace('="', ' = "') + ' hx-confirm="a > b">')
        self.assert_ok('form', "<form data-write='update' hx-post='/admin/x' hx-target='#result' hx-swap='innerHTML'>")
        self.assert_ok('form', '<form action="/api/site/login" method="post">')
        self.assert_ok('form', '<form hx-get="/admin/search">')
        self.assert_bad('form', FORM + ' hx-confirm="a > b" data-write-item>', 'E4')


class FullScriptTest(unittest.TestCase):
    def run_on(self, body, mutate=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            scripts = root / 'scripts'
            scripts.mkdir()
            shutil.copy2(GATE, scripts / GATE.name)
            # 只复制当前基座；门禁不依赖已经退役的编码器。
            relative = 'internal/templates/static/js/ui/write-base.js'
            dest = root / relative
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / relative, dest)
            fixture = root / 'internal/templates/admin/fixture.html'
            fixture.parent.mkdir(parents=True)
            fixture.write_text(body, encoding='utf-8')
            if mutate:
                mutate(root)
            return subprocess.run(['bash', str(scripts / GATE.name)], text=True, capture_output=True)

    def test_legal_fixture_and_inherited_button(self):
        result = self.run_on(FORM + '>\n<button type="submit">保存</button>\n'
                             '<div data-write-status></div><div data-write-result></div></form>')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_quoted_gt_and_multiline_keep_later_attributes(self):
        result = self.run_on(FORM + '\n hx-confirm="a > b"\n data-write-item>')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('data-write-item', result.stderr)

    def test_non_form_tag_is_scanned_and_jet_comments_are_ignored(self):
        result = self.run_on('{* <input data-write-number> *}' + FORM + '>')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        result = self.run_on(FORM + '><input data-write-number></form>')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('data-write-number', result.stderr)

    def test_reverse_registry_failure_changes_exit_code(self):
        for attr in ('data-write-builder', 'data-write-session', 'data-write-close'):
            def remove_consumer(root):
                path = root / 'internal/templates/static/js/ui/write-base.js'
                path.write_text(path.read_text(encoding='utf-8').replace(attr, 'removed-consumer'), encoding='utf-8')
            with self.subTest(attr=attr):
                result = self.run_on(FORM + '>', remove_consumer)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn(attr, result.stderr)

    def test_scope_includes_non_admin_templates(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'scripts').mkdir()
            shutil.copy2(GATE, root / 'scripts' / GATE.name)
            (root / 'internal/templates/site').mkdir(parents=True)
            (root / 'internal/templates/site/write.html').write_text('<form data-write-url="/api/site/x">', encoding='utf-8')
            result = subprocess.run(['bash', str(root / 'scripts' / GATE.name)], text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('data-write-url', result.stderr)


if __name__ == '__main__':
    unittest.main()
