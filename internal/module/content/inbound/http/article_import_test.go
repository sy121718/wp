package contenthttp

// article_import_test.go — 文章导入画布的口径测试。
//
// 三层：
//  1. 文档组装：文章的 SEO 字段是否真的落进页面设置（seoTitle 优先于 title）；
//  2. **真编译**：组装出来的 Page 文档必须能被 builder.Compile 吃下去 ——
//     settings 少一个必填项、节点树结构不对，都会在这里暴露（只渲染模板的测试看不见）；
//  3. 页面与片段：导入区块按三种前置状态渲染不同内容，预览片段走 handler 能渲染出东西。

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

	"go_wp/internal/builder"
	"go_wp/internal/builder/richdoc"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// stubProjectService 只实现本页用到的 List（其余方法嵌入 nil 接口，测试不会走到）。
type stubProjectService struct {
	projectcontract.ProjectService
	list []projectcontract.ProjectResp
}

func (s stubProjectService) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return s.list, nil
}

// stubPageService 让「导入能力可用」这一分支成立（本测试不触发写路径）。
type stubPageService struct {
	pagecontract.PageService
}

// failingContentService 读文章必失败（用于预览片段的错误分支 —— 不能给 nil 契约，
// 那会让 handler 在调用方法时 panic，测不出任何东西）。
type failingContentService struct {
	contentcontract.ContentService
}

func (failingContentService) Get(context.Context, *contentdto.GetReq) (*contentdto.ContentResp, error) {
	return nil, errors.New("boom")
}

func TestArticleImportDocumentCompiles(t *testing.T) {
	data := map[string]any{
		"title":          "文章标题（正文里的）",
		"body":           `<h2>小节</h2><p>正文 <strong>加粗</strong></p><ul><li>列表项</li></ul><blockquote>引用</blockquote><hr><img src="/storage/image/a.webp" alt="图">`,
		"excerpt":        "摘要",
		"seoTitle":       "SEO 标题优先于文章标题",
		"seoDescription": "SEO 描述",
		"focusKeyword":   "主关键词",
	}
	res, err := richdoc.HTMLToNodes(articleStr(data, "body"))
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	doc, err := articleImportDocument(data, res.Nodes)
	if err != nil {
		t.Fatalf("文档组装失败：%v", err)
	}

	page, err := builder.ParsePage(doc)
	if err != nil {
		t.Fatalf("生成的文档不是合法 Page 文档：%v\n%s", err, doc)
	}
	if page.Settings.SEO.Title != "SEO 标题优先于文章标题" {
		t.Errorf("SEO 标题应优先取 seoTitle，实际 %q", page.Settings.SEO.Title)
	}
	if page.Settings.SEO.Description != "SEO 描述" {
		t.Errorf("SEO 描述应取 seoDescription，实际 %q", page.Settings.SEO.Description)
	}
	if page.Settings.SEO.FocusKeyword != "主关键词" {
		t.Errorf("主关键词未带过去：%q", page.Settings.SEO.FocusKeyword)
	}
	if page.Settings.SEO.SchemaType != "article" {
		t.Errorf("结构化数据类型应为 article，实际 %q", page.Settings.SEO.SchemaType)
	}
	if page.Settings.Layout.Mode == "" {
		t.Error("版心模式为空 —— 编译期会直接拒掉这份文档")
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("模板集加载失败：%v", err)
	}
	compiled, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithProjectID("proj-test"))
	if err != nil {
		t.Fatalf("导入出来的页面编译失败：%v", err)
	}
	for _, want := range []string{"小节", "正文", "列表项", "引用"} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("编译产物缺少内容 %q", want)
		}
	}
}

