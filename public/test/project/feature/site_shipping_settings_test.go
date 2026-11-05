package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"go_wp/internal/adminui"
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
	router.GET("/admin/settings", pageview.Slot(projecthttp.SiteSettingsPageTemplate, adminui.RenderPage), handle.SiteSettings)
	router.POST("/admin/settings/save", pageview.Slot(projecthttp.SiteSettingsPageTemplate, adminui.RenderPage), handle.SaveSiteSettings)
	return router, projects, project.ID
}

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

func TestSiteSettingsShippingSaveAndReadBack(t *testing.T) {
	router, projects, projectID := newSiteShippingEnv(t)
	ctx := context.Background()

	rec := submitShippingSettings(t, router, projectID, map[string]string{
		"shippingBaseFee":       "8.00",
		"shippingFreeThreshold": "100",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应 200 渲染成功提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("响应不是成功提示页（缺 data-jump-state=\"ok\"）:\n%s", rec.Body.String())
	}

	detail, err := projects.Detail(ctx, &projectdto.DetailReq{ID: projectID})
	if err != nil {
		t.Fatalf("读工程失败: %v", err)
	}
	fields := projectdto.ParseSiteSettings(detail.Settings)
	if fields.ShippingBaseFee != 800 || fields.ShippingFreeThreshold != 10000 {
		t.Fatalf("库内应为 800 / 10000 分，实际 %d / %d",
			fields.ShippingBaseFee, fields.ShippingFreeThreshold)
	}

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

func TestSiteSettingsShippingRejectsInvalidWithoutWriting(t *testing.T) {
	router, projects, projectID := newSiteShippingEnv(t)
	ctx := context.Background()

	if rec := submitShippingSettings(t, router, projectID, map[string]string{
		"shippingBaseFee": "8.00",
	}); rec.Code != http.StatusOK {
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
			if rec.Code != http.StatusOK {
				t.Fatalf("非法值应 200 渲染失败提示页，实际 %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `data-jump-state="err"`) {
				t.Fatalf("响应不是失败提示页（缺 data-jump-state=\"err\"）：用户看不到任何原因\n%s", body)
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
