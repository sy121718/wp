package blockhttp

// block_router_permission_test.go — 守「一个权限点只绑一条 API 路径」这条线（免库、快速）。
//
// 背景（task-21 P1）：批量删除曾与单条删除共用 block:delete。本仓的权限模型里
// sys_permission 每个 code 只有**一列** api_path/api_method，启动期
// permission.SyncToDB 按路由声明对齐这两列 —— 两条 API 路径共用一个 code 时，
// 同步只能留下其中一条，另一条路径上的策略立刻失配（超管 403、角色静默失权），
// 而这一步在纯字符串断言、路由快照、权限门禁里全都看不见。
//
// 所以这里直接问**声明表**（permission.RoutesOf 读的就是装配期真实登记的那份）：
// 每条权限点声明了几条路径。真实库上的 SyncToDB + Enforce 交叉验证在
// internal/permission/block_bulk_delete_test.go。

import (
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/permission"
)

func TestBlockRoutesBindOneAPIPathPerPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	permission.Reset()
	t.Cleanup(permission.Reset)

	engine := gin.New()
	// db / projects 只参与运行时读写，不参与注册期声明 —— 这里传 nil 即可。
	// （如果哪天注册需要它们，这里会直接 panic，比悄悄少声明几条路由好。）
	SetupBlockRoutes(permission.NewRouteGroup(engine.Group("/api")), nil, nil)

	for _, tc := range []struct {
		perm permission.Perm
		want string
	}{
		{permission.BlockDelete, "POST /api/block/delete"},
		{permission.BlockBulkDelete, "POST /api/block/bulk-delete"},
	} {
		routes := permission.RoutesOf(tc.perm)
		if len(routes) != 1 {
			t.Errorf("%s 应恰好声明 1 条 API 路径，实际 %d 条 —— 共用 code 会让 SyncToDB 只能留下其中一条（另一条静默失权）",
				tc.perm, len(routes))
			continue
		}
		if got := routes[0].Method + " " + routes[0].Path; got != tc.want {
			t.Errorf("%s 应声明 %s，实际 %s", tc.perm, tc.want, got)
		}
	}

	// 反向：任何权限点都不许绑两条路径（上一条只覆盖当前已知的两个 code）。
	paths := map[permission.Perm][]string{}
	for _, spec := range permission.Snapshot() {
		paths[spec.Perm] = append(paths[spec.Perm], spec.Method+" "+spec.Path)
	}
	for perm, list := range paths {
		if len(list) > 1 {
			t.Errorf("权限点 %s 被声明在 %d 条路径上：%v —— 一个 code 只能对应一条 API 路径", perm, len(list), list)
		}
	}
}
