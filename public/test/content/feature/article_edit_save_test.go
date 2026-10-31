package feature

import (
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
		"focusKeyword": {"新关键词"},
	}

	if err := db.Exec(`ALTER TABLE contents ADD CONSTRAINT article_save_failure_test CHECK (revision < 2)`).Error; err != nil {
		t.Fatalf("安装测试写入约束失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/update", strings.NewReader(posted.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("数据库拒绝应回 500，实际 status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("失败不该发 302，得到 Location=%q", loc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("写动作失败应回 JSON，得到 %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "系统内部错误") {
		t.Errorf("响应应含归口文案：%s", body)
	}

	if strings.Contains(body, "</html>") || strings.Contains(body, `action="/admin/articles/update"`) {
		t.Errorf("失败响应不该含页壳：%s", body)
	}

	if strings.Contains(body, "article_save_failure_test") || strings.Contains(body, "SQLSTATE") {
		t.Error("数据库内部错误泄漏")
	}
	old, err := svc.Get(req.Context(), &contentdto.GetReq{ID: id})
	if err != nil || old.Data["title"] != "旧标题" {
		t.Fatalf("失败后旧文章应保持不变: item=%+v err=%v", old, err)
	}
}
