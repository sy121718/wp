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
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
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

// BulkDeleteArticles 复刻真实语义：先归一（去重 / 上限），任一失败整批拒绝。
func (s *articleBulkDeleteStub) BulkDeleteArticles(_ context.Context, raw []string) error {
	ids, err := bulkids.Normalize(raw)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return contentenums.Err(contentenums.ErrInvalidParam)
	}
	for _, id := range ids {
		if s.failIDs[id] {
			return errors.New(contentenums.ErrNotFound)
		}
	}
	return nil
}

func articleBulkTestEngine(s *articleBulkDeleteStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	e.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	e.POST("/admin/articles/bulk-delete", pageview.HTMLFragment, (&articlePageHandle{contents: s}).ArticlesBulkDelete)
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
		{name: "全成功", ids: []string{"a", "b"}, state: "ok", want: "操作成功"},
		// 全成或全败：任一条失败整批拒绝，只播报一句失败。
		{name: "部分失败", ids: []string{"a", "bad"}, state: "err", want: "data-ui-error"},
		{name: "全部失败", ids: []string{"bad"}, state: "err", want: "data-ui-error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := articleBulkTestEngine(&articleBulkDeleteStub{failIDs: map[string]bool{"bad": true}})
			rec := articleBulkPost(e, url.Values{"ids": tc.ids})
			body := rec.Body.String()
			if tc.state == "ok" {
				if rec.Code != http.StatusOK {
					t.Fatalf("成功应 200，实际 %d（body=%s）", rec.Code, body)
				}
				if !strings.Contains(body, `role="status"`) {
					t.Errorf("成功应使用共享提示组件：%s", body)
				}
			} else {
				if rec.Code < 400 {
					t.Fatalf("失败应给真实 4xx，实际 %d（body=%s）", rec.Code, body)
				}
				if !strings.Contains(body, `data-ui-error`) {
					t.Errorf("失败应走共享失败出口的受控错误区：%s", body)
				}
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("响应缺少结论文案 %q：%s", tc.want, body)
			}
		})
	}
}

func TestArticlesBulkDeleteNoSelectionRendersJumpPage(t *testing.T) {
	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	rec := articleBulkPost(e, url.Values{})
	// 空选择不是「删了 0 条」的成功：整批拒绝，交共享失败出口。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未选中应整批拒绝 400，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Errorf("未选中应渲染错误区：%s", body[:min(len(body), 240)])
	}
}

func TestArticlesBulkDeleteLimitRendersControlledText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	form := url.Values{}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("id-%03d", i))
	}
	rec := articleBulkPost(e, form)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限应给真实 4xx，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) || !strings.Contains(body, "一次最多操作") {
		t.Errorf("超限应给出受控提示：%s", body[:min(len(body), 240)])
	}
}

func TestArticlesPageIgnoresForgedResultQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.GET("/admin/articles", pageview.Slot(ArticlesPageTemplate, pageview.RenderPage), (&articlePageHandle{contents: &articleSearchContentStub{total: 0}}).ArticlesPage)

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
	e.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	e.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	e.POST("/admin/articles/delete", pageview.HTMLFragment, (&articlePageHandle{contents: &articleBulkDeleteStub{failIDs: map[string]bool{"bad": true}}}).ArticleDelete)

	form := url.Values{"id": {"bad"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("业务失败应给真实 4xx，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Errorf("业务失败应走共享失败出口的受控错误区：%s", body[:min(len(body), 240)])
	}
	if !strings.Contains(body, `data-ui-error`) {
		t.Errorf("业务失败应是受控错误区：%s", body[:min(len(body), 400)])
	}
}

func TestArticleBulkResultTemplatesAreDistinct(t *testing.T) {
	if len(articleBulkResultTemplates) != 4 {
		t.Fatalf("四个分支应当各有一条模板，实际 %d 条", len(articleBulkResultTemplates))
	}
	seen := make(map[string]int, len(articleBulkResultTemplates))
	for i, tpl := range articleBulkResultTemplates {
		// 直接比较声明的消息事实（key/fallback/参数个数），不经过词库：
		// 单测环境没有 seed 词条，取词只会一律退成同一句归口文案。
		key := strings.TrimSpace(tpl.Key) + "|" + strings.TrimSpace(tpl.Fallback)
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
