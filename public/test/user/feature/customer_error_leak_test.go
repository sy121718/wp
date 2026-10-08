package feature

// customer_error_leak_test.go — 后台客户页不把 error 原文送进响应（审计 CQ-009 / 第三波收口）。
//
// 两件事：
//   · 批量动作的 id 超限（本批改向）：超限错误是 shell 的**受控类型**
//     （shell.ErrBulkIDsTooMany / *shell.BulkIDsError），页面走 shell.BulkIDsFacingText，
//     文案由类型给出 —— 因此「当前 M 项」（去重后的条数，只有 shell 知道）也回到了页面上，
//     而不是像旧实现那样用 shell.MaxBulkIDs 重组、把 M 丢掉；结构性断言见下；
//   · 取数撞上真实 PG 错误（表被删）：错误原文带表名与 SQLSTATE，页面只给归口文案，
//     并由 customerFacingError 记一条带场景 / user_id / 路径的结构化日志（原文只进日志）。
//
// 断言是**强**的：响应体里出现 SQLSTATE / uq_ / pg_ / relation " 任一即失败；同时断言
// 归口文案确实出现 —— 只断言「没有泄漏」会被「把页面渲染成空白」蒙混过去。

import (
	"context"
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
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/public/test/support"
)

// customerLeakTokens 内部细节指纹：PG 原文与库结构里一定出现、业务文案里一定不出现。
//
// 刻意不放裸的 "users" —— 页面路径 /admin/customers 本身就含这个子串。
var customerLeakTokens = []string{"SQLSTATE", "uq_", "pg_", `relation "`, "constraint"}

// assertCustomerLeakFree 断言给定文本不含任何内部细节指纹。
func assertCustomerLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range customerLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// newCustomerLeakEnv 客户页的最小装配：真实 PG + 生产迁移 + 真实 user service。
//
// 一并把 *gorm.DB 交回给用例：制造「真实基础设施错误」要靠它删表。
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
	// 订单摘要端口传 nil：列表页不用它（详情页才用），本文件只打列表页与批量端点。
	page := userhttp.NewCustomerPageHandle(users, nil, projects)
	engine.GET("/admin/customers", page.CustomersPage)
	engine.POST("/admin/customers/bulk-status", page.CustomerBulkStatusSave)
	engine.POST("/admin/customers/bulk-unlock", page.CustomerBulkUnlock)
	return engine, users, db
}

// TestCustomerBulkOversizedSelectionUsesControlledText 批量入口在 id 超限时回带受控文案，
// 且该文案与 err.Error() 无关（原文里的计数不会跟着出去）。
func TestCustomerBulkOversizedSelectionUsesControlledText(t *testing.T) {
	engine, _, _ := newCustomerLeakEnv(t)
	if engine == nil {
		return
	}
	n := shell.MaxBulkIDs + 1
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	over := strconv.Itoa(n)

	// 反证：shell.BulkIDs 在同一份表单上给出的原文带「当前 N 项」的计数。
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
		{"/admin/customers/bulk-status", url.Values{"ids": ids, "toStatus": {"1"}}},
		{"/admin/customers/bulk-unlock", url.Values{"ids": ids}},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 应渲染提示页（200），实际 %d", tc.path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-jump-state="err"`) {
			t.Errorf("%s 应渲染失败提示页", tc.path)
		}
		if !strings.Contains(body, "一次最多操作") {
			t.Errorf("%s 应显示「一次最多操作」的受控文案", tc.path)
		}
		// 结构性断言（本批改向）：文案由**受控类型**给出，两个数字（上限 / 本次条数）都要在。
		// 旧实现能对上「一次最多操作」，却恰好丢了「当前 M 项」—— 所以少了 M 就是退化。
		if !strings.Contains(body, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(body, over) {
			t.Errorf("%s 的提示页应带上限 %d 与本次条数 %s（由受控类型给回）",
				tc.path, shell.MaxBulkIDs, over)
		}
		assertCustomerLeakFree(t, tc.path+" 的提示页", body)
	}
}

// TestCustomerPageHidesInternalError 客户列表取数撞上真实 PG 错误（表被删）：
// 页面只给归口文案，不带表名 / SQLSTATE。
func TestCustomerPageHidesInternalError(t *testing.T) {
	engine, users, db := newCustomerLeakEnv(t)
	if engine == nil {
		return
	}
	// 制造**真实**基础设施错误：把 users 表删掉。
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
