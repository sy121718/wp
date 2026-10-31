#!/usr/bin/env python3
"""门禁 scripts/check-write-attributes.sh 的行为测试。

被测对象就是**真实脚本**：这里不抄判据、不抽片段，两种执行方式都用：
  · judge 入口：把夹具标签（kind TAB tag）喂 stdin，跑整条判据线；
  · 全脚本：先把真实模板里的写表单计数（基线），再往临时副本模板目录注入夹具，
    断言全脚本的退出码与输出。

夹具标签只有一份来源：本文件里用 extract_tags()（直接调被 gate 包住的解析器
检查入口，见 RE_* 常量）或手写的标签字符串；每个夹具都断言「被解析出来的属性集」，
这样「标签被截断、属性丢失」也会直接变红，而不是仅靠判据的间接症状。
"""

import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GATE = ROOT / 'scripts' / 'check-write-attributes.sh'
BASE_JS = ROOT / 'internal/templates/static/js/ui/write-base.js'
TPL_DIR = ROOT / 'internal/templates/admin'
TAB = '\t'


def judge(kind: str, tag: str) -> subprocess.CompletedProcess:
    """把一行夹具喂给门禁的 judge 入口（跑真实判据）。"""
    return subprocess.run(
        ['bash', str(GATE), 'judge'],
        input=kind + TAB + tag + '\n',
        text=True,
        capture_output=True,
        cwd=str(ROOT),
    )


def violations(kind: str, tag: str) -> list[str]:
    r = judge(kind, tag)
    return [ln for ln in r.stdout.splitlines() if ln and not ln.startswith('BAD')]


class FixtureTestCase(unittest.TestCase):
    def assert_ok(self, kind, tag):
        bad = violations(kind, tag)
        self.assertEqual(bad, [], '不应违规，实际:\n' + '\n'.join(bad))

    def assert_violation(self, kind, tag, code):
        bad = violations(kind, tag)
        self.assertTrue(bad, '应当违规却是绿的: ' + tag)
        self.assertTrue(
            any(ln.split(TAB)[0] == code for ln in bad),
            '期望 ' + code + '，实际:\n' + '\n'.join(bad),
        )


class ExtractorTest(FixtureTestCase):
    """标签提取：引号内的 '>' 不得截断标签，后续属性必须还在。"""

    def test_gt_inside_double_quotes_keeps_later_attributes(self):
        # data-write-confirm 里带 '>'；截断的话 data-write-item 会消失，判据 4 就漏了。
        tag = '<form data-write="create" data-write-url="/api/x" data-write-confirm="a > b" data-write-item>'
        self.assert_violation('form', tag, 'E4')

    def test_gt_inside_double_quotes_keeps_bare_items(self):
        tag = '<form data-write="create" data-write-url="/api/x" data-write-confirm="a > b" data-write-items>'
        self.assert_ok('form', tag)

    def test_gt_inside_single_quotes_keeps_later_attributes(self):
        tag = "<form data-write='update' data-write-url='/api/x' data-write-confirm='a > b' data-write-item>"
        self.assert_violation('form', tag, 'E4')

    def test_spaces_around_equals_are_accepted(self):
        tag = "<form data-write = 'update' data-write-url = '/api/x' data-write-latest>"
        self.assert_ok('form', tag)

    def test_whitespace_normalised_no_tab_breaks_line_splitting(self):
        # 属性之间大量换行/制表：归一后仍是一条「kind TAB tag」，不应产生解析错误。
        tag = '<form\n\tdata-write="create"\n\tdata-write-url="/api/x"\n\tdata-write-items>'
        self.assert_ok('form', tag)

    def test_bare_attribute_at_end_after_quoted_gt(self):
        tag = '<form data-write="bulk" data-write-url="/api/x" data-confirm="x > y" data-write-rows=".r;a">'
        self.assert_ok('form', tag)


