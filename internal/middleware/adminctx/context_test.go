package adminctx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
)

// authzStub 是权限上下文服务的可控桩：三段查询各自可注失败，并记录被调用次数与入参。
// 调用计数是本文件的主判据 —— 失败路径上「下游一次都不该被调」只能靠计数证明。
type authzStub struct {
	codes []string
	aerr  error

	buttonList []string
	berr       error

	tree []admindto.MenuTreeNode
	terr error

	effectiveCalls  int
	buttonCalls     int
	treeCalls       int
	lastButtonCodes []string
}

func (s *authzStub) IsSuperAdmin(context.Context, uint64) (bool, error) { return false, nil }
func (s *authzStub) GetRoleCodesByUserID(context.Context, uint64) ([]string, error) {
	return nil, nil
}
func (s *authzStub) GetPermissionCodesByIDs(context.Context, []uint64) ([]string, error) {
	return nil, nil
}
func (s *authzStub) ListByCodes(context.Context, []string) ([]admindto.PermBrief, error) {
	return nil, nil
}
func (s *authzStub) ExistsEnabledCode(context.Context, string) (bool, error) { return false, nil }

func (s *authzStub) EffectivePermissionCodes(context.Context, uint64) ([]string, error) {
	s.effectiveCalls++
	return s.codes, s.aerr
}

func (s *authzStub) AuthorizedButtonCodes(_ context.Context, codes []string) ([]string, error) {
	s.buttonCalls++
	s.lastButtonCodes = codes
	return s.buttonList, s.berr
}

func (s *authzStub) BuildAuthorizedTree(_ context.Context, codes []string) ([]admindto.MenuTreeNode, error) {
	s.treeCalls++
	return s.tree, s.terr
}

// servePermContext 用真实 gin 链跑一遍中间件（before 用来预置会话态），返回到达 handler 时
// 捕获的上下文。handler 没被到达（例如中间件漏了 c.Next()）即红 —— 那是整页空白的故障。
func servePermContext(t *testing.T, authz admincontract.AuthzContextService, before gin.HandlerFunc) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var captured *gin.Context
	r := gin.New()
	if before != nil {
		r.Use(before)
	}
	r.Use(PermContextMiddleware(authz))
	r.GET("/admin/pages", func(c *gin.Context) {
		captured = c
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if captured == nil {
		t.Fatal("handler 未被到达：权限上下文中间件没有 c.Next()（整页会空白）")
	}
	return captured
}

// withUser 预置会话态：user_id 是中间件唯一认的键（类型必须是 int64）。
func withUser(uid any) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Next()
	}
}

func permSetOf(t *testing.T, c *gin.Context) map[string]bool {
	t.Helper()
	v, ok := c.Get(PermSetKey)
	if !ok {
		t.Fatal("上下文里没有 perm_set 键（Prepare 注入的 PermSet 会恒为空 map，按钮与菜单全消失）")
	}
	m, ok := v.(map[string]bool)
	if !ok {
		t.Fatalf("perm_set 的类型不是 map[string]bool：%T（Prepare 的类型断言会失败）", v)
	}
	return m
}

func buttonsOf(t *testing.T, c *gin.Context) map[string]bool {
	t.Helper()
	v, ok := c.Get(ButtonsKey)
	if !ok {
		t.Fatal("上下文里没有 button_set 键（Prepare 注入的 Buttons 会恒为空 map）")
	}
	m, ok := v.(map[string]bool)
	if !ok {
		t.Fatalf("button_set 的类型不是 map[string]bool：%T", v)
	}
	return m
}

// TestContextKeysAreWireContract 钉住三个键的字面量：键名是写入侧（本包）与读取侧
// （adminui.Prepare 与模块 handler）之间的线协议；改值会让请求上下文失联。
func TestContextKeysAreWireContract(t *testing.T) {
	if PermSetKey != "perm_set" || NavTreeKey != "nav_tree" || ButtonsKey != "button_set" {
		t.Fatalf("上下文键漂移：perm_set=%q nav_tree=%q button_set=%q", PermSetKey, NavTreeKey, ButtonsKey)
	}
}

