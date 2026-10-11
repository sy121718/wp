package adminctx

// context_contract_test.go — 导出请求上下文键的写入与字面值契约。
// 自 shell 迁入写入方：用导出常量读取真实中间件写入的值，
// 与 prepare_e2e_test.go 的中间件 → Prepare 链共同守住三键。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
)

// contextKeyAuthzStub 只跑成功路径：三个键都得被写到。
type contextKeyAuthzStub struct{}

func (contextKeyAuthzStub) IsSuperAdmin(context.Context, uint64) (bool, error) { return false, nil }
func (contextKeyAuthzStub) GetRoleCodesByUserID(context.Context, uint64) ([]string, error) {
	return nil, nil
}
func (contextKeyAuthzStub) GetPermissionCodesByIDs(context.Context, []uint64) ([]string, error) {
	return nil, nil
}
func (contextKeyAuthzStub) ListByCodes(context.Context, []string) ([]admindto.PermBrief, error) {
	return nil, nil
}
func (contextKeyAuthzStub) ExistsEnabledCode(context.Context, string) (bool, error) {
	return false, nil
}
func (contextKeyAuthzStub) EffectivePermissionCodes(context.Context, uint64) ([]string, error) {
	return []string{"page:view"}, nil
}
func (contextKeyAuthzStub) AuthorizedButtonCodes(context.Context, []string) ([]string, error) {
	return []string{"page.edit"}, nil
}
func (contextKeyAuthzStub) BuildAuthorizedTree(context.Context, []string) ([]admindto.MenuTreeNode, error) {
	return []admindto.MenuTreeNode{{ID: 1, Type: 1, Title: "内容"}}, nil
}

func TestAdminctxMiddlewareWritesTheKeysThisPackageNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var captured *gin.Context
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(7))
		c.Next()
	})
	r.Use(PermContextMiddleware(contextKeyAuthzStub{}))
	r.GET("/admin/pages", func(c *gin.Context) {
		captured = c
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if captured == nil {
		t.Fatal("handler 未被到达：中间件没有放行")
	}

	v, ok := captured.Get(PermSetKey)
	if !ok {
		t.Fatalf("本包常量 %s=%q 读不到东西 —— 中间件写的键名与它分歧（页面权限集会恒为空 map）", "PermSetKey", PermSetKey)
	}
	if set, ok := v.(map[string]bool); !ok || !set["page:view"] {
		t.Fatalf("PermSetKey 下的值不是中间件写入的权限集：%#v", v)
	}
	bv, ok := captured.Get(ButtonsKey)
	if !ok {
		t.Fatalf("本包常量 %s=%q 读不到东西 —— 按钮码集合键名分歧（按钮会全部隐藏）", "ButtonsKey", ButtonsKey)
	}
	if buttons, ok := bv.(map[string]bool); !ok || !buttons["page.edit"] {
		t.Fatalf("ButtonsKey 下的值不是中间件写入的按钮集：%#v", bv)
	}
	tv, ok := captured.Get(NavTreeKey)
	if !ok {
		t.Fatalf("本包常量 %s=%q 读不到东西 —— 授权导航树键名分歧（侧栏会整块为空）", "NavTreeKey", NavTreeKey)
	}
	if tree, ok := tv.([]admindto.MenuTreeNode); !ok || len(tree) != 1 {
		t.Fatalf("NavTreeKey 下的值不是中间件写入的授权树：%#v", tv)
	}
}

// TestContextKeyLiterals 钉住三个导出常量的线协议值。
func TestContextKeyLiterals(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"PermSetKey", PermSetKey, "perm_set"},
		{"NavTreeKey", NavTreeKey, "nav_tree"},
		{"ButtonsKey", ButtonsKey, "button_set"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s 漂移：%q（期望 %q）—— 键名是跨包契约，改值等于让读取方静默失联", c.name, c.got, c.want)
		}
	}
}
