package support

import (
	"os"
	"testing"
)

func RequirePG() bool {
	return os.Getenv("GOWP_REQUIRE_PG") == "1"
}

func RequireRedis() bool {
	return os.Getenv("GOWP_REQUIRE_REDIS") == "1"
}

func FailIfRequiredPG(t *testing.T, err error) {
	t.Helper()
	failIfRequired(t, RequirePG(), "PostgreSQL", "GOWP_REQUIRE_PG", err)
}

func FailIfRequiredRedis(t *testing.T, err error) {
	t.Helper()
	failIfRequired(t, RequireRedis(), "Redis", "GOWP_REQUIRE_REDIS", err)
}

func failIfRequired(t *testing.T, required bool, dep, envKey string, err error) {
	t.Helper()
	if err == nil || !required {
		return
	}
	t.Fatalf("%s 不可用，而 %s=1 要求本次测试必须真跑"+
		"（否则失败会被伪装成「跳过」）：%v", dep, envKey, err)
}
