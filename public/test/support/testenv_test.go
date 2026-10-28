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

func TestPGProbeDSNFormats(t *testing.T) {
	for _, dsn := range []string{
		pgDSN("127.0.0.1", "5432", "probe", "a b@x", "probe"),
		"postgres://probe:pass@127.0.0.1:5432/probe",
		"postgresql://probe:pass@127.0.0.1:5432/probe?application_name=probe",
		"host=127.0.0.1 port=5432 user=probe password='a b@x' dbname=probe synchronous_commit=off",
	} {
		t.Run(dsn, func(t *testing.T) {
			before, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			after, err := pgconn.ParseConfig(pgProbeDSN(dsn))
			if err != nil {
				t.Fatal(err)
			}
			if after.ConnectTimeout != 5*time.Second {
				t.Fatalf("超时=%v", after.ConnectTimeout)
			}
			if before.Host != after.Host || before.Port != after.Port || before.User != after.User || before.Password != after.Password || before.Database != after.Database {
				t.Fatal("连接身份发生变化")
			}
			for key, value := range before.RuntimeParams {
				if after.RuntimeParams[key] != value {
					t.Fatalf("运行参数 %s 丢失", key)
				}
			}
		})
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

// 用子进程隔离共享容器缓存；预置已就绪端点，只验证回退编排，不启动容器。
func TestRequiredTestEnvAcceptsContainerFallback(t *testing.T) {
	if os.Getenv("GOWP_TEST_CONTAINER_FALLBACK_CHILD") == "1" {
		t.Setenv("GOWP_REQUIRE_PG", "1")
		t.Setenv("GOWP_REQUIRE_REDIS", "1")
		t.Setenv("PGHOST", "127.0.0.1")
		t.Setenv("PGPORT", "1")
		t.Setenv("TEST_REDIS_ADDR", "127.0.0.1:1")
		sharedPGOnce.Do(func() {
			sharedPGEP = PGEndpoint{Host: "container-pg", Port: "5432", User: "probe", Database: "probe"}
		})
		sharedRedisOnce.Do(func() { sharedRedisAddr = "container-redis:6379" })
		TestRequiredTestEnv(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRequiredTestEnvAcceptsContainerFallback$", "-test.v")
	cmd.Env = append(os.Environ(), "GOWP_TEST_CONTAINER_FALLBACK_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("有效容器回退应通过严格依赖契约: %v\n%s", err, out)
	}
}

func TestRequiredTestEnv(t *testing.T) {
	if !RequirePG() || !RequireRedis() {
		t.Skip("仅严格双依赖回归运行")
	}
	env, err := AcquireTestEnv(t)
	if err != nil {
		t.Fatal(err)
	}
	if env == nil {
		t.Fatal("环境应存在")
	}
	if env.PG.Host == "" || env.PG.Port == "" || env.PG.User == "" || env.PG.Database == "" || env.RedisAddr == "" {
		t.Fatalf("依赖端点应完整: %+v", env)
	}
	for _, source := range []string{env.PGSource, env.RedisSource} {
		if source != "local" && source != "container" {
			t.Fatalf("依赖来源无效: %q", source)
		}
	}
}

func TestPingPGReachesConnectionAttempt(t *testing.T) {
	err := pingPG(PGEndpoint{Host: "127.0.0.1", Port: "1", User: "probe", Database: "probe"})
	if err == nil {
		t.Fatal("关闭端口应连接失败")
	}
	if strings.Contains(err.Error(), "failed to parse") || strings.Contains(err.Error(), "extra key/value separator") {
		t.Fatalf("探测必须进入连接阶段，而非 DSN 解析失败: %v", err)
	}
}
