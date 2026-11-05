#!/usr/bin/env python3
"""路由对账扫描器夹具；替代 go 命令只提供路由行，不声称验证真实数据库装配。"""

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class EndpointTest(unittest.TestCase):
    def scan(self, source, routes, exit_code=0):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'scripts').mkdir()
            shutil.copy2(ROOT / 'scripts/check-write-endpoints.sh', root / 'scripts/check-write-endpoints.sh')
            templates = root / 'internal/templates/site'
            templates.mkdir(parents=True)
            (templates / 'page.html').write_text(source, encoding='utf-8')
            binary = root / 'bin'
            binary.mkdir()
            go = binary / 'go'
            go.write_text('#!/bin/sh\nprintf "%s\\n" ' + ' '.join("'" + line + "'" for line in routes) +
                          '\nexit ' + str(exit_code) + '\n', encoding='utf-8')
            go.chmod(0o700)
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'])
            return subprocess.run(['bash', str(root / 'scripts/check-write-endpoints.sh')],
                                  text=True, capture_output=True, env=env)

    def test_write_method_and_url_must_both_match(self):
        for method, expected in [('POST', 0), ('PUT', 1)]:
            result = self.scan('<form hx-post="/api/site/save">', [method + ' /api/site/save'])
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)

    def test_single_quotes_and_spaces_without_api_prefix(self):
        result = self.scan("<button hx-delete = '/admin/item'>", ['DELETE /admin/item'])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_dynamic_segments_cannot_be_verified_by_prefix(self):
        result = self.scan("<button hx-delete = '/api/item/{{id}}'>", ['DELETE /api/item/:id'])
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('未检查', result.stdout + result.stderr)

    def test_dynamic_query_does_not_change_static_path(self):
        result = self.scan('<form hx-post="/admin/save?q={{urlquery("a > b")}}">', ['POST /admin/save'])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_comment_and_quoted_angle_do_not_break_tag_scan(self):
        result = self.scan('<!-- <form hx-post="/missing"> -->{* <form hx-post="/missing"> *}<form title="x > y" hx-post="/save">', ['POST /save'])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unknown_endpoint_fails(self):
        result = self.scan('<form hx-post="/api/missing">', ['POST /api/other'])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('/api/missing', result.stderr)

    def test_failed_route_assembly_cannot_pass_with_partial_route_output(self):
        result = self.scan('<form hx-post="/admin/save">', ['POST /admin/save'], exit_code=1)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('真实路由装配失败', result.stderr)
        self.assertNotIn('对账通过', result.stdout)

    def test_no_route_or_no_htmx_is_not_reported_as_verified(self):
        result = self.scan('<form hx-post="/api/x">', [])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('未检查', result.stdout + result.stderr)
        result = self.scan('<form data-write-url="/api/x">', ['POST /api/x'])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('未验证', result.stderr)


if __name__ == '__main__':
    unittest.main()
