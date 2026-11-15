#!/usr/bin/env python3
"""接入脚本的 fail-closed 回归：键核验 SQL 失败时不得报「键全部存在」并以 0 退出。"""
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts' / 'schema-adopt.sh'


class SchemaAdoptGuardTest(unittest.TestCase):
    def run_adopt(self, keyquery_fails):
        with tempfile.TemporaryDirectory(prefix='wp-schema-adopt-guard-') as temp:
            bindir = Path(temp) / 'bin'
            bindir.mkdir()

            def fake(name, body):
                path = bindir / name
                path.write_text('#!/usr/bin/env bash\n' + textwrap.dedent(body), encoding='utf-8')
                path.chmod(path.stat().st_mode | stat.S_IXUSR)

            fake('psql', f"""
                args="$*"
                case "$args" in
                  *"COPY (SELECT"*)
                    if [ "{1 if keyquery_fails else 0}" = "1" ]; then
                      echo "ERROR: simulated key export failure" >&2
                      exit 1
                    fi
                    printf 'key1\\tzh-CN\\nkey1\\ten-US\\n'
                    exit 0 ;;
                  *"SELECT count(*) FROM sys_baseline"*) echo 0; exit 0 ;;
                  *"SELECT 1"*) echo 1; exit 0 ;;
                  *) exit 0 ;;
                esac
            """)
            fake('pg_dump', """
                echo 'CREATE TABLE public.sys_baseline (id bigint);'
            """)
            fake('go', """
                case "$*" in
                  *schema-digest*) echo 0000000000000000000000000000000000000000000000000000000000000000; echo schema-00000000 ;;
                esac
                exit 0
            """)

            env = {
                **os.environ,
                'PATH': str(bindir) + os.pathsep + os.environ.get('PATH', ''),
                # 本机 BASH_ENV 会把 Homebrew 路径插到 PATH 最前，替身就永远排第二。
                'BASH_ENV': os.devnull,
            }
            result = subprocess.run(
                ['bash', str(SCRIPT), '--db', 'probe'],
                env=env, capture_output=True, text=True, timeout=120,
            )
            return result.returncode, result.stdout + result.stderr

    def test_key_query_failure_is_fatal(self):
        code, output = self.run_adopt(keyquery_fails=True)
        self.assertNotEqual(code, 0, output)
        self.assertIn('SQL 错误', output)
        self.assertNotIn('键全部存在', output)

    def test_clean_probe_passes_without_writing(self):
        code, output = self.run_adopt(keyquery_fails=False)
        self.assertEqual(code, 0, output)
        self.assertIn('核验通过', output)


if __name__ == '__main__':
    unittest.main()
