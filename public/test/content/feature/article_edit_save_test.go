package feature

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	contentdto "go_wp/internal/module/content/dto"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// The feature exercises the real content service, migrated PostgreSQL and Jet renderer.
func TestArticleEditSaveFailureKeepsPostValues(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	svc := contentservice.NewService(contentmodel.NewModel(db))
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO contents (id, entity_type, slug, revision, data, create_time, update_time) VALUES (?, 'article', 'old-slug', 1, ?::jsonb, now(), now())`, id, `{"title":"旧标题","body":"<p>旧正文</p>","seoTitle":"旧 SEO"}`).Error; err != nil {
		t.Fatalf("插入旧文章失败: %v", err)
	}
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := contenthttp.NewArticlePageHandle(svc, nil, nil, nil, nil)
	e.POST("/admin/articles/update", h.ArticleUpdate)
	e.GET("/admin/articles/edit", h.ArticleEditPage)

	posted := url.Values{
		"id": {id}, "title": {"草稿 & <特别> \"引号\""},
		"body":    {"<h2>新正文 &amp; 内容</h2><p>第二段</p>"},
		"excerpt": {"第一行\n第二行"}, "featuredImage": {"https://img.example/cover?a=1&b=2"},
		"seoTitle": {""}, "seoDescription": {"描述 <>&\"'"}, "focusKeyword": {"新关键词"},
	}
	// A database constraint rejects the write while the old row remains readable.
	if err := db.Exec(`ALTER TABLE contents ADD CONSTRAINT article_save_failure_test CHECK (revision < 2)`).Error; err != nil {
		t.Fatalf("安装测试写入约束失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/update", strings.NewReader(posted.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
		t.Fatalf("失败必须留在同请求: status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"</html>", "系统内部错误", `action="/admin/articles/update"`, `name="id" value="` + posted.Get("id") + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("失败页缺少 %q", want)
		}
	}
	for _, key := range []string{"title", "body", "excerpt", "featuredImage", "seoTitle", "seoDescription", "focusKeyword"} {
		if !strings.Contains(body, html.EscapeString(posted.Get(key))) {
			t.Errorf("%s 提交原值丢失: %q", key, posted.Get(key))
		}
	}
	if strings.Contains(body, "旧标题") || strings.Contains(body, "旧正文") || strings.Contains(body, "旧 SEO") {
		t.Error("失败时从数据库回显了旧字段")
	}
	if strings.Contains(body, "article_save_failure_test") || strings.Contains(body, "SQLSTATE") {
		t.Error("数据库内部错误泄漏")
	}
	old, err := svc.Get(req.Context(), &contentdto.GetReq{ID: id})
	if err != nil || old.Data["title"] != "旧标题" {
		t.Fatalf("失败后旧文章应保持不变: item=%+v err=%v", old, err)
	}
}
