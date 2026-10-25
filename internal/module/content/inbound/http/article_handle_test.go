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

// TestArticleUpdateFailureReturnsJSON 保存失败回 JSON 错误体，页面不动。
//
// 判据在 2026-10 改过：从前这里断言「失败页必须含 </html> 且回填本次提交的值」——
// 那是**原生表单提交**时代的需求（浏览器跳到白屏，所以服务端得重渲一遍）。
// 表单改 htmx 之后（hx-swap="none"）页面根本没离开过，用户写的内容还在框里：
// 回填这个需求不存在，重渲这条路也就不该存在。
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
			// 页壳缺席：这是「失败不重渲」的分水岭。
			for _, banned := range []string{"</html>", `action="/admin/articles/update"`} {
				if strings.Contains(body, banned) {
					t.Errorf("失败响应不该含页壳片段 %q：%s", banned, body)
				}
			}
			// 原文与裸 key 都不许出现。
			for _, leaked := range []string{"secret_table", "admin.article.err."} {
				if strings.Contains(body, leaked) {
					t.Errorf("响应不应出现 %q：%s", leaked, body)
				}
			}
		})
	}
}

func TestArticleUpdateMissingIDReturnsJSON(t *testing.T) {
	// 缺 id 属参数错误：400 + 归口文案，不重渲页面（同上一个用例的判据变化）。
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

// TestArticleUpdateSuccessReturnsJSON 成功只回 JSON，导航交给前端。
//
// 通道又变了一次：302+?ok= → 提示页（data-jump-state）→ **JSON**。
// 最后这次的理由是 AGENTS.md 那条「成功写动作只回 JSON；导航由前端 data-write-nav 决定，
// 后端不发 302 / HX-Redirect」—— 提示页是原生提交时代为了「让用户看到结论」而造的第二条通道，
// 表单改 htmx 之后 toast 就够，页面留在原地。
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
	// 归口文案 + 新实体 id（前端 data-write-nav 的 {id} 从这里取）。
	for _, want := range []string{"已保存", `"id":"a1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("成功响应缺少 %q：%s", want, body)
		}
	}
	// 提示页那套标记不该再出现（它们是旧通道的产物）。
	if strings.Contains(body, "data-jump-state") {
		t.Errorf("成功响应不该再渲染提示页：%s", body)
	}
	if s.getCalls != 0 {
		t.Errorf("成功路径不应为回填读取数据库: %d", s.getCalls)
	}
}
