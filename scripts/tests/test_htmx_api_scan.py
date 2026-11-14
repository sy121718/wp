#!/usr/bin/env python3
"""验证真实 HTMX 4 门禁，不再依赖退役 form-json 编码器。"""
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class HtmxAPITest(unittest.TestCase):
    def run_on(self, html='<form hx-post="/admin/x" hx-target="#result" hx-swap="innerHTML"></form>', extra_js=''):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for relative in ('scripts/check-htmx-api.sh', 'pkg/view/ui/assets/js/ui/htmx.min.js'):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / relative, path)
            # 门禁现在扫三个模板属主；夹具必须镜像真实布局，否则「扫描根缺失」护栏会判红。
            for shared in ('pkg/view/ui/templates', 'pkg/builder'):
                (root / shared).mkdir(parents=True, exist_ok=True)
            fixture = root / 'internal/templates/fixture.html'
            fixture.parent.mkdir(parents=True, exist_ok=True)
            fixture.write_text(html, encoding='utf-8')
            control = root / 'internal/templates/static/js/control.js'
            control.parent.mkdir(parents=True, exist_ok=True)
            control.write_text(extra_js, encoding='utf-8')
            return subprocess.run(['bash', str(root / 'scripts/check-htmx-api.sh')], text=True, capture_output=True)

    def test_real_htmx_attributes_pass_without_form_json(self):
        result = self.run_on()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_retired_extension_attribute_is_not_assumed_supported(self):
        result = self.run_on('<form hx-post="/save" hx-ext="form-json"></form>')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('hx-ext', result.stderr)

    def test_control_v2_api_and_events_are_not_ignored(self):
        for source in ('htmx.defineExtension("bad", {});', 'htmx.onLoad(function () {});',
                       'document.addEventListener("htmx:afterRequest", function () {});'):
            with self.subTest(source=source):
                result = self.run_on(extra_js=source)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unknown_extension_attributes_are_not_wildcard_allowed(self):
        for name in ('hx-form-json', 'hx-timeout', 'hx-imaginary-extension'):
            with self.subTest(name=name):
                result = self.run_on('<form hx-post="/save" ' + name + '="x"></form>')
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn(name, result.stderr)

    def test_v2_disabled_attribute_fails(self):
        result = self.run_on('<form hx-post="/save" hx-disabled-elt="button"></form>')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('hx-disabled-elt', result.stderr)


if __name__ == '__main__':
    unittest.main()
