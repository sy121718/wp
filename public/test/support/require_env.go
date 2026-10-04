package support

import (
	"os"
	"testing"
)

// require_env.go — 「依赖不可用」是跳过还是失败，由环境开关决定。
//
// 背景（审计 R-05）：测试基建历史上把「PG/Redis 不可用」一律当作 t.Skip —— 本地方便，
// CI 上却是**假绿**：集成 job 的 service 起不来、库名连错、迁移没跑成，全都表现成
// 「用例被跳过」，而 go test 的汇总照样是 ok。这一步的收口开关是 GOWP_REQUIRE_PG=1：
// 依赖不可用即 t.Fatalf，「跳过」与「通过」在 CI 里不能再混淆。
// 本地不置该变量时行为与历史完全一致（仍 Skip）—— 本地缺库是常态，不应该打断开发。

// RequirePG 报告本次测试是否要求 PostgreSQL 必须可用。
func RequirePG() bool {
	return os.Getenv("GOWP_REQUIRE_PG") == "1"
}

// RequireRedis 报告本次测试是否要求 Redis 必须可用。
func RequireRedis() bool {
	return os.Getenv("GOWP_REQUIRE_REDIS") == "1"
}

// FailIfRequiredPG 在 PG 不可用（err != nil）且 GOWP_REQUIRE_PG=1 时直接判失败。
//
// 调用点后面都紧跟一句 t.Skipf：严格模式下 t.Fatalf 会立刻终止该用例，
// 所以调用方的 Skip 分支不会被走到 —— 不必逐个改写那些 Skip 调用点。
func FailIfRequiredPG(t *testing.T, err error) {
	t.Helper()
	failIfRequired(t, RequirePG(), "PostgreSQL", "GOWP_REQUIRE_PG", err)
}

// FailIfRequiredRedis 与 FailIfRequiredPG 同形，判据是 GOWP_REQUIRE_REDIS=1。
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
