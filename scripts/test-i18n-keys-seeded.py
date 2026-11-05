#!/usr/bin/env python3
"""验证短词条门禁会因真实缺词、拼接和单侧语言变红。"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('check-i18n-keys-seeded.sh')


class ShortKeyGateTest(unittest.TestCase):
    def run_gate(self, source, sql, registration=''):
        with tempfile.TemporaryDirectory(prefix='wp-short-key-gate-') as temp:
            root = Path(temp)
            (root / 'internal/templates').mkdir(parents=True)
            (root / 'public/migrations').mkdir(parents=True)
            (root / 'internal/templates/probe.html').write_text(source, encoding='utf-8')
            (root / 'public/migrations/001_probe.sql').write_text(sql, encoding='utf-8')
            (root / 'public/migrations/register_probe.go').write_text(registration, encoding='utf-8')
            (root / 'baseline').write_text('0\n', encoding='utf-8')
            result = subprocess.run(['bash', str(SCRIPT)], env={
                **os.environ, 'GOWP_ROOT': str(root),
                'I18N_KEYS_BASELINE': str(root / 'baseline'),
                'I18N_KEYS_LIST': str(root / 'missing'),
                'I18N_KEYS_UNPAIRED': str(root / 'unpaired'),
            }, capture_output=True, text=True, timeout=10)
            return result.returncode, result.stdout + result.stderr

    def test_short_key_without_seed_is_rejected(self):
        code, output = self.run_gate('{{t("shortMissing", "缺词")}}', '')
        self.assertNotEqual(code, 0, output)
        self.assertIn('shortMissing', output)

    def test_comment_and_condition_are_not_seed(self):
        code, output = self.run_gate('{{t("onlyInComment", "缺词")}}',
                                     '-- onlyInComment\n',
                                     'ConditionSQL: "onlyInComment"')
        self.assertNotEqual(code, 0, output)

    def test_single_language_is_rejected(self):
        code, output = self.run_gate("{{tn('oneSide', '单侧', map())}}",
                                     "INSERT INTO sys_i18n (item_key, lang, item_value) VALUES ('oneSide', 'zh-CN', '单侧');")
        self.assertNotEqual(code, 0, output)
        self.assertIn('oneSide', output)

    def test_both_languages_and_literal_quotes(self):
        for quote in ('"', "'"):
            with self.subTest(quote=quote):
                code, output = self.run_gate('{{.[' + quote + 't' + quote + '](' + quote + 'shortReady' + quote + ', "可用")}}',
                                             "INSERT INTO sys_i18n (item_key, lang, item_value) VALUES ('shortReady', 'zh-CN', '可用'), ('shortReady', 'en-US', 'Ready');")
                self.assertEqual(code, 0, output)

    def test_insert_select_values_is_seed_but_mapping_is_not(self):
        sql = """WITH keys(old_key, new_key, retire) AS (VALUES ('oldKey', 'newKey', true))
INSERT INTO sys_i18n (item_key, lang, item_value)
SELECT d.item_key, d.lang, d.item_value FROM (VALUES
('newKey', 'zh-CN', '新词'), ('newKey', 'en-US', 'New word')
) AS d(item_key, lang, item_value);"""
        code, output = self.run_gate('{{t("newKey", "新词")}}', sql)
        self.assertEqual(code, 0, output)
        code, output = self.run_gate('{{t("oldKey", "旧词")}}', sql)
        self.assertNotEqual(code, 0, output)

    def test_dynamic_concatenation_is_rejected(self):
        code, output = self.run_gate('{{t("short" + "Tail", "拼接")}}', '')
        self.assertNotEqual(code, 0, output)
        self.assertIn('动态拼接', output)

    def test_commented_out_call_and_non_translation_literal_are_ignored(self):
        code, output = self.run_gate('{* {{t("unusedKey", "注释")}} *}<div data-id="notAWord"></div>', '')
        self.assertEqual(code, 0, output)

    def test_local_translation_alias_cannot_hide_missing_short_key(self):
        for source in (
            '{{gfTr := .["t"]}}{{gfTr("aliasMissing", "缺词")}}',
            "{{vfTr := .['t']}}{{local := vfTr}}{{local('aliasMissing', '缺词')}}",
        ):
            with self.subTest(source=source):
                code, output = self.run_gate(source, '')
                self.assertNotEqual(code, 0, output)
                self.assertIn('aliasMissing', output)

    def test_local_translation_alias_cannot_hide_dynamic_concatenation(self):
        code, output = self.run_gate('{{gfTr := .["t"]}}{{gfTr("short" + suffix, "拼接")}}', '')
        self.assertNotEqual(code, 0, output)
        self.assertIn('动态拼接', output)

    def test_local_translation_alias_with_both_languages_is_valid(self):
        code, output = self.run_gate('{{tr := .["t"]}}{{gfTr := tr}}{{gfTr("aliasReady", "可用")}}',
                                     "INSERT INTO sys_i18n (item_key, lang, item_value) VALUES ('aliasReady', 'zh-CN', '可用'), ('aliasReady', 'en-US', 'Ready');")
        self.assertEqual(code, 0, output)

    def test_non_translation_alias_is_ignored(self):
        code, output = self.run_gate('{{format := .["format"]}}{{format("notAWord")}}', '')
        self.assertEqual(code, 0, output)

    def test_legacy_missing_key_still_fails(self):
        code, output = self.run_gate('{{t("admin.probe.missing", "旧形状缺词")}}', '')
        self.assertNotEqual(code, 0, output)


if __name__ == '__main__':
    unittest.main()
