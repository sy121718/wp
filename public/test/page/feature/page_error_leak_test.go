package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagehttp "go_wp/internal/module/page/inbound/http"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

var pageInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoPageInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range pageInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

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

func TestPageDeleteHidesInternalError(t *testing.T) {
	engine, db, svc, _ := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "22222222-2222-2222-2222-222222222222"

	if err := db.Exec("ALTER TABLE pages RENAME TO pages_hidden").Error; err != nil {
		t.Fatalf("改名 pages 表失败：%v", err)
	}

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

func TestPageDeleteKeepsBusinessError(t *testing.T) {
	engine, _, svc, projectID := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	// 取词收口后业务错误的 Fallback 恒为空串，文案只能来自词库：注入译文再断言，
	// 别让「key 或译文」这种两可期望留着 —— 归口契约就是「不把裸 key 交出去」。
	i18n.InjectForTest(map[string]map[string]string{
		pageenums.ErrPageNotFound: {"zh-CN": "页面不存在"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath:     "/leak-probe",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败：%v", err)
	}

	okBody := pageJumpBody(t, "ok", postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
	assertNoPageInternalLeak(t, "成功提示页", okBody)

	errBody := pageJumpBody(t, "err", postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
	assertNoPageInternalLeak(t, "失败提示页", errBody)
	if strings.Contains(errBody, "系统内部错误") {
		t.Fatalf("业务错误被吞成归口文案：body=%s", errBody)
	}
	if !strings.Contains(errBody, "页面不存在") {
		t.Fatalf("业务错误应透出词库译文，实际 body=%s", errBody)
	}
}

func TestPageBulkDeleteKeepsControlledLimitText(t *testing.T) {
	engine, _, _, _ := newPageErrorLeakEnv(t)
	if engine == nil {
		return
	}
	form := url.Values{}

	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	body := pageJumpBody(t, "err", postPageForm(engine, "/admin/pages/bulk-delete", form))
	assertNoPageInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，实际 body=%s", body)
	}
}