func TestArticleImportDocumentFallsBackToArticleFields(t *testing.T) {
	// 没填 SEO 字段时用文章自身的标题/摘要（与编辑期评分器的回落口径一致）。
	data := map[string]any{"title": "标题", "body": "<p>正文</p>", "excerpt": "摘要"}
	res, err := richdoc.HTMLToNodes(articleStr(data, "body"))
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	doc, err := articleImportDocument(data, res.Nodes)
	if err != nil {
		t.Fatalf("文档组装失败：%v", err)
	}
	page, err := builder.ParsePage(doc)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if page.Settings.SEO.Title != "标题" || page.Settings.SEO.Description != "摘要" {
		t.Errorf("回落到文章字段失败：title=%q description=%q", page.Settings.SEO.Title, page.Settings.SEO.Description)
	}
	if page.Settings.SEO.FocusKeyword != "" {
		t.Errorf("没填主关键词时不该凭空造一个：%q", page.Settings.SEO.FocusKeyword)
	}
}

func TestArticleImportPreviewView(t *testing.T) {
	// 两个标题 + 一个段落 + 一段会被剥壳的 div 内容。
	res, err := richdoc.HTMLToNodes("<h2>一</h2><h2>二</h2><p>段落</p><div>壳里的字</div>")
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	view := articleImportPreviewViewOf(res)
	if !view.OK {
		t.Fatal("预览视图应为可用状态")
	}
	// 4 个节点：两个标题 + 一个段落 + div 剥壳后剩下的那段文字（剥壳不等于丢弃，内容照样成节点）。
	if view.NodeCount != 4 {
		t.Errorf("节点数应为 4（两个标题 + 段落 + 剥壳后的文字），实际 %d", view.NodeCount)
	}
	if view.Lossless {
		t.Error("含被剥壳的 div 时必须标为有损")
	}
	if len(view.Types) != 2 {
		t.Fatalf("应出现两种组件，实际 %d 种", len(view.Types))
	}
	if view.Types[0].Label != "标题" || view.Types[0].Count != 2 {
		t.Errorf("标题计数错误：%+v", view.Types[0])
	}
	if len(view.Warnings) != 1 || view.Warnings[0].Action != "去壳保留内容" {
		t.Errorf("降级动作未翻译成中文：%+v", view.Warnings)
	}
	// 无损输入：不产生警告，Lossless 为真。
	clean, err := richdoc.HTMLToNodes("<h2>标题</h2><p>段落</p>")
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	if v := articleImportPreviewViewOf(clean); !v.Lossless {
		t.Errorf("无损输入被标成有损：%+v", v.Warnings)
	}
}

func TestArticleImportBlockViewStates(t *testing.T) {
	h := &articlePageHandle{pages: stubPageService{}}
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello", Data: map[string]any{"title": "标题", "body": "<p>x</p>"}}
	projects := []gin.H{{"ID": "p1", "Name": "官网"}}

	v := articleImportBlockView(h, item, "a1", projects)
	if v["ImportAvailable"] != true {
		t.Errorf("具备全部前置时应可用：%v", v)
	} else if path, _ := v["ImportDefaultPath"].(string); path != "/article-hello" {
		t.Errorf("默认路径应为 /article-<slug>，实际 %q", path)
	}
	if v := articleImportBlockView(h, item, "", projects); v["ImportAvailable"] != false {
		t.Error("新建中（无 id）时不应给导入入口")
	}
	if v := articleImportBlockView(h, item, "a1", nil); v["ImportAvailable"] != false {
		t.Error("没有站点工程时不应给导入入口")
	}
	if v := articleImportBlockView(&articlePageHandle{}, item, "a1", projects); v["ImportAvailable"] != false {
		t.Error("页面能力未装配时不应给导入入口")
	}
}

// TestArticleImportPreviewFragmentRenders 直接打 handler，钉住"片段模板名对得上"
// （响应 200 但 body 为空，正是这类错的表现）。
func TestArticleImportPreviewFragmentRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	h := &articlePageHandle{contents: failingContentService{}}
	engine.POST("/admin/articles/import-preview", h.ArticleImportPreview)

	form := url.Values{"id": {"whatever"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/import-preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("预览片段渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("预览片段响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}
	if !strings.Contains(body, "badge") {
		t.Errorf("错误分支应渲染出提示：%q", body)
	}
}
