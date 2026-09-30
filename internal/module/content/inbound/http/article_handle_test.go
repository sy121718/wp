package contenthttp

import (
	"context"
	"errors"
	"html"
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

func TestArticleUpdateFailureEchoesSubmittedFields(t *testing.T) {
	old := &contentdto.ContentResp{ID: "a1", Slug: "original-slug", Revision: 7, UpdatedAt: "2026-09-19 12:30:45", Data: map[string]any{
		"title": "数据库旧标题", "body": "<p>数据库旧正文</p>", "excerpt": "数据库旧摘要",
		"featuredImage": "/storage/old.jpg", "seoTitle": "旧SEO", "seoDescription": "旧描述", "focusKeyword": "旧关键词",
	}}
	cases := []struct {
		name    string
		err     error
		form    url.Values
		message string
	}{
		{name: "标题缺失", form: url.Values{"id": {"a1"}, "title": {""}, "body": {"<p>新正文 &amp; 更多</p>"}, "excerpt": {""}, "featuredImage": {"/storage/new.jpg"}, "seoTitle": {""}, "seoDescription": {"新描述 <>&\"'"}, "focusKeyword": {"新词"}}, message: articleMissingTitleText},
		{name: "业务拒绝", err: errors.New(contentenums.ErrInvalidField), form: url.Values{"id": {"a1"}, "title": {"新标题 & < > \" '"}, "body": {"<h2>新正文 &amp; 标题</h2><p>第二段</p>"}, "excerpt": {"新摘要\n第二行"}, "featuredImage": {"https://example.test/?x=1&y=2"}, "seoTitle": {"SEO 新标题"}, "seoDescription": {"新描述 <>&\"'"}, "focusKeyword": {"新词"}}, message: articleFacingMessages[contentenums.ErrInvalidField]},
		{name: "内部错误归口", err: errors.New("relation secret_table does not exist"), form: url.Values{"id": {"a1"}, "title": {"新标题"}, "body": {"<p>新正文</p>"}, "excerpt": {"新摘要"}, "featuredImage": {""}, "seoTitle": {""}, "seoDescription": {""}, "focusKeyword": {""}}, message: "系统内部错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &articleUpdateContentStub{item: old, updateErr: tc.err}
			rec := articleUpdatePost(articleUpdateTestEngine(s), tc.form)
			if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
				t.Fatalf("失败须在原请求 200 渲染: status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range []string{"</html>", tc.message, `action="/admin/articles/update"`, `value="original-slug"`, `name="id" value="a1"`, "2026-09-19 12:30:45"} {
				if !strings.Contains(body, want) {
					t.Errorf("缺少 %q", want)
				}
			}
			// seoTitle / seoDescription 不在这里：两栏已与标题 / 摘要合并（2026-09-30），
			// 表单里没有对应输入框，页面自然也不回显它们的提交值。
			for field, value := range map[string]string{"title": tc.form.Get("title"), "body": tc.form.Get("body"), "excerpt": tc.form.Get("excerpt"), "featuredImage": tc.form.Get("featuredImage"), "focusKeyword": tc.form.Get("focusKeyword")} {
				if !strings.Contains(body, html.EscapeString(value)) {
					t.Errorf("%s 未保留原始提交值 %q", field, value)
				}
			}
			for _, stale := range []string{"数据库旧标题", "数据库旧正文", "数据库旧摘要", "/storage/old.jpg", "旧SEO", "旧描述", "旧关键词"} {
				if strings.Contains(body, stale) {
					t.Errorf("失败页回退到旧值 %q", stale)
				}
			}
			if strings.Contains(body, "secret_table") {
				t.Error("内部错误原文泄漏")
			}
			if !strings.Contains(body, `form="article-form"`) || strings.Contains(body, "以下是本次提交内容") {
				t.Error("元数据可信时应保留可操作的保存入口")
			}
			if tc.name == "标题缺失" && !strings.Contains(body, `aria-invalid="true" aria-describedby="article-edit-error"`) {
				t.Error("缺少标题时应标注出错字段并关联错误槽")
			}
		})
	}
}

func TestArticleUpdateMissingIDAndMetadataFailure(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		getErr  error
		message string
	}{
		{name: "缺少文章标识", message: articleMissingIDText},
		{name: "文章元数据读取失败", id: "a1", getErr: errors.New("relation private_table does not exist"), message: articleMissingTitleText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &articleUpdateContentStub{getErr: tc.getErr}
			form := url.Values{"id": {tc.id}, "title": {""}, "body": {"<p>提交的正文</p>"}, "focusKeyword": {"提交的关键词"}}
			rec := articleUpdatePost(articleUpdateTestEngine(s), form)
			if rec.Code != http.StatusOK {
				t.Fatalf("应原地渲染 200，实际 %d: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range []string{"</html>", tc.message, `action="/admin/articles/update"`, `name="body" value="&lt;p&gt;提交的正文&lt;/p&gt;"`, `name="focusKeyword"`, "提交的关键词"} {
				if !strings.Contains(body, want) {
					t.Errorf("缺少 %q", want)
				}
			}
			if strings.Contains(body, "private_table") {
				t.Error("元数据读取错误原文泄漏")
			}
			for _, forbidden := range []string{`form="article-form"`, `hx-post="/admin/articles/score"`, `action="/admin/articles/publish"`, `action="/admin/articles/import-page"`} {
				if strings.Contains(body, forbidden) {
					t.Errorf("元数据缺失时不应提供动作 %q", forbidden)
				}
			}
			if !strings.Contains(body, "以下是本次提交内容") {
				t.Error("只读回填态应明确提示回列表重新打开")
			}
		})
	}
}

func TestArticleUpdateSuccessRetainsRedirect(t *testing.T) {
	s := &articleUpdateContentStub{item: &contentdto.ContentResp{ID: "a1"}}
	rec := articleUpdatePost(articleUpdateTestEngine(s), url.Values{"id": {"a1"}, "title": {"更新"}})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "/admin/articles/edit?id=a1") {
		t.Fatalf("成功跳转变化: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if s.getCalls != 0 {
		t.Errorf("成功路径不应为回填读取数据库: %d", s.getCalls)
	}
}