// TestMiddlewareWritesBothSetsAndTree 成功路径：两集合按桩返回填充，树写进上下文，
// 且按钮查询收到的是**同一批**权限码（同源）。
func TestMiddlewareWritesBothSetsAndTree(t *testing.T) {
	tree := []admindto.MenuTreeNode{{ID: 1, Type: 1, Title: "内容"}}
	st := &authzStub{codes: []string{"page:view"}, buttonList: []string{"page.edit"}, tree: tree}

	c := servePermContext(t, st, withUser(int64(7)))

	set := permSetOf(t, c)
	if len(set) != 1 || !set["page:view"] {
		t.Fatalf("权限集与桩返回不一致：%#v", set)
	}
	buttons := buttonsOf(t, c)
	if len(buttons) != 1 || !buttons["page.edit"] {
		t.Fatalf("按钮集与桩返回不一致：%#v", buttons)
	}
	got, ok := c.Get(NavTreeKey)
	if !ok {
		t.Fatal("授权树没写进上下文（侧栏会整块为空）")
	}
	if tree2, ok := got.([]admindto.MenuTreeNode); !ok || len(tree2) != 1 || tree2[0].Title != "内容" {
		t.Fatalf("上下文里的授权树与桩返回不一致：%#v", got)
	}
	if st.effectiveCalls != 1 || st.buttonCalls != 1 || st.treeCalls != 1 {
		t.Fatalf("成功路径应各查一次，实际 effective=%d button=%d tree=%d", st.effectiveCalls, st.buttonCalls, st.treeCalls)
	}
	if len(st.lastButtonCodes) != 1 || st.lastButtonCodes[0] != "page:view" {
		t.Fatalf("按钮查询没收到同一批权限码：%#v", st.lastButtonCodes)
	}
}

// TestMiddlewareFailClosedOnPermissionQueryError 本批有意变更的回归判据：
// 服务返回「非空 codes + err」时，权限集与按钮集必须为空、授权树不得注入，
// **且按钮查询与建树一次都不能发生**（旧实现在这里照用 codes 并继续下游查询）。
func TestMiddlewareFailClosedOnPermissionQueryError(t *testing.T) {
	st := &authzStub{
		codes:      []string{"*:*", "page:view"}, // 诱饵：服务「既报错又给码」
		aerr:       errors.New("权限上下文查询失败（桩）"),
		buttonList: []string{"page.edit"},
		tree:       []admindto.MenuTreeNode{{ID: 1, Type: 1, Title: "内容"}},
	}

	c := servePermContext(t, st, withUser(int64(7)))

	set := permSetOf(t, c)
	if len(set) != 0 {
		t.Fatalf("查询失败时权限集必须为空，实际 %#v（fail-open：日志说空集、行为却照授权）", set)
	}
	if buttons := buttonsOf(t, c); len(buttons) != 0 {
		t.Fatalf("查询失败时按钮集必须为空，实际 %#v", buttons)
	}
	if v, ok := c.Get(NavTreeKey); ok {
		t.Fatalf("查询失败时不得注入授权树，实际 %#v", v)
	}
	if st.buttonCalls != 0 || st.treeCalls != 0 {
		t.Fatalf("查询失败后按钮查询与建树都不该被调用，实际 button=%d tree=%d（丢弃 codes 必须连带跳过下游）", st.buttonCalls, st.treeCalls)
	}
}