class BehaviorFlagTest(FixtureTestCase):
    def test_items_on_create_is_legal(self):
        self.assert_ok('form', '<form data-write="create" data-write-url="/api/x" data-write-items>')

    def test_items_on_bulk_redundant_but_legal(self):
        # bulk 本来就消费 items；显式再声明是噪音，不是违规（门禁不越权禁冗余）。
        self.assert_ok('form', '<form data-write="bulk" data-write-url="/api/x" data-write-items>')

    def test_latest_is_legal_on_every_mode(self):
        for mode in ('create', 'update', 'delete', 'bulk', 'custom', 'single'):
            with self.subTest(mode=mode):
                self.assert_ok('form', '<form data-write="' + mode + '" data-write-url="/api/x" data-write-latest>')

    def test_items_with_attached_value_is_rejected(self):
        # 基座 hasAttr 只读存在性：带值不是开关语义，剥值放过会漏判。
        self.assert_violation('form', '<form data-write="create" data-write-url="/api/x" data-write-items="1">', 'E5a')

    def test_misspelled_items_is_rejected(self):
        self.assert_violation('form', '<form data-write="create" data-write-url="/api/x" data-write-item>', 'E4')

    def test_misspelled_latest_is_rejected(self):
        self.assert_violation('form', '<form data-write="update" data-write-url="/api/x" data-write-lastest>', 'E4')

    def test_button_flags_are_not_judged_by_mode(self):
        # 按钮上的 data-write 可以继承表单：孤立一行判不出 mode，所以只核裸属性形态。
        self.assert_ok('button', '<button type="submit" data-write-url="/api/x" data-write-items>')

    def test_button_flag_with_value_still_rejected(self):
        self.assert_violation('button', '<button type="submit" data-write-url="/api/x" data-write-latest="1">', 'E5a')

    def test_button_flag_with_value_no_own_url_still_rejected(self):
        # 继承路径：按钮自己不写 url，基座仍会 hasAttr 读到它的 data-write-latest。
        self.assert_violation('button', '<button type="submit" data-write-latest="1">', 'E5a')

    def test_isolated_form_without_mode_still_reports_missing_mode(self):
        # 语义作用域只认「带值 data-write」或「自报 url」：只有 url 的表单要报 E3a（缺 mode）。
        self.assert_violation('form', '<form data-write-url="/api/x">', 'E3a')


class ShapePhaseTest(FixtureTestCase):
    """形态阶段（E4/E5a）：无条件执行，不看标签是不是写声明、有没有自身 url。

    这类属性在基座里按 hasAttr 读存在性，写在孤立 <button> 上也照样静默失效，
    所以作用域过滤必须排在它们之后 —— 否则「无 url」的标签会被整条 continue 掉。
    """

    def test_isolated_button_with_value_items_is_E5a(self):
        self.assert_violation('button', '<button type="submit" data-write-items="1">提交</button>', 'E5a')

    def test_isolated_button_with_value_latest_is_E5a(self):
        self.assert_violation('button', '<button type="submit" data-write-latest="0">提交</button>', 'E5a')

    def test_isolated_button_misspelled_bare_flag_is_E4(self):
        self.assert_violation('button', '<button type="submit" data-write-item>提交</button>', 'E4')

    def test_isolated_button_unknown_bare_attribute_is_E4(self):
        # 不是拼错、也不是登记开关：未知裸 data-write-* 一律错，无需推断它属于哪个表单。
        self.assert_violation('button', '<button type="submit" data-write-frobnicate>提交</button>', 'E4')

    def test_button_with_legal_bare_flag_and_no_url_passes(self):
        self.assert_ok('button', '<button type="submit" data-write-items>提交</button>')

    def test_bare_data_write_on_form_is_a_shape_error(self):
        # 裸 data-write 自己就是形态错误（E4），同时仍是写声明 → E3a 报缺 mode。
        bad = violations('form', '<form data-write data-write-url="/api/x">')
        codes = [ln.split(TAB)[0] for ln in bad]
        self.assertIn('E4', codes, '实际:\n' + '\n'.join(bad))
        self.assertIn('E3a', codes, '实际:\n' + '\n'.join(bad))

    def test_bare_data_write_on_button_is_E4(self):
        self.assert_violation('button', '<button type="submit" data-write>提交</button>', 'E4')

    def test_non_write_tags_with_value_nav_or_confirm_stay_clean(self):
        # 带值的 nav / confirm 是合法属性，普通非写标签不该被形态阶段误报。
        self.assert_ok('form', '<form action="/x" data-write-nav="/p/{id}">')
        self.assert_ok('button', '<button type="button" data-write-confirm="a > b">x</button>')


