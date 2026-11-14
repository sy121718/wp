#!/usr/bin/env python3
"""真实 METHOD+PATH、原生字段名与确定输入协议的字段门禁回归。"""
import hashlib
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

    def test_empty_contract_fails(self):
        # 空契约不能当作「全部对账」：没有任何端点可核对时门禁必须失败。
        self.assert_bad({}, {'a.html': '<form hx-post="/save"><input name="id"></form>'},
                        '写端点契约为空')

    def test_json_provider_ledger_requires_matching_dto(self):
        # 台账存在但 DTO 与当前契约不一致（生成物过期）：不能摘出该端点。
        self.assert_bad({'POST /save': contract('id', protocol='json')},
                        {'a.html': '<form data-write-builder="x" hx-post="/save"><input name="id"></form>'},
                        'JSON 对象提供器字段')

    def test_json_provider_requires_ledger(self):
        templates = {'a.html': '<form data-write-builder="x" hx-post="/save"><input name="id"></form>'}
        contracts = {'POST /save': contract('id', protocol='json')}
        self.assert_bad(contracts, templates, 'JSON 对象提供器字段')

    def test_json_provider_ledger_with_matching_dto_passes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(
                json.dumps({'POST /save': contract('id', protocol='json')}), encoding='utf-8')
            # 台账必须带契约字段表与 provider 源码指纹；缺任一项都视为过期证据。
            (root / 'provider.js').write_text('// provider', encoding='utf-8')
            import hashlib
            fingerprint = hashlib.sha256(b'// provider').hexdigest()[:16]
            # 台账必须带字段契约指纹与场景形状指纹：缺任一项都视为过期证据。
            import hashlib
            fields_payload = [['id', True]]
            fields_hash = hashlib.sha256(
                json.dumps(fields_payload, ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()[:16]
            cases = [{'name': 'case', 'keys': ['id'], 'endpoint': 'POST /save'}]
            case_payload = sorted([['case', ['id']]], key=lambda p: json.dumps(p, ensure_ascii=False))
            case_hash = hashlib.sha256(
                json.dumps(case_payload, ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()[:16]
            # 台账还必须登记产出它的驱动脚本指纹：驱动改过而台账没重跑时证据过期。
            driver = root / 'scripts' / 'write-object-keys.js'
            driver.parent.mkdir(parents=True, exist_ok=True)
            driver.write_text('// driver', encoding='utf-8')
            driver_hash = hashlib.sha256(b'// driver').hexdigest()[:16]
            (root / 'write-object-providers.json').write_text(
                json.dumps({'POST /save': {'dto': 'Request', 'provider': 'x', 'source': 'provider.js',
                                           'sourceHash': fingerprint, 'fields': ['id'], 'keys': ['id'],
                                           'fieldsHash': fields_hash, 'caseShape': case_hash,
                                           'driver': 'scripts/write-object-keys.js',
                                           'driverHash': driver_hash,
                                           'cases': cases}}),
                encoding='utf-8')
            (root / 'a.html').write_text('<form data-write-builder="x" hx-post="/save"><input name="id"></form>', encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def _ledger_root(self, root):
        """与 test_json_provider_ledger_with_matching_dto_passes 同构的可用台账夹具。"""
        import hashlib
        (root / 'contracts.json').write_text(
            json.dumps({'POST /save': contract('id', protocol='json')}), encoding='utf-8')
        (root / 'provider.js').write_text('// provider', encoding='utf-8')
        (root / 'a.html').write_text('<form data-write-builder="x" hx-post="/save"><input name="id"></form>', encoding='utf-8')
        fields_hash = hashlib.sha256(
            json.dumps([['id', True]], ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()[:16]
        cases = [{'name': 'case', 'keys': ['id'], 'endpoint': 'POST /save'}]
        case_hash = hashlib.sha256(
            json.dumps([['case', ['id']]], ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()[:16]
        driver = root / 'scripts' / 'write-object-keys.js'
        driver.parent.mkdir(parents=True, exist_ok=True)
        driver.write_text('// driver', encoding='utf-8')
        return fields_hash, cases, case_hash

    def test_changed_driver_invalidates_old_ledger(self):
        # 核心反例：驱动改了（场景/预期值/嵌套断言），台账没重跑。
        # 台账里的 cases 与它自己的 caseShape 依然自洽 —— 只比 caseShape 会放过旧证据。
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            fields_hash, cases, case_hash = self._ledger_root(root)
            old_hash = hashlib.sha256(b'// driver').hexdigest()[:16]
            (root / 'write-object-providers.json').write_text(json.dumps({
                'POST /save': {'dto': 'Request', 'provider': 'x', 'source': 'provider.js',
                               'sourceHash': hashlib.sha256(b'// provider').hexdigest()[:16],
                               'fields': ['id'], 'keys': ['id'], 'fieldsHash': fields_hash,
                               'caseShape': case_hash, 'cases': cases,
                               'driver': 'scripts/write-object-keys.js', 'driverHash': old_hash},
            }), encoding='utf-8')
            # 驱动被编辑：预期值/嵌套断言变化不影响台账自身自洽，但驱动指纹必须对不上。
            (root / 'scripts' / 'write-object-keys.js').write_text(
                '// driver edited: expect 1 -> 2', encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('JSON 对象提供器字段', result.stdout + result.stderr)

    def test_ledger_without_driver_fingerprint_fails(self):
        # 没有驱动指纹的旧台账不能继续自证：按过期证据处理。
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            fields_hash, cases, case_hash = self._ledger_root(root)
            (root / 'write-object-providers.json').write_text(json.dumps({
                'POST /save': {'dto': 'Request', 'provider': 'x', 'source': 'provider.js',
                               'sourceHash': hashlib.sha256(b'// provider').hexdigest()[:16],
                               'fields': ['id'], 'keys': ['id'], 'fieldsHash': fields_hash,
                               'caseShape': case_hash, 'cases': cases},
            }), encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('JSON 对象提供器字段', result.stdout + result.stderr)

    def test_json_provider_ledger_with_wrong_dto_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(
                json.dumps({'POST /save': contract('id', protocol='json')}), encoding='utf-8')
            (root / 'write-object-providers.json').write_text(
                json.dumps({'POST /save': {'dto': 'Other', 'provider': 'x', 'keys': ['id']}}),
                encoding='utf-8')
            (root / 'a.html').write_text('<form data-write-builder="x" hx-post="/save"><input name="id"></form>', encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_stale_provider_ledger_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(
                json.dumps({'POST /save': contract('id')}), encoding='utf-8')
            (root / 'write-object-providers.json').write_text(
                json.dumps({'POST /gone': {'dto': 'Request'}}), encoding='utf-8')
            (root / 'a.html').write_text('<form hx-post="/save"><input name="id"></form>', encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('已退役端点', result.stdout + result.stderr)

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
        # 空契约现在会先被「契约为空」拦下；用一条无关契约证明「不在契约里」仍会报。
        self.assert_bad({'POST /other': contract('id')},
                        {'page.html': '<form hx-post="/api/order/status"></form>'},
                        'POST /api/order/status 不在')

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
        # 空契约本身会让门禁失败，因此给一条无关契约，只验证「禁用的独立入口不是提交入口」。
        self.assert_ok({'POST /other': contract('id')},
                       {'page.html': '<button disabled hx-post="/missing">禁用</button>'})

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

    def test_static_field_source_registration_must_match_dto(self):
        # 登记了静态字段来源，但 DTO 已删字段：登记过期必须红。
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            contracts = {'POST /save': {'dto': 'Request', 'protocol': 'form',
                                        'fields': [{'name': 'kept'}]}}
            (root / 'contracts.json').write_text(json.dumps(contracts), encoding='utf-8')
            (root / 'a.html').write_text(
                '<form hx-post="/save">{{for f := .Fields}}<input name="{{f.Name}}">{{end}}</form>',
                encoding='utf-8')
            result = subprocess.run(
                [sys.executable, str(SCANNER), str(root / 'contracts.json'), str(root)],
                text=True, capture_output=True)
            # 没有登记时如实报未检查，不会静默通过。
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('动态字段名', result.stdout + result.stderr)




    # —— 新增协议：nobody / raw / query 的反例 ——

    def test_nobody_endpoint_rejects_business_fields(self):
        # 处理器完全不读 body：表单带业务字段会被静默丢弃，门禁必须报出来。
        self.assert_bad({'POST /logout': {'protocol': 'nobody', 'fields': []}},
                        {'a.html': '<form hx-post="/logout"><input name="id"></form>'},
                        '不读请求体')

    def test_nobody_endpoint_allows_csrf_only(self):
        # 只带 CSRF 的登出表单是合法形态。
        self.assert_ok({'POST /logout': {'protocol': 'nobody', 'fields': []}},
                       {'a.html': '<form hx-post="/logout"><input name="csrf_token"></form>'})

    def test_raw_endpoint_rejects_template_submission(self):
        # 原始字节协议由外部定义，界面模板不能提交。
        self.assert_bad({'POST /callback': {'protocol': 'raw', 'fields': []}},
                        {'a.html': '<form hx-post="/callback"><input name="csrf_token"></form>'},
                        '原始字节协议端点')

    def test_query_endpoint_rejects_body_fields(self):
        # 查询串端点：body 字段不会生效，必须报出来。
        self.assert_bad({'POST /list': {'dto': 'ListReq', 'protocol': 'query',
                                        'fields': [{'name': 'page'}]}},
                        {'a.html': '<form hx-post="/list"><input name="page"></form>'},
                        '查询串端点')

    def test_query_endpoint_accepts_keys_on_action_url(self):
        # 键在 action URL 的 query 上、且落在 DTO 字段表里时通过。
        self.assert_ok({'POST /list': {'dto': 'ListReq', 'protocol': 'query',
                                       'fields': [{'name': 'page'}]}},
                       {'a.html': '<form hx-post="/list?page=1"><input name="csrf_token"></form>'})

    def test_query_endpoint_rejects_unknown_query_key(self):
        self.assert_bad({'POST /list': {'dto': 'ListReq', 'protocol': 'query',
                                        'fields': [{'name': 'page'}]}},
                        {'a.html': '<form hx-post="/list?pgae=1"><input name="csrf_token"></form>'},
                        '不在')


class StaticSourceAndVariantTest(unittest.TestCase):
    """静态来源 / 变体分支的漏检反例：这些形状以前会被放过。"""

    SCANNER = SCANNER

    def run_fixture(self, contracts, templates):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(json.dumps(contracts), encoding='utf-8')
            for name, source in templates.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(source, encoding='utf-8')
            return subprocess.run([sys.executable, str(self.SCANNER), str(root / 'contracts.json'), str(root)],
                                  text=True, capture_output=True)

    def test_variant_with_all_branches_missing_required_fails(self):
        contracts = {
            'POST /save': {
                'dto': 'MainReq', 'protocol': 'form',
                'fields': [{'name': 'id', 'required': True}],
                'variants': [{'dto': 'AltReq', 'protocol': 'form',
                              'fields': [{'name': 'ids', 'required': True}]}],
            }
        }
        source = '<form hx-post="/save"><input name="other"></form>'
        result = self.run_fixture(contracts, {'a.html': source})
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('每个输入分支都缺必填字段', result.stdout + result.stderr)

    def test_variant_with_one_complete_branch_passes(self):
        contracts = {
            'POST /save': {
                'dto': 'MainReq', 'protocol': 'form',
                'fields': [{'name': 'id', 'required': True}],
                'variants': [{'dto': 'AltReq', 'protocol': 'form',
                              'fields': [{'name': 'ids', 'required': True}]}],
            }
        }
        # 只提交 ids：主分支缺 id，但变体分支齐备 —— 真实提交可能走变体，应通过。
        result = self.run_fixture(contracts, {'a.html': '<form hx-post="/save"><input name="ids"></form>'})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_static_typo_fails_even_when_dynamic_names_present(self):
        # 用一个显式登记的端点做真实扫描：动态名之外再拼错一个静态名。
        contracts = json.loads(pathlib.Path(
            'internal/templates/static/js/ui/write-contracts.json').read_text(encoding='utf-8'))
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / 'contracts.json').write_text(json.dumps(contracts), encoding='utf-8')
            rel = 'internal/templates/admin/project'
            (root / rel).mkdir(parents=True)
            # 与真实模板同一端点，但静态字段拼错一个字符。
            (root / rel / 'bad.html').write_text(
                '<form hx-post="/admin/themes/settings/save">'
                '<input name="motion.eeasing"><input name="{{f.Name}}">'
                '</form>', encoding='utf-8')
            result = subprocess.run([sys.executable, str(self.SCANNER), str(root / 'contracts.json'), str(root)],
                                    text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('motion.eeasing', result.stdout + result.stderr)

if __name__ == '__main__':
    unittest.main()
