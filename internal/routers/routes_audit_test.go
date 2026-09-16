package routers

// routes_audit_test.go — dump 运行时路由表，供 scripts/check-permission-gaps.sh 比对权限点。
//
// 默认**跳过**（需要数据库与完整组件装配，不能让 go test ./... 每次都付这个代价）：
// 只有显式设置 WP_DUMP_ROUTES=1 才跑。
//
// 为什么要留这条路：authorizedAPI 组挂了 CasbinMiddleware()，它按**实际请求路径** enforce ——
// 权限点表里没有对应条目时，没有任何策略能匹配，含超管在内全员 403。
// 这个坑在 072/077/078/079 的迁移注释里各写过一遍。靠人记是记不住的，
// 所以把它变成一条可重复执行的审计命令（脚本从本测试的 stdout 取路由）。
//
// 装配与 dump 的实现在 routes_snapshot_test.go 的 bootstrapRouter（与快照测试共用）。

import (
	"fmt"
	"os"
	"testing"
)

func TestDumpRoutesForAudit(t *testing.T) {
	if os.Getenv("WP_DUMP_ROUTES") != "1" {
		t.Skip("设置 WP_DUMP_ROUTES=1 才运行（需要数据库与完整组件装配）")
	}
	for _, l := range bootstrapRouter(t) {
		fmt.Println(l)
	}
}
