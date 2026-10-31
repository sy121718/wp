package blockhttp

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
