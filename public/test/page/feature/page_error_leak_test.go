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
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
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

// pageWriteBody 断言写端点按共享成功 / 失败出口返回片段。
// 成功是 200 + 共享提示组件，失败是真实业务状态码 + 错误区。
func pageWriteBody(t *testing.T, wantOK bool, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := rec.Body.String()
	if wantOK {
		if rec.Code != http.StatusOK || !strings.Contains(body, `role="status"`) {
			t.Fatalf("成功应返回 200 + 共享提示片段，实际 %d，body=%s", rec.Code, body)
		}
		return body
	}
	if rec.Code < http.StatusBadRequest || !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应返回 4xx/5xx + 错误区，实际 %d，body=%s", rec.Code, body)
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
	// 与生产装配一致：失败出口、成功出口与请求级取词都在场。
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := pagehttp.NewPagesAdminHandle(svc, nil, nil, nil)
	// 生产路由把写端点声明为 HTML 片段；夹具必须同协议，否则失败会落到 JSON 出口。
	engine.POST("/admin/pages/delete", pageview.HTMLFragment, handle.DeletePage)
	engine.POST("/admin/pages/bulk-delete", pageview.HTMLFragment, handle.PagesBulkDelete)
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

	body := pageWriteBody(t, false, postPageForm(engine, "/admin/pages/delete", url.Values{"id": {pid}}))
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

	okBody := pageWriteBody(t, true, postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
	assertNoPageInternalLeak(t, "成功提示页", okBody)

	errBody := pageWriteBody(t, false, postPageForm(engine, "/admin/pages/delete", url.Values{"id": {created.ID}}))
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
	// 测试库只跑结构迁移、不跑种子；注入与生产同一条受控词条。
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	form := url.Values{}

	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	body := pageWriteBody(t, false, postPageForm(engine, "/admin/pages/bulk-delete", form))
	assertNoPageInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，实际 body=%s", body)
	}
}
