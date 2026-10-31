package contenthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/internal/templates"
)

type articleUpdateContentStub struct {
	contentcontract.ContentService
	item      *contentdto.ContentResp
	updateErr error
	getErr    error
	getCalls  int
	updateReq *contentdto.UpdateReq
}

func (s *articleUpdateContentStub) Get(_ context.Context, _ *contentdto.GetReq) (*contentdto.ContentResp, error) {
	s.getCalls++
	return s.item, s.getErr
}

func (s *articleUpdateContentStub) Update(_ context.Context, req *contentdto.UpdateReq) (*contentdto.ContentResp, error) {
	s.updateReq = req
	return s.item, s.updateErr
}

func articleUpdateTestEngine(s *articleUpdateContentStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.POST("/admin/articles/update", (&articlePageHandle{contents: s}).ArticleUpdate)
	return e
}

func articleUpdatePost(e *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestArticleUpdateFailureReturnsJSON(t *testing.T) {
	old := &contentdto.ContentResp{ID: "a1", Slug: "original-slug", Revision: 7, UpdatedAt: "2026-09-19 12:30:45", Data: map[string]any{
		"title": "数据库旧标题", "body": "<p>数据库旧正文</p>",
	}}
	cases := []struct {
		name    string
		err     error
		form    url.Values
		status  int
		message string
	}{
		{name: "标题缺失", form: url.Values{"id": {"a1"}, "title": {""}, "body": {"<p>新正文</p>"}}, status: http.StatusBadRequest, message: articleMissingTitleMsg.Fallback},
		{name: "业务拒绝", err: errors.New(contentenums.ErrInvalidField), form: url.Values{"id": {"a1"}, "title": {"新标题"}, "body": {"<p>新正文</p>"}}, status: http.StatusBadRequest, message: articleFallbacks[contentenums.ErrInvalidField]},
		{name: "内部错误归口", err: errors.New("relation secret_table does not exist"), form: url.Values{"id": {"a1"}, "title": {"新标题"}, "body": {"<p>新正文</p>"}}, status: http.StatusInternalServerError, message: "系统内部错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &articleUpdateContentStub{item: old, updateErr: tc.err}
			rec := articleUpdatePost(articleUpdateTestEngine(s), tc.form)
			if rec.Code != tc.status {
				t.Fatalf("状态码应为 %d，实际 %d：%s", tc.status, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("写动作失败应回 JSON，得到 %q", ct)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("失败不该发 302，得到 Location=%q", loc)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.message) {
				t.Errorf("响应应含归口文案 %q：%s", tc.message, body)
			}

			for _, banned := range []string{"</html>", `action="/admin/articles/update"`} {
				if strings.Contains(body, banned) {
					t.Errorf("失败响应不该含页壳片段 %q：%s", banned, body)
				}
			}

			for _, leaked := range []string{"secret_table", "admin.article.err."} {
				if strings.Contains(body, leaked) {
					t.Errorf("响应不应出现 %q：%s", leaked, body)
				}
			}
		})
	}
}

func TestArticleUpdateMissingIDReturnsJSON(t *testing.T) {

	s := &articleUpdateContentStub{}
	form := url.Values{"id": {""}, "title": {"标题"}, "body": {"<p>正文</p>"}}
	rec := articleUpdatePost(articleUpdateTestEngine(s), form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 id 应回 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, articleMissingIDMsg.Fallback) {
		t.Errorf("响应应含受控文案：%s", body)
	}
	if strings.Contains(body, "</html>") {
		t.Error("失败不该重渲页面")
	}
}

func TestArticleUpdateSuccessReturnsJSON(t *testing.T) {
	s := &articleUpdateContentStub{item: &contentdto.ContentResp{ID: "a1"}}
	rec := articleUpdatePost(articleUpdateTestEngine(s), url.Values{"id": {"a1"}, "title": {"更新"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("成功应回 200 JSON，实际 status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("成功不该发 302（导航归前端 data-write-nav），得到 Location=%q", loc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("成功应回 JSON，得到 %q", ct)
	}
	body := rec.Body.String()

	for _, want := range []string{"已保存", `"id":"a1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("成功响应缺少 %q：%s", want, body)
		}
	}

	if strings.Contains(body, "data-jump-state") {
		t.Errorf("成功响应不该再渲染提示页：%s", body)
	}
	if s.getCalls != 0 {
		t.Errorf("成功路径不应为回填读取数据库: %d", s.getCalls)
	}
}
