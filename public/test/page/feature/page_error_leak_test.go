package feature

// page_error_leak_test.go — page 后台页不直出内部错误（第三波 CQ-009 形态 ②③）。
//
// 写动作的结论现在由 shell.RenderJump 渲染成**整页提示**（HTTP 200，文案走响应体），
// 取代原先的 303 + ?err=：查询参数不是可信边界，读侧判定随之整批删除。
// 本文件制造一个**真实的基础设施错误** —— 把 pages 表改名，查询立刻报
// relation "pages" does not exist (SQLSTATE 42P01) —— 先反证 service 层的原始错误
// 确实带表名与 SQLSTATE，再断言提示页里没有它、只有归口文案。
//
// 另两条覆盖「收口不能过头」：
//   · 业务 sentinel（ErrPageNotFound）仍要原样透出（只是翻成当前语言）；
//   · shell.BulkIDs 的受控上限提示（一次最多操作 N 项）保持可见。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// pageInternalLeakTokens 内部细节指纹：出现任一即视为泄漏。
var pageInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoPageInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range pageInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// pageJumpBody 断言响应是整页提示（HTTP 200 + data-jump-state）并返回响应体。
func pageJumpBody(t *testing.T, state string, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应为提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="`+state+`"`) {
		t.Fatalf("提示页缺少 data-jump-state=%q：%s", state, body)
	}
	return body
}

func postPageForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// newPageErrorLeakEnv 装配只挂两个写端点的测试引擎（真实 service + 真实 PG + 真实模板引擎）。
func newPageErrorLeakEnv(t *testing.T) (engine *gin.Engine, db *gorm.DB, svc pagecontract.PageService, projectID string) {
	t.Helper()
	db, svc, projectID = newPageService(t)
	if db == nil {
		return nil, nil, nil, ""
	}
	gin.SetMode(gin.TestMode)
	engine = gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := pagehttp.NewPagesAdminHandle(svc, nil, nil, nil)
	engine.POST("/admin/pages/delete", handle.DeletePage)
	engine.POST("/admin/pages/bulk-delete", handle.PagesBulkDelete)
	return engine, db, svc, projectID
}

// TestPageDeleteHidesInternalError 真实基础设施错误（pages 表不存在）只进日志。
func TestPageDeleteHidesInternalError(t *testing.T) {
	engine, db, svc, _ := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "22222222-2222-2222-2222-222222222222"

	if err := db.Exec("ALTER TABLE pages RENAME TO pages_hidden").Error; err != nil {
		t.Fatalf("改名 pages 表失败：%v", err)
	}
	// 反证：去掉 handler 这一层，service 的原始错误确实带表名与 SQLSTATE。
	rawErr := svc.Delete(context.Background(), &pagecontract.DeleteReq{ID: pid})
	if rawErr == nil {
		t.Fatalf("pages 表不存在时删除应失败")
	}
	if !strings.Contains(rawErr.Error(), "pages") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	body := pageJumpBody(t, "err", postPageForm(engine, "/admin/pages/delete", url.Values{"id": {pid}}))
	assertNoPageInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("内部错误应给归口文案，实际 body=%s", body)
	}
}

// TestPageDeleteKeepsBusinessError 页面不存在（业务 sentinel）不能被吞成通用文案。
func TestPageDeleteKeepsBusinessError(t *testing.T) {
	engine, _, svc, projectID := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath:     "/leak-probe",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败：%v", err)
	}
	// 第一次删除成功（成功提示页），第二次必然落到「页面不存在」这条业务 sentinel。
	okBody := pageJumpBody(t, "ok", postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
	assertNoPageInternalLeak(t, "成功提示页", okBody)

	errBody := pageJumpBody(t, "err", postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
	assertNoPageInternalLeak(t, "失败提示页", errBody)
	if strings.Contains(errBody, "系统内部错误") {
		t.Fatalf("业务错误被吞成归口文案：body=%s", errBody)
	}
	if !strings.Contains(errBody, "ErrPageNotFound") && !strings.Contains(errBody, "页面不存在") {
		t.Fatalf("业务错误应原样透出（key 或译文），实际 body=%s", errBody)
	}
}

// TestPageBulkDeleteKeepsControlledLimitText 受控提示（shell.BulkIDs 上限）保持可见。
func TestPageBulkDeleteKeepsControlledLimitText(t *testing.T) {
	engine, _, _, _ := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	form := url.Values{}
	// 用高于上限一条即触发整批拒绝（不依赖 @10 这类经验数字）。
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	body := pageJumpBody(t, "err", postPageForm(engine, "/admin/pages/bulk-delete", form))
	assertNoPageInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，实际 body=%s", body)
	}
}
