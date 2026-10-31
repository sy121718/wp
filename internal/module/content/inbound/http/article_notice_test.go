package contenthttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
)

type articleBulkDeleteStub struct {
	contentcontract.ContentService
	failIDs map[string]bool
}

func (s *articleBulkDeleteStub) Delete(_ context.Context, req *contentdto.DeleteReq) error {
	if s.failIDs[req.ID] {
		return errors.New(contentenums.ErrNotFound)
	}
	return nil
}

func articleBulkTestEngine(s *articleBulkDeleteStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.POST("/admin/articles/bulk-delete", (&articlePageHandle{contents: s}).ArticlesBulkDelete)
	return e
}

func articleBulkPost(e *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestArticlesBulkDeleteRendersJumpPage(t *testing.T) {
	cases := []struct {
		name  string
		ids   []string
		state string
		want  string
	}{
		{name: "全成功", ids: []string{"a", "b"}, state: "ok", want: "已删除 2 篇文章。"},
		{name: "部分成功", ids: []string{"a", "bad"}, state: "err", want: "已删除 1 篇，1 篇未能删除"},
		{name: "全部失败", ids: []string{"bad"}, state: "err", want: "1 篇文章都未能删除"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := articleBulkTestEngine(&articleBulkDeleteStub{failIDs: map[string]bool{"bad": true}})
			rec := articleBulkPost(e, url.Values{"ids": tc.ids})
			if rec.Code != http.StatusOK {
				t.Fatalf("应渲染提示页（200），实际 status=%d location=%q", rec.Code, rec.Header().Get("Location"))
			}
			body := rec.Body.String()
			if !strings.Contains(body, `data-jump-state="`+tc.state+`"`) {
				t.Errorf("提示页状态应为 %q：%s", tc.state, body[:min(len(body), 240)])
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("提示页缺少结论文案 %q", tc.want)
			}
		})
	}
}

func TestArticlesBulkDeleteNoSelectionRendersJumpPage(t *testing.T) {
	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	rec := articleBulkPost(e, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("未选中应渲染提示页（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) || !strings.Contains(body, "没有选中任何文章") {
		t.Errorf("未选中应给出「没有选中任何文章」的提示：%s", body[:min(len(body), 240)])
	}
}

func TestArticlesBulkDeleteLimitRendersControlledText(t *testing.T) {
	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	form := url.Values{}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("id-%03d", i))
	}
	rec := articleBulkPost(e, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("超限应渲染提示页（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) || !strings.Contains(body, "一次最多操作") {
		t.Errorf("超限应给出受控提示：%s", body[:min(len(body), 240)])
	}
}

func TestArticlesPageIgnoresForgedResultQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.GET("/admin/articles", adminui.PageSlot(ArticlesPageTemplate), (&articlePageHandle{contents: &articleSearchContentStub{total: 0}}).ArticlesPage)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/articles?err="+url.QueryEscape("伪造失败文案")+
			"&ok="+url.QueryEscape("伪造成功文案")+
			"&done="+url.QueryEscape("伪造批量文案"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, forged := range []string{"伪造失败文案", "伪造成功文案", "伪造批量文案"} {
		if strings.Contains(body, forged) {
			t.Errorf("列表页仍回显查询参数里的伪造提示 %q", forged)
		}
	}
}

func TestArticleDeleteRendersJumpPageWithBusinessText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.POST("/admin/articles/delete", (&articlePageHandle{contents: &articleBulkDeleteStub{failIDs: map[string]bool{"bad": true}}}).ArticleDelete)

	form := url.Values{"id": {"bad"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染提示页（200），实际 status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Errorf("业务失败应是 err 态提示页：%s", body[:min(len(body), 240)])
	}
	if !strings.Contains(body, "这篇文章不存在") {
		t.Errorf("业务文案应原样可见：%s", body[:min(len(body), 400)])
	}
	if strings.Contains(body, "系统内部错误") {
		t.Errorf("业务错误被吞成归口文案")
	}
}

func TestArticleBulkResultTemplatesAreDistinct(t *testing.T) {
	if len(articleBulkResultTemplates) != 4 {
		t.Fatalf("四个分支应当各有一条模板，实际 %d 条", len(articleBulkResultTemplates))
	}
	c := articleBulkCtx(t)
	seen := make(map[string]int, len(articleBulkResultTemplates))
	for i, tpl := range articleBulkResultTemplates {

		key := strings.TrimSpace(adminui.MsgText(c, tpl))
		if prev, ok := seen[key]; ok {
			t.Errorf("第 %d 与第 %d 条模板完全相同：%q", prev, i, key)
		}
		seen[key] = i
	}
}

func articleBulkCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
	return c
}
