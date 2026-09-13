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

import (
	"fmt"
	"os"
	"sort"
	"testing"

	"go_wp/config"

	"github.com/gin-gonic/gin"
)

func TestDumpRoutesForAudit(t *testing.T) {
	if os.Getenv("WP_DUMP_ROUTES") != "1" {
		t.Skip("设置 WP_DUMP_ROUTES=1 才运行（需要数据库与完整组件装配）")
	}
	if err := config.Init("../../config.yaml"); err != nil {
		t.Fatalf("配置加载失败: %v", err)
	}
	if _, err := config.GetServer(); err != nil {
		t.Fatalf("server 配置解析失败: %v", err)
	}
	if err := config.InitComponents(); err != nil {
		t.Fatalf("组件初始化失败: %v", err)
	}
	defer func() { _ = config.CloseComponents() }()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetupRoutes(router, func() error { return nil })

	lines := make([]string, 0, 256)
	for _, r := range router.Routes() {
		lines = append(lines, r.Method+" "+r.Path)
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
}
