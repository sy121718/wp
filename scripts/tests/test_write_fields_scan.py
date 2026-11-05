#!/usr/bin/env python3
"""真实 METHOD+PATH、原生字段名与确定输入协议的字段门禁回归。"""
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCANNER = pathlib.Path(__file__).resolve().parents[1] / 'write-fields-scan.py'


def contract(*fields, protocol='form'):
    return {'dto': 'Request', 'protocol': protocol,
            'fields': [{'name': field, 'required': True} for field in fields]}


class WriteFieldsScanTest(unittest.TestCase):
    def run_fixture(self, contracts, templates):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(json.dumps(contracts), encoding='utf-8')
            for name, source in templates.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(source, encoding='utf-8')
            return subprocess.run([sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)], text=True, capture_output=True)

    def assert_ok(self, contracts, templates):
        result = self.run_fixture(contracts, templates)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def assert_bad(self, contracts, templates, message):
        result = self.run_fixture(contracts, templates)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(message, result.stdout + result.stderr)

    def test_missing_or_empty_scan_scope_is_not_verified(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            contracts = root / 'contracts.json'
            contracts.write_text('{}', encoding='utf-8')
            for scope in (root, root / 'missing'):
                result = subprocess.run([sys.executable, str(SCANNER), str(contracts), str(scope)], text=True, capture_output=True)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('未检查', result.stdout + result.stderr)

    def test_override_consumes_its_own_fields(self):
        contracts = {'POST /preview': contract('project', 'note'), 'POST /apply': contract('project', 'note')}
        source = '<form hx-post="/preview"><input name="project"><input name="note"><button type="button" hx-post="/apply">应用</button></form>'
        self.assert_ok(contracts, {'form.html': source})
        self.assert_bad({'POST /preview': contract('project'), 'POST /apply': contract('project', 'note')},
                        {'form.html': source}, 'name="note" 不在 POST /preview')

    def test_override_required_and_unknown_endpoint_are_checked(self):
        source = '<form hx-post="/preview"><input name="project"><button type="button" hx-post="/apply">应用</button></form>'
        self.assert_bad({'POST /preview': contract('project'), 'POST /apply': contract('project', 'note')},
                        {'form.html': source}, '必填字段 "note"')
        self.assert_bad({'POST /preview': contract('project')}, {'form.html': source}, 'POST /apply 不在')

    def test_called_import_block_supplies_ids_after_nested_condition(self):
        self.assert_ok({'POST /delete': contract('ids')}, {
            'page.html': '{{import "parts/rows.html"}}<form hx-post="/delete">{{range _, item := items}}{{yield row(item)}}{{end}}</form>',
            'parts/rows.html': '{{block row(item)}}{{if item.Visible}}<span>行</span>{{end}}<input name="ids">{{end}}',
        })

    def test_unused_import_block_cannot_supply_required_field(self):
        self.assert_bad({'POST /delete': contract('ids')}, {
            'page.html': '{{import "rows.html"}}<form hx-post="/delete">{{yield summary()}}</form>',
            'rows.html': '{{block row()}}<input name="ids">{{end}}{{block summary()}}<span>数量</span>{{end}}',
        }, '必填字段 "ids"')

    def test_called_block_resolves_its_own_relative_include(self):
        self.assert_ok({'POST /delete': contract('ids')}, {
            'page.html': '{{import "parts/rows.html"}}<form hx-post="/delete">{{yield row()}}</form>',
            'parts/rows.html': '{{block row()}}{{include "checkbox.html"}}{{end}}',
            'parts/checkbox.html': '<input name="ids">',
        })

    def test_include_typo_is_not_silently_ignored(self):
        self.assert_bad({'POST /preview': contract('amount')}, {
            'page.html': '<form hx-post="/preview">{{include "fields.html"}}</form>',
            'fields.html': '<input name="amout">',
        }, 'name="amout"')

    def test_native_form_names_are_not_json_paths(self):
        for name in ('ids[]', 'ids[0]', 'ids.value'):
            self.assert_bad({'POST /save': contract('ids')}, {
                'page.html': '<form hx-post="/save"><input name="' + name + '"></form>',
            }, 'name="' + name + '"')
        self.assert_ok({'POST /save': contract('ids')}, {
            'page.html': '<form hx-post="/save"><input name="ids"><input name="ids"></form>',
        })

    def test_inert_or_commented_controls_do_not_supply_required_fields(self):
        for controls in ('<template><input name="ids"></template>', '{* <input name="ids"> *}',
                         '<!-- <input name="ids"> -->', '<input name="ids" disabled>'):
            self.assert_bad({'POST /save': contract('ids')}, {
                'page.html': '<form hx-post="/save">' + controls + '</form>',
            }, '必填字段 "ids"')

    def test_form_inside_template_is_checked_itself(self):
        self.assert_ok({'POST /save': contract('ids')}, {
            'page.html': '<template><form hx-post="/save"><input name="ids"></form></template>',
        })

    def test_native_submitter_value_and_other_submitter_are_separate(self):
        source = '<form hx-post="/activate"><button type="submit" name="id" value="one">启用</button></form>'
        self.assert_ok({'POST /activate': contract('id')}, {'page.html': source})
        self.assert_bad({'POST /activate': contract('id')}, {
            'page.html': source.replace('</form>', '<button type="submit">遗漏入口</button></form>'),
        }, '必填字段 "id"')

    def test_external_form_controls_do_not_mix_bulk_and_single_delete(self):
        source = '<form id="bulk" hx-post="/bulk"><input name="ids"><button type="submit" form="single" name="key" value="one">单删</button></form><form id="single" hx-post="/single"></form>'
        self.assert_ok({'POST /bulk': contract('ids'), 'POST /single': contract('key')}, {'page.html': source})

    def test_external_dynamic_form_ids_keep_identical_expression_identity(self):
        self.assert_ok({'POST /save': contract('key')}, {
            'page.html': '<form id="delete-{{i}}" hx-post="/save"></form><button form="delete-{{i}}" name="key" value="{{item.Key}}">删除</button>',
        })

    def test_method_is_part_of_identity(self):
        self.assert_ok({'POST /same': contract('title'), 'DELETE /same': contract('id')}, {
            'page.html': '<form hx-post="/same"><input name="title"></form><form hx-delete="/same"><input name="id"></form>',
        })
        self.assert_bad({'POST /same': contract('id')}, {
            'page.html': '<form hx-delete="/same"><input name="id"></form>',
        }, 'DELETE /same 不在')
        self.assert_bad({'/same': contract('id')}, {
            'page.html': '<form hx-post="/same"><input name="id"></form>',
        }, 'POST /same 不在')

    def test_protocol_mismatch_and_unknown_contract_cannot_pass(self):
        for protocol in ('json', 'multipart'):
            self.assert_bad({'POST /save': contract('id', protocol=protocol)}, {
                'page.html': '<form hx-post="/save"><input name="id"></form>',
            }, '输入协议不匹配')
        self.assert_ok({'POST /upload': contract('file', protocol='multipart')}, {
            'page.html': '<form hx-post="/upload" hx-encoding="multipart/form-data"><input type="file" name="file"></form>',
        })
        self.assert_bad({'POST /save': {'protocol': 'unknown', 'reason': '未知 binder', 'fields': []}}, {
            'page.html': '<form hx-post="/save"></form>',
        }, '未检查 POST /save：未知 binder')

    def test_json_builder_is_reported_unchecked_not_silently_skipped(self):
        self.assert_bad({'POST /save': contract('rows', protocol='json')}, {
            'page.html': '<form hx-post="/save" data-write-builder="rows"><input name="localField"></form>',
        }, 'JSON 对象提供器字段')

    def test_old_exemption_does_not_hide_missing_contract(self):
        self.assert_bad({}, {'page.html': '<form hx-post="/api/order/status"></form>'}, 'POST /api/order/status 不在')

    def test_dynamic_path_not_accepted_by_prefix_but_query_does_not_change_route(self):
        self.assert_bad({'POST /save/:id': contract('id')}, {
            'page.html': '<form hx-post="/save/{{id}}"><input name="id"></form>',
        }, '未检查动态或非本站端点')
        self.assert_ok({'POST /save': contract('id')}, {
            'page.html': '<form hx-post="/save?q={{urlquery("a > b")}}"><input name="id"></form>',
        })

    def test_native_post_form_is_checked_without_htmx(self):
        self.assert_ok({'POST /account': contract('email')}, {
            'page.html': '<form method="post" action="/account"><input name="email"></form>',
        })

    def test_disabled_fieldsets_do_not_supply_required_fields(self):
        self.assert_bad({'POST /save': contract('id')}, {
            'page.html': '<form hx-post="/save"><fieldset disabled><input name="id"></fieldset></form>',
        }, '必填字段 "id"')
        self.assert_ok({'POST /save': contract('id')}, {
            'page.html': '<form hx-post="/save"><fieldset disabled><legend><input name="id"></legend><input name="ignored"></fieldset></form>',
        })
        self.assert_bad({'POST /save': contract('id')}, {
            'page.html': '<form hx-post="/save"><fieldset disabled><legend>第一项</legend><legend><input name="id"></legend></fieldset></form>',
        }, '必填字段 "id"')

    def test_disabled_standalone_request_is_not_a_submission_entry(self):
        self.assert_ok({}, {'page.html': '<button disabled hx-post="/missing">禁用</button>'})

    def test_template_form_does_not_steal_outer_form_controls(self):
        self.assert_ok({'POST /outer': contract('id'), 'POST /inner': contract('title')}, {
            'page.html': '<form hx-post="/outer"><template><form hx-post="/inner"><input name="title"></form></template><input name="id"></form>',
        })

    def test_native_submit_override_uses_its_method_and_action(self):
        self.assert_ok({'POST /save': contract('id'), 'POST /remove': contract('id')}, {
            'page.html': '<form method="post" action="/save"><input name="id"><button>保存</button><button formmethod="post" formaction="/remove">删除</button></form>',
        })
        self.assert_bad({'POST /save': contract('id')}, {
            'page.html': '<form method="post" action="/save"><input name="id"><button formaction="/missing">另存</button></form>',
        }, 'POST /missing 不在')

    def test_additional_htmx_parameters_cannot_be_claimed_checked(self):
        self.assert_bad({'POST /save': contract('id')}, {
            'page.html': '<section hx-include="#extra"><form hx-post="/save"><input name="id"></form></section>',
        }, '未检查')

    def test_legacy_shaping_does_not_supply_required_keys(self):
        for legacy in ('data-transfer-lines-name="rows"', 'data-write-rows=".row;name,value"'):
            self.assert_bad({'POST /save': contract('rows')}, {
                'page.html': '<form hx-post="/save"><div ' + legacy + '></div></form>',
            }, '必填字段 "rows"')


if __name__ == '__main__':
    unittest.main()