class ScopeTest(FixtureTestCase):
    """语义阶段：带值 data-write 在 form/button 上，或标签自报 data-write-url，才判 E1/E2/E3a/E3b/E5b。

    原判据「自身必须有 data-write-url」把三类真实写法整条放过了：
    合法继承表单 url 的按钮、按钮只带 items 带值、拼错的裸 flag（它本来就没有 url）。
    前两类现在由形态阶段无条件覆盖；语义阶段仍按写声明作用域判。
    """

    def test_button_inheriting_form_url_still_judged_for_removed_attribute(self):
        # 按钮没有自己的 url，但写声明语义（继承表单）仍在：已删属性照样判。
        self.assert_violation('button', '<button type="submit" data-write data-write-number>提交</button>', 'E1')

    def test_button_with_url_still_judged_for_removed_attribute(self):
        self.assert_violation('button', '<button type="submit" data-write-url="/api/x" data-write-mode>提交</button>', 'E1')

    def test_form_flag_does_not_need_own_url_to_be_judged(self):
        # 表单缺 url：E3b 会红，但形态判据（拼错裸属性）同样要红 —— 不能因为缺 url 就整条跳过。
        bad = violations('form', '<form data-write="create" data-write-item>')
        codes = [ln.split(TAB)[0] for ln in bad]
        self.assertIn('E4', codes, '实际:\n' + '\n'.join(bad))
        self.assertIn('E3b', codes, '实际:\n' + '\n'.join(bad))

    def test_tag_with_only_url_is_still_in_scope(self):
        # 只带 url（漏了 data-write）本就要判：E3a 报缺 mode。
        self.assert_violation('form', '<form data-write-url="/api/x">', 'E3a')

    def test_non_write_form_without_data_write_is_out_of_semantic_scope(self):
        # 语义阶段只判写声明：普通非写 form 不进 E1/E2/E3/E5b。
        self.assert_ok('form', '<form action="/x">')

    def test_non_write_button_with_hx_post_is_out_of_semantic_scope(self):
        # htmx 冲突判据（E2）相对写基座才有意义；普通非写按钮不判。
        self.assert_ok('button', '<button type="button" hx-post="/page">刷新</button>')


class ExistingRulesTest(FixtureTestCase):
    def test_missing_mode_on_form_is_rejected(self):
        self.assert_violation('form', '<form data-write-url="/api/x">', 'E3a')

    def test_mode_outside_enum_is_rejected(self):
        self.assert_violation('form', '<form data-write="upsert" data-write-url="/api/x">', 'E3a')

    def test_non_api_url_is_rejected(self):
        self.assert_violation('form', '<form data-write="create" data-write-url="/admin/x">', 'E3b')

    def test_removed_attribute_is_rejected_exact_name(self):
        self.assert_violation('form', '<form data-write="create" data-write-url="/api/x" data-write-number>', 'E1')

    def test_removed_check_uses_exact_attribute_name(self):
        # 旧的 grep -E "\s<attr>([= />])" 是前缀式匹配：data-write-json 会被
        # 已删项 data-write-json-… 之类连带命中。这里确认合法属性不被已删账误伤。
        self.assert_ok('form', '<form data-write="create" data-write-url="/api/x" data-write-json="values" '
                              'data-write-json-single="a,b" data-write-json-wrap>')

    def test_htmx_attributes_are_rejected(self):
        self.assert_violation('form', '<form data-write="create" data-write-url="/api/x" hx-swap="none">', 'E2')

    def test_legit_shaping_attributes_stay_legal(self):
        self.assert_ok(
            'form',
            '<form data-write="create" data-write-url="/api/x" data-write-rows=".r;a,b" '
            'data-write-json="values" data-write-keep-empty="a,b" data-write-reload data-write-status>',
        )

    def test_value_attributes_are_not_mistaken_for_bare(self):
        self.assert_ok(
            'form',
            '<form data-write="create" data-write-url="/api/x" data-write-nav="/p/{id}" data-write-confirm="a > b">',
        )


