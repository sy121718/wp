#!/usr/bin/env python3
"""baseline-adopt.sh 的核验放行回归测试（不连数据库）。

评审 2026-11-15 用替身探针证实的缺陷：必需键核验的两次 SQL 都失败时，脚本仍报
「键全部存在」并以 0 退出 —— 因为函数调用处写了 || true、失败分支的 out 是空串。
这里用假 psql/pg_dump/go 复现同一条路径，锁住三种结果：
  1. 键查询 SQL 失败 → 拒绝接入，且不得出现「全部存在」；
  2. 确有缺键 → 拒绝接入；
  3. 一切正常 → 核验通过（exit 0）。
另外覆盖系统数据行数查询失败：必须报错，而不是被当成「0 行」误判。
"""
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "baseline-adopt.sh"

FAKE_PSQL = """#!/usr/bin/env bash
# 假 psql：按参数形状分派，绝不连库。
printf '%s\\n' "psql $*" >> "${FAKE_LOG:?}"
args=" $* "
case "$args" in
  *" -q "*)
    case "${FAKE_KEYQUERY_MODE:-ok}" in
      fail) echo "psql: could not create temporary table" >&2; exit 4 ;;
      missing) printf 'admin.missing.key|en\\nadmin.other.key|zh\\n'; exit 0 ;;
      *) exit 0 ;;
    esac ;;
  *"COPY (SELECT"*)
    case "${FAKE_KEYCOPY_MODE:-ok}" in
      fail) echo "psql: COPY failed" >&2; exit 5 ;;
      *) printf 'a\\tb\\nc\\td\\n'; exit 0 ;;
    esac ;;
  *"count(*) FROM sys_baseline"*)
    echo 'ERROR: relation "sys_baseline" does not exist' >&2; exit 1 ;;
  *"count(*) FROM public."*)
    case "${FAKE_COUNT_MODE:-ok}" in
      fail) echo "psql: connection lost" >&2; exit 6 ;;
      *) echo 100000; exit 0 ;;
    esac ;;
  *"SELECT 1"*) echo 1; exit 0 ;;
  *"CREATE DATABASE"*|*"DROP DATABASE"*) exit 0 ;;
esac
exit 0
"""

FAKE_PG_DUMP = """#!/usr/bin/env bash
# 假 pg_dump：重放库与目标库输出同样的结构，让 diff 通过。
cat <<'SQL'
CREATE TABLE public.sys_i18n (
    id bigint NOT NULL,
    item_key text NOT NULL
);
SQL
"""

FAKE_GO = """#!/usr/bin/env bash
if [ "$1" = "run" ] && [ "$2" = "./cmd/baseline-digest" ]; then
  echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  exit 0
fi
if [ "$1" = "run" ]; then
  exit 0
fi
exit 0
"""


class AdoptGuardTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        bindir = Path(self.tmp.name) / "bin"
        bindir.mkdir()
        for name, body in (("psql", FAKE_PSQL), ("pg_dump", FAKE_PG_DUMP), ("go", FAKE_GO)):
            path = bindir / name
            path.write_text(body, encoding="utf-8")
            path.chmod(path.stat().st_mode | stat.S_IEXEC | stat.S_IXGRP | stat.S_IXOTH)
        self.bindir = bindir

    def tearDown(self):
        self.tmp.cleanup()

    def run_script(self, **env_extra):
        env = dict(os.environ)
        env["PATH"] = str(self.bindir) + os.pathsep + env.get("PATH", "")
        env["FAKE_LOG"] = str(Path(self.tmp.name) / "psql.log")
        # 本机 BASH_ENV 会把 Homebrew 目录插到 PATH 最前面，替身 psql 就永远排第二。
        # 置空它，替身才是脚本看到的第一个 psql。
        env["BASH_ENV"] = os.devnull
        env.update(env_extra)
        return subprocess.run(
            ["bash", str(SCRIPT), "--db", "fake_db"],
            cwd=str(ROOT), env=env, capture_output=True, text=True, timeout=120,
        )

    def test_key_query_sql_error_is_not_reported_as_pass(self):
        res = self.run_script(FAKE_KEYQUERY_MODE="fail")
        self.assertNotEqual(res.returncode, 0, "SQL 失败必须拒绝接入")
        combined = res.stdout + res.stderr
        self.assertIn("SQL 错误", combined)
        self.assertNotIn("全部存在", combined, "SQL 失败时不得报告键齐全")
        self.assertNotIn("核验通过", combined)

    def test_key_copy_sql_error_is_not_reported_as_pass(self):
        res = self.run_script(FAKE_KEYCOPY_MODE="fail")
        self.assertNotEqual(res.returncode, 0)
        combined = res.stdout + res.stderr
        self.assertIn("SQL 错误", combined)
        self.assertNotIn("全部存在", combined)

    def test_missing_keys_still_rejected(self):
        res = self.run_script(FAKE_KEYQUERY_MODE="missing")
        self.assertNotEqual(res.returncode, 0)
        combined = res.stdout + res.stderr
        self.assertIn("缺少基线里的键", combined)
        self.assertNotIn("核验通过", combined)

    def test_row_count_query_error_is_not_treated_as_zero(self):
        res = self.run_script(FAKE_COUNT_MODE="fail")
        self.assertNotEqual(res.returncode, 0)
        combined = res.stdout + res.stderr
        self.assertIn("统计", combined)
        self.assertIn("核验未成立", combined)

    def test_happy_path_still_passes(self):
        res = self.run_script()
        combined = res.stdout + res.stderr
        self.assertEqual(res.returncode, 0, combined)
        self.assertIn("核验通过", res.stdout)
        self.assertNotIn("写入水位", res.stdout, "不带 --apply 不得写库")


if __name__ == "__main__":
    unittest.main()
