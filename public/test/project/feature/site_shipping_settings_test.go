package feature

// site_shipping_settings_test.go — 站点级运费设置的**端到端**链路
// （gin 路由 → 站点设置页 handler → project 契约 → 真实 PostgreSQL → Jet 模板 → 运费端口读回）。
//
// 为什么值得一条 feature：本批的失效模式几乎全部是「不报错的静默错」——
//
//	· 元/分换算方向写反 → 运费变成 100 倍或 1/100（页面照常）；
//	· 保存与读取的键名 / 判据分叉 → 后台存了、结算按 0 走（两边各自的单测都绿）；
//	· 模板字段名与 handler 读的表单名不一致 → 保存 200、那个字段永远是空。
//
// 这三种只有「保存 → 读库 → 读回 → 渲染」走一遍才暴露。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// newSiteShippingEnv 装配站点设置页的真实链路环境（真实 PG + project 契约 + Jet 模板）。
func newSiteShippingEnv(t *testing.T) (*gin.Engine, *projectservice.Service, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "运费测试站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	handle := projecthttp.NewSiteSettingsAdminHandle(projects, nil, nil)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/settings", handle.SiteSettings)
	router.POST("/admin/settings/save", handle.SaveSiteSettings)
	return router, projects, project.ID
}

// submitShippingSettings 提交站点设置页表单。
func submitShippingSettings(t *testing.T, router *gin.Engine, projectID string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"projectId": {projectID}, "name": {"运费测试站点"}}
	for k, v := range extra {
		form.Set(k, v)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(rec, req)
	return rec
}

// TestSiteSettingsShippingSaveAndReadBack 保存 → 读库 → 回显 → 运费端口读回，四步一致。
//
// 金额口径的完整闭环：表单填「8.00 元 / 100 元」→ 库内 800 / 10000 分 →
// 运费端口读回 800 / 10000 分 → 页面回显 8.00 / 100.00 元。
func TestSiteSettingsShippingSaveAndReadBack(t *testing.T) {
	router, projects, projectID := newSiteShippingEnv(t)
	ctx := context.Background()

	rec := submitShippingSettings(t, router, projectID, map[string]string{
		"shippingBaseFee":       "8.00",
		"shippingFreeThreshold": "100",
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("保存应 303 回跳，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}

	// ① 库内是**分**（与 orders 同口径）。
	detail, err := projects.Detail(ctx, &projectdto.DetailReq{ID: projectID})
	if err != nil {
		t.Fatalf("读工程失败: %v", err)
	}
	fields := projectdto.ParseSiteSettings(detail.Settings)
	if fields.ShippingBaseFee != 800 || fields.ShippingFreeThreshold != 10000 {
		t.Fatalf("库内应为 800 / 10000 分，实际 %d / %d",
			fields.ShippingBaseFee, fields.ShippingFreeThreshold)
	}

	// ② 运费端口（结算侧唯一的读取口）读出同一份规则。
	if _, ok := projectcontract.ProjectService(projects).(projectcontract.ShippingPolicyReader); !ok {
		t.Fatal("project service 未实现 ShippingPolicyReader：结算侧读不到任何运费规则")
	}
	policy, err := projects.ShippingPolicyOf(ctx, projectID)
	if err != nil {
		t.Fatalf("读运费规则失败: %v", err)
	}
	if policy.BaseFeeCents != 800 || policy.FreeThresholdCents != 10000 {
		t.Fatalf("运费端口应读回 800 / 10000 分，实际 %+v", policy)
	}

	// ③ 页面回显是**元**，且整页完整（渲染断言：缺 </html> 说明模板那一行断了）。
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/admin/settings?project="+projectID, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("站点设置页应 200，实际 %d", getRec.Code)
	}
	body := getRec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("站点设置页未完整渲染（缺 </html>）：\n%s", body)
	}
	for _, want := range []string{
		`name="shippingBaseFee" value="8.00"`,
		`name="shippingFreeThreshold" value="100.00"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q —— 保存的值没有回显到表单（或键名与 handler 读的不一致）", want)
		}
	}
}

// TestSiteSettingsShippingRejectsInvalidWithoutWriting 非法金额**不落库**，且提示经 ?err= 回带。
//
// 「不落库」是这条的核心断言：把非法值静默归零（或存成负数）都不报错，
// 而前者让运营以为自己配的运费生效了（实际从没生效过），后者是倒贴钱。
func TestSiteSettingsShippingRejectsInvalidWithoutWriting(t *testing.T) {
	router, projects, projectID := newSiteShippingEnv(t)
	ctx := context.Background()

	// 先存一份合法配置，用来验证「被拒绝时库里的旧值原样不动」。
	if rec := submitShippingSettings(t, router, projectID, map[string]string{
		"shippingBaseFee": "8.00",
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("前置保存失败：%d", rec.Code)
	}

	cases := []struct {
		name  string
		extra map[string]string
	}{
		{"负数", map[string]string{"shippingBaseFee": "-1"}},
		{"非数字", map[string]string{"shippingBaseFee": "八元"}},
		{"门槛负数", map[string]string{"shippingFreeThreshold": "-0.01"}},
		{"超上限", map[string]string{"shippingBaseFee": "20000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := submitShippingSettings(t, router, projectID, tc.extra)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("非法值应 303 回带提示，实际 %d", rec.Code)
			}
			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, "err=") {
				t.Fatalf("回跳地址没有 err= 提示：用户看不到任何原因（%q）", loc)
			}
			detail, err := projects.Detail(ctx, &projectdto.DetailReq{ID: projectID})
			if err != nil {
				t.Fatalf("读工程失败: %v", err)
			}
			fields := projectdto.ParseSiteSettings(detail.Settings)
			if fields.ShippingBaseFee != 800 {
				t.Fatalf("非法值被写进了库（基础运费变成 %d 分）—— 应当是保存被拒绝、旧值原样", fields.ShippingBaseFee)
			}
		})
	}
}