class ScopeParamTest(FixtureTestCase):
    """作用域 × 属性形态的穷举：每条都要求「被解析出来的属性集」完整，不靠截断捡漏。"""

    def test_bare_shaping_on_inheriting_button_is_legal(self):
        for kind, tag in (
            ('button', '<button type="submit" data-write-rows=".r;a">x</button>'),
            # 裸 data-write 是形态错误（E4），这里要验的是「裸整形属性不被误报」，
            # 所以给 data-write 一个合法值，让它只是背景而不是被测对象。
            ('button', '<button type="submit" data-write="update" data-write-reload>x</button>'),
            ('form', '<form data-write="custom" data-write-url="/api/x" data-write-host>'),
        ):
            with self.subTest(tag=tag):
                self.assert_ok(kind, tag)

    # 「继承表单的提交按钮」的判据要求同一行里**看得出继承**：只有本行自己带写声明
    # （data-write / data-write-url）才算写声明，因为判决只吃单行。此前这两条夹具只写
    # 「type=submit + 属性」，孤立行判不出继承，理应是形态阶段的 E4（未登记裸属性）
    # 或干脆不进语义阶段；把它俩当 E1/E2 的期望，等于要求扫描器凭空知道父表单。
    #
    # 真正的「继承」由守卫单独覆盖：全脚本模式喂 form+button 两行（见
    # test_misspelled_attribute_after_quoted_gt_fails_the_whole_run 与
    # test_legal_fixture_passes_the_whole_run），那时父表单的 url 与 mode 都在上下文里。
    def test_removed_attribute_on_inheriting_button_is_rejected(self):
        self.assert_violation('button', '<button type="submit" data-write data-write-number>x</button>', 'E1')

    def test_htmx_on_inheriting_button_is_rejected(self):
        self.assert_violation('button', '<button type="submit" data-write hx-post="/api/x">x</button>', 'E2')

    def test_bare_removed_attribute_on_button_without_declaration_is_E4(self):
        # 没有写声明时，已删属性名只按「未登记裸属性」报（E1 相对写声明才有意义）。
        self.assert_violation('button', '<button type="submit" data-write-number>x</button>', 'E4')


class ReverseCriteriaTest(unittest.TestCase):
    """反向判据：白名单属性必须被基座真的消费（按字面量，不按函数名存在）。"""

    def test_shaping_attributes_appear_in_write_base(self):
        base = BASE_JS.read_text(encoding='utf-8')
        for attr in ('data-write-json', 'data-write-rows', 'data-write-reload', 'data-write-status'):
            with self.subTest(attr=attr):
                self.assertIn(attr, base)

    def test_flag_consumers_read_the_attribute_inside_their_body(self):
        base = BASE_JS.read_text(encoding='utf-8')
        for fn, attr in (('itemsWanted', 'data-write-items'), ('latestMode', 'data-write-latest')):
            with self.subTest(fn=fn):
                m = re.search(r'function\s+' + re.escape(fn) + r'\s*\([^)]*\)\s*\{', base)
                self.assertIsNotNone(m, '基座里找不到函数 ' + fn)
                i, depth = m.end(), 1
                while i < len(base) and depth:
                    depth += {'{': 1, '}': -1}.get(base[i], 0)
                    i += 1
                body = base[m.end():i]
                self.assertIn(attr, body, fn + ' 的函数体没有消费 ' + attr)


