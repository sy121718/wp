package contenthttp

// article_notice_test.go — 文章写动作结论的**新传输通道**回归。
//
// 背景：写动作原先靠 302 + `?err=` / `?ok=` / `?done=` 回列表页汇报结果，读侧要再判一次
// 「这条提示是不是本仓给的」（articleQueryText / articlePageDone）—— 而查询参数不是可信边界。
// 现在结论由 shell.RenderJump 渲染成整页提示（文案走响应体），那套读侧判定已整批删除。
//
// 本文件钉住两件事：
//   - 写侧四个批量分支都渲染出提示页（不是旧的 302），文案与计数说清楚；
//   - 手拼 /admin/articles?err=任意文案 再也不能往列表页塞伪造提示（读侧已无这条路）。

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
	"go_wp/internal/shell"
)

// articleBulkDeleteStub 只实现批量删除用到的 Delete（其余方法嵌入 nil 接口，测试不会走到）。
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

// articleBulkTestEngine 挂真实 Jet 渲染器与批量删除路由（提示页要真的渲染出来）。
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

// TestArticlesBulkDeleteRendersJumpPage 写侧四个分支都渲染提示页（不再是 302 + ?done=）。
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

// TestArticlesBulkDeleteNoSelectionRendersJumpPage 未选中任何 id 也走提示页（不是静默跳走）。
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

// TestArticlesBulkDeleteLimitRendersControlledText 超限整批拒绝的受控提示保持可见。
func TestArticlesBulkDeleteLimitRendersControlledText(t *testing.T) {
	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	form := url.Values{}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
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

// TestArticlesPageIgnoresForgedResultQuery 手拼 ?err= / ?ok= / ?done= 不再能注入伪造提示。
//
// 旧的读侧（articleQueryText / articlePageDone）已整批删除：列表页只剩**取数失败**一处提示。
// 这条用例取代原先的「伪造被读侧拒绝」断言 —— 现在根本没有那条通道可走。
func TestArticlesPageIgnoresForgedResultQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.GET("/admin/articles", (&articlePageHandle{contents: &articleSearchContentStub{total: 0}}).ArticlesPage)

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

// TestArticleDeleteRendersJumpPageWithBusinessText 单条删除的失败也走提示页，业务文案原样可见。
//
// 与批量删除共用同一套出口（articleListJump），但白名单命中的是单条删除的归口路径
// （articleFacingError）—— 覆盖 public/test/content 里那条 302→提示页的断言。
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

// TestArticleBulkResultTemplatesAreDistinct 四个分支的候选不能互相覆盖。
//
// 归一（数字 → 占位）之后如果两条模板变成同一串，说明它们只差一个计数 ——
// 那「全部失败」就会被当成「部分成功」显示（反之亦然），属于会误导运营的静默错误。
func TestArticleBulkResultTemplatesAreDistinct(t *testing.T) {
	if len(articleBulkResultTemplates) != 4 {
		t.Fatalf("四个分支应当各有一条模板，实际 %d 条", len(articleBulkResultTemplates))
	}
	c := articleBulkCtx(t)
	seen := make(map[string]int, len(articleBulkResultTemplates))
	for i, tpl := range articleBulkResultTemplates {
		// 比较**当前语言**的模板：词条写重了同样要红（中文相同、英文不同也算重）。
		key := strings.TrimSpace(articleBulkTextOf(c, tpl))
		if prev, ok := seen[key]; ok {
			t.Errorf("第 %d 与第 %d 条模板完全相同：%q", prev, i, key)
		}
		seen[key] = i
	}
}

// articleBulkCtx 纯函数用例用的上下文：批量结论文案的模板要按当前语言取
// （articleBulkTextOf），取词必须带 c。本包单跑时 i18n 未初始化，取词回落到中文原文。
func articleBulkCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
	return c
}