// TestMiddlewareWithoutUserLeavesSetsEmpty 没有可信用户身份时：三集合空、树不注入、
// 一次查询都不发生（未登录页面照常渲染，只是菜单与按钮消失）。
func TestMiddlewareWithoutUserLeavesSetsEmpty(t *testing.T) {
	cases := []struct {
		name   string
		before gin.HandlerFunc
	}{
		{"无 user_id", func(c *gin.Context) { c.Next() }},
		{"user_id 类型不符", withUser("7")},
		{"user_id 为 0", withUser(int64(0))},
		{"user_id 为负数", withUser(int64(-1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &authzStub{codes: []string{"page:view"}}
			c := servePermContext(t, st, tc.before)
			if set := permSetOf(t, c); len(set) != 0 {
				t.Fatalf("权限集必须为空，实际 %#v", set)
			}
			if buttons := buttonsOf(t, c); len(buttons) != 0 {
				t.Fatalf("按钮集必须为空，实际 %#v", buttons)
			}
			if v, ok := c.Get(NavTreeKey); ok {
				t.Fatalf("不得注入授权树，实际 %#v", v)
			}
			if st.effectiveCalls != 0 || st.buttonCalls != 0 || st.treeCalls != 0 {
				t.Fatalf("无可信身份时不该查任何东西，实际 effective=%d button=%d tree=%d", st.effectiveCalls, st.buttonCalls, st.treeCalls)
			}
		})
	}
}

// TestMiddlewareNilAuthzDoesNotPanic 装配缺失（authz 为 nil）时不 panic：两个键仍写入空 map。
func TestMiddlewareNilAuthzDoesNotPanic(t *testing.T) {
	c := servePermContext(t, nil, withUser(int64(7)))
	if set := permSetOf(t, c); len(set) != 0 {
		t.Fatalf("权限集必须为空，实际 %#v", set)
	}
	if buttons := buttonsOf(t, c); len(buttons) != 0 {
		t.Fatalf("按钮集必须为空，实际 %#v", buttons)
	}
	if v, ok := c.Get(NavTreeKey); ok {
		t.Fatalf("不得注入授权树，实际 %#v", v)
	}
}

// TestMiddlewarePartialFailuresKeepTheOtherHalves：按钮查询失败 → 只按钮全隐（权限集与树照旧）；
// 建树失败 → 只树不注入（权限集与按钮照旧）。两条现状语义各钉一颗钉子。
func TestMiddlewarePartialFailuresKeepTheOtherHalves(t *testing.T) {
	t.Run("按钮码查询失败", func(t *testing.T) {
		st := &authzStub{
			codes: []string{"page:view"},
			berr:  errors.New("按钮码查询失败（桩）"),
			tree:  []admindto.MenuTreeNode{{ID: 1, Type: 1, Title: "内容"}},
		}
		c := servePermContext(t, st, withUser(int64(7)))
		if set := permSetOf(t, c); !set["page:view"] {
			t.Fatalf("按钮失败不该影响权限集：%#v", set)
		}
		if buttons := buttonsOf(t, c); len(buttons) != 0 {
			t.Fatalf("按钮失败时按钮集必须为空，实际 %#v", buttons)
		}
		if _, ok := c.Get(NavTreeKey); !ok {
			t.Fatal("按钮失败不该影响侧栏树")
		}
		if st.treeCalls != 1 {
			t.Fatalf("按钮失败后建树应照常发生，实际 tree=%d", st.treeCalls)
		}
	})

	t.Run("建树失败", func(t *testing.T) {
		st := &authzStub{
			codes:      []string{"page:view"},
			buttonList: []string{"page.edit"},
			terr:       errors.New("导航菜单树构建失败（桩）"),
		}
		c := servePermContext(t, st, withUser(int64(7)))
		if set := permSetOf(t, c); !set["page:view"] {
			t.Fatalf("建树失败不该影响权限集：%#v", set)
		}
		if buttons := buttonsOf(t, c); !buttons["page.edit"] {
			t.Fatalf("建树失败不该影响按钮集：%#v", buttons)
		}
		if v, ok := c.Get(NavTreeKey); ok {
			t.Fatalf("建树失败时不得注入授权树，实际 %#v", v)
		}
	})
}
