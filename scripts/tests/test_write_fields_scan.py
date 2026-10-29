#!/usr/bin/env python3
"""验证提交按钮覆盖端点时，所有字段有消费方且每个端点的必填项齐全。"""
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCANNER = pathlib.Path(__file__).resolve().parents[1] / 'write-fields-scan.py'

class WriteFieldsScanTest(unittest.TestCase):
    def run_fixture(self, contracts, templates):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(json.dumps(contracts))
            for name, source in templates.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(source, encoding='utf-8')
            return subprocess.run([sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)], text=True, capture_output=True)

    def run_scan(self, fields, button='/apply'):
        contracts = {
            '/preview': {'dto': 'PreviewReq', 'fields': [{'name': 'project', 'required': True}]},
            '/apply': {'dto': 'ApplyReq', 'fields': [{'name': 'project', 'required': True}, {'name': 'note', 'required': True}]},
        }
        source = '<form data-write-url="/preview">' + ''.join('<input name="'+name+'">' for name in fields) + '<button type="submit" data-write-url="'+button+'">提交</button></form>'
        return self.run_fixture(contracts, {'form.html': source})

    def test_override_consumes_its_own_fields(self):
        result = self.run_scan(['project', 'note'])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unknown_field_still_fails(self):
        result = self.run_scan(['project', 'note', 'typo'])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('typo', result.stdout)

    def test_override_required_field_is_checked(self):
        result = self.run_scan(['project'])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('必填字段 "note"', result.stdout)

    def test_unknown_override_endpoint_fails(self):
        result = self.run_scan(['project', 'note'], '/missing')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('/missing', result.stdout)

    def test_called_import_block_supplies_ids_after_nested_condition(self):
        contracts = {'/delete': {'dto': 'DeleteReq', 'fields': [{'name': 'ids', 'required': True}]}}
        result = self.run_fixture(contracts, {
            'page.html': '{{import "parts/rows.html"}}<form data-write-url="/delete">{{range _, item := items}}{{yield row(item)}}{{end}}</form>',
            'parts/rows.html': '{{block row(item)}}{{if item.Visible}}<span>行</span>{{end}}<input name="ids[]">{{end}}',
        })
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unused_import_block_cannot_supply_required_field(self):
        contracts = {'/delete': {'dto': 'DeleteReq', 'fields': [{'name': 'ids', 'required': True}]}}
        result = self.run_fixture(contracts, {
            'page.html': '{{import "rows.html"}}<form data-write-url="/delete">{{yield summary()}}</form>',
            'rows.html': '{{block row()}}<input name="ids">{{end}}{{block summary()}}<span>数量</span>{{end}}',
        })
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('必填字段 "ids"', result.stdout)

    def test_called_block_resolves_its_own_relative_include(self):
        contracts = {'/delete': {'dto': 'DeleteReq', 'fields': [{'name': 'ids', 'required': True}]}}
        result = self.run_fixture(contracts, {
            'page.html': '{{import "parts/rows.html"}}<form data-write-url="/delete">{{yield row()}}</form>',
            'parts/rows.html': '{{block row()}}{{include "checkbox.html"}}{{end}}',
            'parts/checkbox.html': '<input name="ids">',
        })
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_include_unknown_field_is_not_silently_ignored(self):
        contracts = {'/preview': {'dto': 'PreviewReq', 'fields': [{'name': 'amount', 'required': True}]}}
        result = self.run_fixture(contracts, {
            'page.html': '<form data-write-url="/preview">{{include "fields.html"}}</form>',
            'fields.html': '<input name="amout">',
        })
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('name="amout"', result.stdout)
        self.assertIn('必填字段 "amount"', result.stdout)

    def test_dynamic_row_container_is_a_real_required_field(self):
        contracts = {'/transfer': {'dto': 'TransferReq', 'fields': [{'name': 'lines', 'required': True}]}}
        result = self.run_fixture(contracts, {'page.html': '<form data-write-url="/transfer"><div data-transfer-lines-name="lines"></div></form>'})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_legacy_container_declarations_supply_their_output_keys(self):
        contracts = {'/save': {'dto': 'SaveReq', 'fields': [{'name': 'rows', 'required': True}, {'name': 'ruleParams', 'required': True}]}}
        result = self.run_fixture(contracts, {'page.html': '<form data-write-url="/save" data-write-rows=".row;name,value" data-write-object=".rule;ruleParams;ruleType"></form>'})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_commented_controls_cannot_supply_required_field(self):
        contracts = {'/delete': {'dto': 'DeleteReq', 'fields': [{'name': 'ids', 'required': True}]}}
        result = self.run_fixture(contracts, {'page.html': '<form data-write-url="/delete">{* <input name="ids"> *}</form>'})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('必填字段 "ids"', result.stdout)

    def test_submitter_value_requires_explicit_write_extra(self):
        contracts = {'/activate': {'dto': 'ActivateReq', 'fields': [{'name': 'id', 'required': True}]}}
        for extra, expected in [('', 1), (' data-write-extra="id"', 0)]:
            result = self.run_fixture(contracts, {'page.html': '<form data-write-url="/activate"><button type="submit" name="id" value="one"'+extra+'>启用</button></form>'})
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)

    def test_other_actions_extra_cannot_supply_required_field(self):
        contracts = {'/activate': {'dto': 'ActivateReq', 'fields': [{'name': 'id', 'required': True}]}}
        result = self.run_fixture(contracts, {'page.html': '<form data-write-url="/activate"><button type="submit" name="id" value="one" data-write-extra="id">正确入口</button><button type="submit">遗漏入口</button></form>'})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('必填字段 "id"', result.stdout)

    def test_bulk_override_clears_other_actions_extra_fields(self):
        contracts = {
            '/status': {'dto': 'StatusReq', 'fields': [{'name': 'ids', 'required': True}, {'name': 'status', 'required': True}]},
            '/unlock': {'dto': 'UnlockReq', 'fields': [{'name': 'ids', 'required': True}]},
        }
        source = '<form data-write-url="/status" data-write="bulk" data-write-extra="status"><input name="ids"><button type="submit" name="status" value="1">启用</button><button type="submit" data-write-url="/unlock" data-write-extra="">解锁</button></form>'
        result = self.run_fixture(contracts, {'page.html': source})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

if __name__ == '__main__':
    unittest.main()
