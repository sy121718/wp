#!/usr/bin/env python3
"""对象提供器对账的可失败反例：场景绑定、标识语义、集合覆盖与 builder 一致。

每个反例都通过临时契约/驱动变异触发，证明断言真的会报错，而不是只跑一遍默认样本。
"""
import json
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCANNER = ROOT / 'scripts/write-object-keys.js'
CONTRACTS = ROOT / 'internal/templates/static/js/ui/write-contracts.json'


def load_contracts():
    return json.loads(CONTRACTS.read_text(encoding='utf-8'))


class WriteObjectKeysTest(unittest.TestCase):
    def run_on(self, contracts, mutate_driver=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            script = root / 'scripts' / 'write-object-keys.js'
            script.parent.mkdir(parents=True)
            source = SCANNER.read_text(encoding='utf-8')
            if mutate_driver:
                source = mutate_driver(source)
            script.write_text(source, encoding='utf-8')
            # 脚本按 ROOT = 脚本上一级解析源文件；夹具用符号链接镜像仓库根。
            for rel in ('internal/templates/static/js/ui', 'pkg/view/ui/assets/js/ui',
                        'internal/templates/static/js/editor-app'):
                target = root / rel
                target.mkdir(parents=True)
                for src in (ROOT / rel).glob('*.js'):
                    (target / src.name).symlink_to(src)
            contracts_path = root / 'contracts.json'
            contracts_path.write_text(json.dumps(contracts), encoding='utf-8')
            return subprocess.run(['node', str(script), str(contracts_path)],
                                  text=True, capture_output=True)

    def test_default_drivers_pass(self):
        result = self.run_on(load_contracts())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_workbench_panel_provider_requires_real_object(self):
        # 工作台面板的 provider 只组装对象；把 accessPassword 去掉后，
        # 密码场景的 expect 断言必须红 —— 证明它真的执行了 provider，而不是只看键名。
        contracts = load_contracts()

        def mutate(src):
            return src.replace("expect: { accessPassword: 'p@ssw0rd' } },", "expect: { accessPassword: 'other' } },")

        result = self.run_on(contracts, mutate)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('accessPassword', result.stdout + result.stderr)

    def test_update_scenario_without_id_fails(self):
        # attribute-group 更新场景缺 id：更新 DTO 的 id 是必填标识，不能算覆盖。
        contracts = load_contracts()
        for field in contracts['POST /api/product/attribute/update']['fields']:
            if field['name'] == 'id':
                field['required'] = True

        def mutate(src):
            # 去掉场景的身份期望，同时让更新表单不再带 id。
            src = src.replace("expect: { id: 'attr-1' } },", "expect: {} },")
            return src.replace("fields: { projectId: 'p1', id: 'attr-1', name: '颜色', sort: '1' }",
                               "fields: { projectId: 'p1', name: '颜色', sort: '1' }")

        result = self.run_on(contracts, mutate)
        # 驱动脚本自身必须拒绝「更新场景缺 id」：否则覆盖断言形同虚设。
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_missing_collection_does_not_pass(self):
        # 声明有行却读不到行：集合断言必须红。
        contracts = load_contracts()

        def mutate(src):
            return src.replace("rowSets: { '.transfer-line': [rowStub({ '[data-line-stock]': 'sku-1', '[data-line-qty]': '3' })] },",
                               "rowSets: { '.transfer-line': [] },")

        result = self.run_on(contracts, mutate)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('没有任何非空集合', result.stdout + result.stderr)

    def test_bulk_ids_must_reach_provider(self):
        # bulkIDs 没接到真实读取路径时，ids 集合断言必须红。
        contracts = load_contracts()

        def mutate(src):
            return src.replace("if (sel === '[data-list-bulk]' && form.__bulkScope) return form.__bulkScope;",
                               "if (false) return null;")

        result = self.run_on(contracts, mutate)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('ids', result.stdout + result.stderr)

    def test_scenario_cannot_borrow_another_endpoint_dto(self):
        # update 场景的键不能靠 create DTO 包含就通过。
        contracts = load_contracts()
        for field in contracts['POST /api/product/category/update']['fields']:
            if field['name'] == 'id':
                contracts['POST /api/product/category/update']['fields'].remove(field)
                break

        result = self.run_on(contracts)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_empty_contracts_fail(self):
        result = self.run_on({})
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)




class RequiredAndRowContentTest(unittest.TestCase):
    """对象提供器的两类漏检反例：必填字段缺失、行内容错误。"""

    def run_on(self, contracts, mutate_driver=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            script = root / 'scripts' / 'write-object-keys.js'
            script.parent.mkdir(parents=True)
            source = SCANNER.read_text(encoding='utf-8')
            if mutate_driver:
                source = mutate_driver(source)
            script.write_text(source, encoding='utf-8')
            for rel in ('internal/templates/static/js/ui', 'pkg/view/ui/assets/js/ui'):
                target = root / rel
                target.mkdir(parents=True)
                for src in (ROOT / rel).glob('*.js'):
                    (target / src.name).symlink_to(src)
            contracts_path = root / 'contracts.json'
            contracts_path.write_text(json.dumps(contracts), encoding='utf-8')
            return subprocess.run(['node', str(script), str(contracts_path)],
                                  text=True, capture_output=True)

    def test_missing_required_field_fails(self):
        contracts = load_contracts()
        # 让调拨端点的 projectId 变成必填：场景没有产出它时必须红。
        for field in contracts['POST /api/inventory/transfer/create']['fields']:
            if field['name'] == 'projectId':
                field['required'] = True
        result = self.run_on(contracts)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('缺必填字段', result.stdout + result.stderr)

    def test_wrong_row_key_fails(self):
        # 把调拨 provider 里的 stockId 改名：数组长度不变，但行内容错了。
        # 行内容由**provider 源码**决定，所以变异 provider 文件而不是驱动脚本。
        contracts = load_contracts()
        provider_rel = 'internal/templates/static/js/ui/inventory_transfer.js'
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            script = root / 'scripts' / 'write-object-keys.js'
            script.parent.mkdir(parents=True)
            script.write_text(SCANNER.read_text(encoding='utf-8'), encoding='utf-8')
            ui = root / 'internal/templates/static/js/ui'
            (root / 'pkg/view/ui/assets/js/ui').mkdir(parents=True)
            ui.mkdir(parents=True)
            for rel in ('internal/templates/static/js/ui', 'pkg/view/ui/assets/js/ui'):
                (root / rel).mkdir(parents=True, exist_ok=True)
                for src in (ROOT / rel).glob('*.js'):
                    text = src.read_text(encoding='utf-8')
                    if src == ROOT / provider_rel:
                        text = text.replace(
                            'lines.push({stockId: stockId, quantity: Number.isFinite(quantity) ? quantity : 0});',
                            'lines.push({sku: stockId, quantity: Number.isFinite(quantity) ? quantity : 0});')
                    (root / rel / src.name).write_text(text, encoding='utf-8')
            contracts_path = root / 'contracts.json'
            contracts_path.write_text(json.dumps(contracts), encoding='utf-8')
            result = subprocess.run(['node', str(script), str(contracts_path)],
                                    text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('stockId', result.stdout + result.stderr)

if __name__ == '__main__':
    unittest.main()