class FullScriptTest(unittest.TestCase):
    """整脚本：真实模板目录 + 临时注入的夹具文件，断言退出码。

    judge 入口测的是判据；这一层测的是「模板遍历、行切分、计数传播」有没有断。
    夹具目录是 TPL_DIR 的临时副本（不写仓库），路径形如 tmproot/internal/templates/admin/…，
    与被测脚本对文件路径的处理一致。
    """

    def run_on(self, body: str):
        with tempfile.TemporaryDirectory() as tmp:
            shutil.copytree(ROOT / 'internal', Path(tmp) / 'internal')
            scripts_dir = Path(tmp) / 'scripts'
            scripts_dir.mkdir()
            shutil.copy2(GATE, scripts_dir / 'check-write-attributes.sh')
            fixture = Path(tmp) / 'internal/templates/admin/zz_gate_fixture.html'
            fixture.write_text(body, encoding='utf-8')
            return subprocess.run(
                ['bash', str(scripts_dir / 'check-write-attributes.sh'), 'zz_gate_fixture'],
                text=True, capture_output=True,
            )

    def test_real_templates_pass(self):
        r = subprocess.run(['bash', str(GATE)], text=True, capture_output=True, cwd=str(ROOT))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_misspelled_attribute_after_quoted_gt_fails_the_whole_run(self):
        r = self.run_on(
            '<form data-write="create" data-write-url="/api/x" '
            'data-write-confirm="a > b" data-write-item>\n'
        )
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('data-write-item', r.stderr)

    def test_legal_fixture_passes_the_whole_run(self):
        r = self.run_on(
            '<form data-write="bulk" data-write-url="/api/x" '
            'data-write-confirm="a > b" data-write-items data-write-latest>\n'
        )
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_button_inheriting_form_mode_passes(self):
        r = self.run_on(
            '<form data-write="update" data-write-url="/api/x">\n'
            '  <button type="submit" data-write-items>提交</button>\n'
            '</form>\n'
        )
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_form_with_mode_and_url_button_items_with_value_fails(self):
        # 表单已有 mode/url，按钮只带 items 带值：按钮无需自带 url，仍必须变红。
        r = self.run_on(
            '<form data-write="update" data-write-url="/api/x">\n'
            '  <button type="submit" data-write-items="1">提交</button>\n'
            '</form>\n'
        )
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('data-write-items', r.stderr)

    def test_form_with_mode_and_url_button_misspelled_flag_fails(self):
        r = self.run_on(
            '<form data-write="update" data-write-url="/api/x">\n'
            '  <button type="submit" data-write-item>提交</button>\n'
            '</form>\n'
        )
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('data-write-item', r.stderr)

    def test_containing_form_with_own_url_button_without_own_url_attributes_fails(self):
        # 真实上下文：外层 form 自带 mode/url，button 不写任何自己的 url/mode。
        # 按钮那行照样进形态阶段：干净按钮整页绿，带值开关整页红（不靠表单救它）。
        ok = self.run_on(
            '<form data-write="update" data-write-url="/api/x">\n'
            '  <button type="submit">保存</button>\n'
            '</form>\n'
        )
        self.assertEqual(ok.returncode, 0, ok.stdout + ok.stderr)
        bad = self.run_on(
            '<form data-write="update" data-write-url="/api/x">\n'
            '  <button type="submit" data-write-items="1">保存</button>\n'
            '</form>\n'
        )
        self.assertNotEqual(bad.returncode, 0, bad.stdout + bad.stderr)
        self.assertIn('data-write-items', bad.stderr)

    def test_standalone_button_bare_flag_is_judged(self):
        # 独立按钮（不靠表单 url）：合法裸 flag 绿，拼错裸 flag 红。
        ok = self.run_on('<button type="submit" data-write-items>提交</button>\n')
        self.assertEqual(ok.returncode, 0, ok.stdout + ok.stderr)
        bad = self.run_on('<button type="submit" data-write-item>提交</button>\n')
        self.assertNotEqual(bad.returncode, 0, bad.stdout + bad.stderr)
        self.assertIn('data-write-item', bad.stderr)

    def test_button_inherits_form_without_own_url_no_duplicate_requirement(self):
        # 表单继承形态不重复要求按钮 own URL：按钮不写 url 不得产生 E3b。
        r = self.run_on(
            '<form data-write="delete" data-write-url="/api/x">\n'
            '  <button type="submit" data-write-latest data-write-items>删除</button>\n'
            '</form>\n'
        )
        self.assertEqual(r.returncode, 0, r.stderr)


if __name__ == '__main__':
    unittest.main()
