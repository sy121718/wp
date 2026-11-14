package routers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/middleware"
	"go_wp/pkg/view"
)

// 闸门在装配落定前默认拒绝：任何业务请求都不能穿过一个还没核对完的实例。
func TestStartupGateDeniesBeforeSettle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gate := &startupGate{}
	r.Use(gate.middleware())
	r.GET("/api/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("装配未落定前业务请求应被拒绝，实际 %d", rec.Code)
	}
}

// 装配通过后才放行；不一致时保持拒绝，且不因探针请求改变状态。
func TestStartupGateSettlesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		ok   bool
		want int
	}{{true, http.StatusOK}, {false, http.StatusServiceUnavailable}}
	for _, tc := range cases {
		r := gin.New()
		gate := &startupGate{}
		r.Use(gate.middleware())
		r.GET("/api/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
		gate.settle(tc.ok)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
		if rec.Code != tc.want {
			t.Fatalf("settle(ok=%v) 后请求状态应为 %d，实际 %d", tc.ok, tc.want, rec.Code)
		}
	}
}

// 探针端点始终可达：否则外部无法读取 not_ready 的原因。
func TestStartupGateAlwaysServesProbes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gate := &startupGate{}
	r.Use(gate.middleware())
	r.GET("/readyz", func(c *gin.Context) { c.String(http.StatusServiceUnavailable, "not_ready") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not_ready") {
		t.Fatalf("探针不能被闸门拦截，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// 闸门 503 必须遵守路由声明的协议：HTML 片段走共享失败出口拿错误区，
// 数据接口才拿 JSON。不能对浏览器页面回一段 JSON。
func TestStartupGateHonorsDeclaredProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		marker   gin.HandlerFunc
		wantCT   string
		wantJSON bool
	}{
		{name: "html fragment", marker: view.HTMLFragment, wantCT: "text/html"},
		{name: "plain data endpoint", marker: func(c *gin.Context) { c.Next() }, wantCT: "application/json", wantJSON: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(view.FailureMiddleware(func(c *gin.Context, status int, _ string, _ error) {
				c.Data(status, "text/html; charset=utf-8", []byte("<div data-ui-error>未就绪</div>"))
			}))
			gate := &startupGate{}
			r.Use(gate.middleware())
			r.POST("/save", tc.marker, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/save", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("未就绪实例必须拒绝业务请求，实际 %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tc.wantCT) {
				t.Fatalf("内容类型应为 %s，实际 %q body=%s", tc.wantCT, ct, rec.Body.String())
			}
			if tc.wantJSON && strings.Contains(rec.Body.String(), "data-ui-error") {
				t.Fatalf("数据接口不应拿到 HTML 错误区：%s", rec.Body.String())
			}
			if !tc.wantJSON && !strings.Contains(rec.Body.String(), "data-ui-error") {
				t.Fatalf("HTML 页面应拿到共享失败出口的错误区，实际 %s", rec.Body.String())
			}
		})
	}
}

// 停止句柄归装配实例：两个实例各自停止自己的任务，互不影响；
// 重复 Stop 只执行一次，不会重复调用停止函数。
func TestAssemblyStopsAreInstanceScoped(t *testing.T) {
	var aStops, bStops int
	a := &Assembly{stops: []func(){func() { aStops++ }}}
	b := &Assembly{stops: []func(){func() { bStops++ }}}

	a.Stop()
	a.Stop()
	if aStops != 1 {
		t.Fatalf("同一实例重复 Stop 只应执行一次，实际 %d", aStops)
	}
	if bStops != 0 {
		t.Fatalf("停止 A 不得触发 B 的停止函数，实际 %d", bStops)
	}
	b.Stop()
	if bStops != 1 {
		t.Fatalf("B 的停止函数应执行一次，实际 %d", bStops)
	}
}

// 停止按逆序执行：后启动的先停，缩短任务之间的依赖窗口。
func TestAssemblyStopsReverseOrder(t *testing.T) {
	var order []int
	a := &Assembly{stops: []func(){
		func() { order = append(order, 1) },
		func() { order = append(order, 2) },
		func() { order = append(order, 3) },
	}}
	a.Stop()
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Fatalf("停止顺序应为逆序 3,2,1，实际 %v", order)
	}
}

// nil 句柄与 nil 停止函数都必须安全（装配早期失败或可选任务未启动）。
func TestAssemblyStopNilSafe(t *testing.T) {
	var a *Assembly
	a.Stop()
	(&Assembly{stops: []func(){nil}}).Stop()
}

// 「全部登记后再注入授权索引」的装配顺序：快照必须包含后注册的绑定，
// 冻结后的迟到注册明确报错，不能悄悄长目录。
func TestRouteBindingIndexSeesLateRegistrations(t *testing.T) {
	reg := middleware.NewRegistry()
	reg.DeclareRoute(middleware.RouteSpec{Method: "GET", Path: "/api/early", Perm: "early:read", Kind: middleware.RouteProtected})
	reg.DeclareRoute(middleware.RouteSpec{Method: "POST", Path: "/api/late", Perm: "late:write", Kind: middleware.RouteProtected})

	index := middleware.NewRegistry()
	for _, spec := range reg.Snapshot() {
		index.DeclareRoute(spec)
	}
	snap := index.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("授权索引应包含后注册的绑定，实际 %d 条：%+v", len(snap), snap)
	}

	index.Freeze()
	defer func() {
		if recover() == nil {
			t.Fatal("冻结后的迟到注册必须明确报错，而不是悄悄追加")
		}
	}()
	index.DeclareRoute(middleware.RouteSpec{Method: "POST", Path: "/api/after-freeze", Perm: "x:write", Kind: middleware.RouteProtected})
}
