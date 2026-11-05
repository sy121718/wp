package feature

import (
	"context"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	userdto "go_wp/internal/module/user/dto"
	userhttp "go_wp/internal/module/user/inbound/http"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/internal/shell"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	"go_wp/public/test/support"
)

var customerLeakTokens = []string{"SQLSTATE", "uq_", "pg_", `relation "`, "constraint"}

func assertCustomerLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range customerLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

func newCustomerLeakEnv(t *testing.T) (*gin.Engine, *userservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	users := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, "测试站")

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们失败会落成 JSON。
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	page := userhttp.NewCustomerPageHandle(users, nil, projects)
	engine.GET("/admin/customers", pageview.Slot(userhttp.CustomersListPageTemplate, adminui.RenderPage), page.CustomersPage)
	engine.POST("/api/customer/bulk-status", pageview.HTMLFragment, userhttp.NewCustomerHandle(users).BulkCustomerStatus)
	engine.POST("/api/customer/bulk-unlock", pageview.HTMLFragment, userhttp.NewCustomerHandle(users).BulkCustomerUnlock)
	return engine, users, db
}

func TestCustomerBulkOversizedSelectionUsesControlledText(t *testing.T) {
	engine, _, _ := newCustomerLeakEnv(t)
	if engine == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate, "en-US": "At most %s items; got %s"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	n := bulkids.MaxIDs + 1
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	over := strconv.Itoa(n)

	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := url.Values{"ids": ids, "toStatus": {"1"}}
	probe.Request = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body.Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil || !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/api/customer/bulk-status", url.Values{"ids": ids, "status": {"1"}}},
		{"/api/customer/bulk-unlock", url.Values{"ids": ids}},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应渲染失败片段（400），实际 %d", tc.path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "data-ui-error") {
			t.Errorf("%s 应渲染失败片段", tc.path)
		}
		if !strings.Contains(body, "一次最多操作") {
			t.Errorf("%s 应显示「一次最多操作」的受控文案", tc.path)
		}

		if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(body, over) {
			t.Errorf("%s 的错误片段应带上限 %d 与本次条数 %s（由受控类型给回）",
				tc.path, bulkids.MaxIDs, over)
		}
		assertCustomerLeakFree(t, tc.path+" 的错误片段", body)
	}
}

func TestCustomerPageHidesInternalError(t *testing.T) {
	engine, users, db := newCustomerLeakEnv(t)
	if engine == nil {
		return
	}

	if err := db.Exec("DROP TABLE IF EXISTS users CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	_, rawErr := users.ListCustomers(context.Background(), &userdto.CustomerListReq{Limit: 5})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListCustomers 却成功了")
	}
	if !strings.Contains(rawErr.Error(), `relation "users"`) || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers", nil))
	out := rec.Body.String()
	assertCustomerLeakFree(t, "客户列表页响应体", out)
	for _, tok := range []string{"relation", "SQLSTATE", "42P01"} {
		if strings.Contains(out, tok) {
			t.Errorf("客户列表页响应体泄漏内部细节 %q", tok)
		}
	}
	if !strings.Contains(out, "系统内部错误，请稍后重试") {
		t.Errorf("页面应显示归口文案，实际未出现（渲染中断？）")
	}
}
