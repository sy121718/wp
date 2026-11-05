package contenthttp

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/richdoc"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

type stubProjectService struct {
	projectcontract.ProjectService
	list []projectcontract.ProjectResp
}

func (s stubProjectService) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return s.list, nil
}

type stubPageService struct {
	pagecontract.PageService
}

type failingContentService struct {
	contentcontract.ContentService
}

func (failingContentService) Get(context.Context, *contentdto.GetReq) (*contentdto.ContentResp, error) {
	return nil, errors.New("boom")
}

func TestArticleImportDocumentCompiles(t *testing.T) {
	data := map[string]any{
		"title":        "文章标题（正文里的）",
		"body":         `<h2>小节</h2><p>正文 <strong>加粗</strong></p><ul><li>列表项</li></ul><blockquote>引用</blockquote><hr><img src="https://assets.example.test/storage/image/a.webp" alt="图">`,
		"excerpt":      "摘要",
		"focusKeyword": "主关键词",

		"seoTitle":       "SEO 标题优先于文章标题",
		"seoDescription": "SEO 描述",
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
	if page.Settings.SEO.Title != "文章标题（正文里的）" {
		t.Errorf("SEO 标题应取文章标题，实际 %q", page.Settings.SEO.Title)
	}
	if page.Settings.SEO.Description != "摘要" {
		t.Errorf("SEO 描述应取摘要，实际 %q", page.Settings.SEO.Description)
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
	for _, want := range []string{"小节", "正文", "列表项", "引用", `src="https://assets.example.test/storage/image/a.webp"`} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("编译产物缺少内容 %q", want)
		}
	}
}

func TestArticleImportDocumentFallsBackToArticleFields(t *testing.T) {

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

	res, err := richdoc.HTMLToNodes("<h2>一</h2><h2>二</h2><p>段落</p><div>壳里的字</div>")
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	view := articleImportPreviewViewOf(res)
	if !view.OK {
		t.Fatal("预览视图应为可用状态")
	}

	if view.NodeCount != 4 {
		t.Errorf("节点数应为 4（两个标题 + 段落 + 剥壳后的文字），实际 %d", view.NodeCount)
	}
	if view.Lossless {
		t.Error("含被剥壳的 div 时必须标为有损")
	}
	if len(view.Types) != 2 {
		t.Fatalf("应出现两种组件，实际 %d 种", len(view.Types))
	}
	if view.Types[0].LabelFallback != "标题" || view.Types[0].Count != 2 {
		t.Errorf("标题计数错误：%+v", view.Types[0])
	}
	if len(view.Warnings) != 1 || view.Warnings[0].ActionFallback != "去壳保留内容" {
		t.Errorf("降级动作应给出中文兜底（取词在模板里）：%+v", view.Warnings)
	}

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
	projects := []OptionResp{{ID: "p1", Name: "官网"}}

	v := articleImportBlockView(h, item, "a1", projects)
	if !v.ImportAvailable {
		t.Errorf("具备全部前置时应可用：%+v", v)
	} else if v.ImportDefaultPath != "/article-hello" {
		t.Errorf("默认路径应为 /article-<slug>，实际 %q", v.ImportDefaultPath)
	}
	if v := articleImportBlockView(h, item, "", projects); v.ImportAvailable {
		t.Error("新建中（无 id）时不应给导入入口")
	}
	if v := articleImportBlockView(h, item, "a1", nil); v.ImportAvailable {
		t.Error("没有站点工程时不应给导入入口")
	}
	if v := articleImportBlockView(&articlePageHandle{}, item, "a1", projects); v.ImportAvailable {
		t.Error("页面能力未装配时不应给导入入口")
	}
}

func TestArticleImportPreviewFragmentRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	h := &articlePageHandle{contents: failingContentService{}}
	engine.POST("/admin/articles/import-preview", adminui.BareSlot(ArticleImportFragmentTemplate), h.ArticleImportPreview)

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
