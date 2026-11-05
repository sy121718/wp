package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/builder"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

const ga4DocJSON = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

func TestSiteGA4InjectedIntoBuiltHead(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "GA4 注入测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	ctx := context.Background()

	setGA4 := func(raw string) {
		t.Helper()
		settings := map[string]any{}
		if raw != "" {
			settings["ga4MeasurementId"] = raw
		}
		encoded, merr := json.Marshal(settings)
		if merr != nil {
			t.Fatalf("序列化设置失败: %v", merr)
		}
		if _, uerr := projects.Update(ctx, &projectcontract.UpdateReq{
			ID: project.ID, Name: project.Name, Settings: encoded,
		}); uerr != nil {
			t.Fatalf("写入站点设置失败: %v", uerr)
		}
	}

	compilePublished := func() ([]byte, error) {
		page, err := builder.ParsePage([]byte(ga4DocJSON))
		if err != nil {
			return nil, err
		}
		set, err := templates.NewEmbeddedComponentSet()
		if err != nil {
			return nil, err
		}
		opts := append(pipeline.AnalyticsCompileOptions(ctx, projects, project.ID), builder.WithComponentSet(set), builder.WithCompileMode(builder.CompileModePublish))
		compiled, err := builder.Compile(page, opts...)
		if err != nil {
			return nil, err
		}
		html, err := builder.RenderDocument(compiled)
		return []byte(html), err
	}
	setGA4("G-ABC1234567")
	preview, err := pages.CompilePreview(ctx, []byte(ga4DocJSON), project.ID, "/about", "zh-CN", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), "googletagmanager") {
		t.Fatal("只读预览不应执行正式GA4采集")
	}
	html, err := compilePublished()
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	if !strings.Contains(string(html), "googletagmanager.com/gtag/js?id=G-ABC1234567") {
		t.Fatalf("产物 head 缺少 gtag 引用:\n%s", html)
	}
	if !strings.Contains(string(html), "gtag('config','G-ABC1234567')") {
		t.Fatalf("产物 head 缺少 gtag config:\n%s", html)
	}
	if n := strings.Count(string(html), "googletagmanager"); n != 1 {
		t.Errorf("统计脚本应恰好注入一次，实际 %d 次", n)
	}

	setGA4("")
	html, err = compilePublished()
	if err != nil {
		t.Fatalf("清空后预览编译失败: %v", err)
	}
	if strings.Contains(string(html), "googletagmanager") {
		t.Fatalf("清空测量 ID 后产物仍含统计代码:\n%s", html)
	}

	setGA4(`G-ABC"><script>alert(1)</script>`)
	html, err = compilePublished()
	if err != nil {
		t.Fatalf("非法 ID 时预览编译失败: %v", err)
	}
	if strings.Contains(string(html), "googletagmanager") || strings.Contains(string(html), "alert(1)") {
		t.Fatalf("非法测量 ID 不该进产物:\n%s", html)
	}
}
