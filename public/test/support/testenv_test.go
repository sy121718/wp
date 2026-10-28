package support

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPGAppendParamsFormats 钉住「拼接唯一入口」的两条分支。
//
// 回归对象：testenv.go 曾用 `pgDSN(...) + " connect_timeout=5"` 拼接，而 pgDSN 返回 URL
// 形式，pgx 因此在连库之前就报 extra key/value separator —— 严格模式（GOWP_REQUIRE_PG=1）
// 全部用例死在解析阶段，看起来像「环境不可用」，实际是一行 DSN 拼错。
func TestPGAppendParamsFormats(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		// pgDSN 的真实产物：URL 且已带查询串。
		{"pgDSN 产物", pgDSN("127.0.0.1", "5432", "probe", "a b@x", "probe")},
		{"URL 无查询串", "postgres://probe:pass@127.0.0.1:5432/probe"},
		{"URL 有查询串", "postgresql://probe:pass@127.0.0.1:5432/probe?application_name=probe"},
		{"关键字格式", "host=127.0.0.1 port=5432 user=probe password='a b@x' dbname=probe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := pgconn.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatalf("原始 DSN 无法解析: %v", err)
			}
			after, err := pgconn.ParseConfig(pgAppendParams(tc.dsn, [2]string{"connect_timeout", "5"}))
			if err != nil {
				t.Fatalf("追加后 DSN 无法解析: %v", err)
			}
			if after.ConnectTimeout != 5*time.Second {
				t.Fatalf("connect_timeout 未生效: %v", after.ConnectTimeout)
			}
			assertSameConnIdentity(t, before, after)
		})
	}
}

// TestPGAppendParamsPreservesExisting 追加不得覆盖或丢失原参数、也不得改变连接身份。
func TestPGAppendParamsPreservesExisting(t *testing.T) {
	dsn := pgDSN("127.0.0.1", "5432", "probe", "a b@x", "probe")
	before, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	after, err := pgconn.ParseConfig(pgAppendParams(dsn, [2]string{"search_path", "public,ext_shared"}))
	if err != nil {
		t.Fatalf("追加 search_path 后无法解析: %v", err)
	}
	if got := after.RuntimeParams["synchronous_commit"]; got != "off" {
		t.Fatalf("原有 synchronous_commit 丢失: %q", got)
	}
	if got := after.RuntimeParams["search_path"]; got != "public,ext_shared" {
		t.Fatalf("search_path 未生效: %q（逗号被当成多值分隔符就是这里出的事）", got)
	}
	assertSameConnIdentity(t, before, after)
}

// TestPGAppendParamsIgnoresUnrelatedKeys 关键字格式下，带空格/引号的值必须被正确转义。
func TestPGAppendParamsIgnoresUnrelatedKeys(t *testing.T) {
	dsn := pgAppendParams("host=127.0.0.1 port=5432 user=probe dbname=probe",
		[2]string{"application_name", "a b'c"})
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("含空格与引号的值把 DSN 弄坏了: %v", err)
	}
	if got := cfg.RuntimeParams["application_name"]; got != "a b'c" {
		t.Fatalf("值被改写: %q", got)
	}
}

func assertSameConnIdentity(t *testing.T, before, after *pgconn.Config) {
	t.Helper()
	if before.Host != after.Host || before.Port != after.Port ||
		before.User != after.User || before.Password != after.Password || before.Database != after.Database {
		t.Fatalf("连接身份发生变化: %+v → %+v", before, after)
	}
}

// 子进程隔离 Fatalf，确保依赖失败后的 Skipf 在严格模式不会执行。
func TestRequiredDependencyFailure(t *testing.T) {
	if dep := os.Getenv("GOWP_TEST_REQUIRED_CHILD"); dep != "" {
		err := errors.New("dependency-probe")
		if dep == "PG" {
			FailIfRequiredPG(t, err)
		} else {
			FailIfRequiredRedis(t, err)
		}
		t.Skipf("dependency-probe: %v", err)
	}
	for _, dep := range []string{"PG", "REDIS"} {
		for _, required := range []string{"0", "1"} {
			t.Run(dep+required, func(t *testing.T) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestRequiredDependencyFailure$", "-test.v")
				cmd.Env = append(os.Environ(), "GOWP_TEST_REQUIRED_CHILD="+dep, "GOWP_REQUIRE_"+dep+"="+required)
				out, err := cmd.CombinedOutput()
				if required == "1" {
					if err == nil || !strings.Contains(string(out), "--- FAIL:") || strings.Contains(string(out), "--- SKIP:") {
						t.Fatalf("严格模式应失败且无跳过: err=%v 输出=%s", err, out)
					}
				} else if err != nil || !strings.Contains(string(out), "--- SKIP:") {
					t.Fatalf("宽松模式应跳过: err=%v 输出=%s", err, out)
				}
			})
		}
	}
}
