package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"fmt"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	contentdto "go_wp/internal/module/content/dto"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/public/test/support"
)

var contentInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoContentInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range contentInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

func newContentErrorLeakEnv(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil
	}
	svc := contentservice.NewService(contentmodel.NewModel(db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	translations := contenthttp.NewArticleTranslationHandle(svc)
	engine.GET("/admin/articles/translations", pageview.Slot(contenthttp.ArticleTranslationsTemplate, adminui.RenderPage), translations.ArticleTranslations)
	page := contenthttp.NewArticlePageHandle(svc, nil, nil, nil, nil)
	engine.POST("/admin/articles/bulk-delete", page.ArticlesBulkDelete)
	engine.POST("/admin/articles/delete", page.ArticleDelete)
	return engine, db
}

func TestArticleTranslationsHidesInternalError(t *testing.T) {
	engine, db := newContentErrorLeakEnv(t)
	if engine == nil {
		return
	}
	if err := db.Exec("ALTER TABLE contents RENAME TO contents_hidden").Error; err != nil {
		t.Fatalf("改名 contents 表失败：%v", err)
	}

	svc := contentservice.NewService(contentmodel.NewModel(db))
	_, rawErr := svc.List(context.Background(), &contentdto.ListReq{EntityType: "article", Limit: 50})
	if rawErr == nil {
		t.Fatalf("contents 表不存在时列表应失败")
	}
	if !strings.Contains(rawErr.Error(), "contents") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/articles/translations?lang=zh-CN", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("工作台应 200（渲染错误条），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentInternalLeak(t, "译文工作台 HTML", body)
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("模板数据的错误文案应是归口文案，body=%s", body)
	}
	if !strings.Contains(body, "读取文章总数失败") {
		t.Fatalf("先计数、后取页：计数失败应保留「读取文章总数失败」语境，body=%s", body)
	}

	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面未渲染完整（缺 </html>）：%s", body)
	}
}

func TestArticleDeleteKeepsBusinessErrorText(t *testing.T) {
	engine, _ := newContentErrorLeakEnv(t)
	if engine == nil {
		return
	}

	form := url.Values{"id": {"88888888-8888-8888-8888-888888888888"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染整页提示（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentInternalLeak(t, "文章删除提示页 HTML", body)
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("业务失败应是 err 态提示页：%s", body[:min(len(body), 240)])
	}
	if !strings.Contains(body, "这篇文章不存在") {
		t.Fatalf("业务文案应原样可见（文章不存在）：%s", body[:min(len(body), 400)])
	}
	if strings.Contains(body, "系统内部错误") {
		t.Fatalf("业务错误被吞成归口文案")
	}
}

func TestArticlesBulkDeleteKeepsControlledLimitText(t *testing.T) {
	engine, _ := newContentErrorLeakEnv(t)
	if engine == nil {
		return
	}
	form := url.Values{}

	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染整页提示（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentInternalLeak(t, "批量删除提示页 HTML", body)
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("超限应是 err 态提示页：%s", body[:min(len(body), 240)])
	}
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见：%s", body[:min(len(body), 400)])
	}
}
