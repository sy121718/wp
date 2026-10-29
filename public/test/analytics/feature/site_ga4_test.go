package feature

// site_ga4_test.go — 站点统计代码（GA4）的**构建期端到端**验证（BIZ-8 目标 1）。
//
// 链路：SiteSettings（projects.settings）→ page 服务构建上下文 → builder → 产物 head。
// 使用真实项目设置与发布共用的AnalyticsCompileOptions；另验证只读预览不执行GA4。
//
// 断言两条：
//   - 配了测量 ID：产物 head 里出现 gtag 脚本，ID 正确；
//   - 清空测量 ID：产物里一个字节的统计代码都没有（零字节注入）。
//
// 依赖 PostgreSQL（support 三级回退，均不可用时整体 Skip）。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder"
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

// ga4DocJSON 最小页面文档（内容为空：本例验证的是 head 注入，不是组件渲染）。
const ga4DocJSON = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// TestSiteGA4InjectedIntoBuiltHead 站点设置里的测量 ID 进产物 head；清空后零字节。
func TestSiteGA4InjectedIntoBuiltHead(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "GA4 注入测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	// 预览编译与发布共用 compileDocument；这里不需要 artifact / publication 契约（预览不落盘）。
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

	// 清空测量 ID：产物里不再有统计代码（零字节注入不变量）。
	setGA4("")
	html, err = compilePublished()
	if err != nil {
		t.Fatalf("清空后预览编译失败: %v", err)
	}
	if strings.Contains(string(html), "googletagmanager") {
		t.Fatalf("清空测量 ID 后产物仍含统计代码:\n%s", html)
	}

	// 非法形状：不注入，也不把原始输入拼进 head（脚本注入防线）。
	setGA4(`G-ABC"><script>alert(1)</script>`)
	html, err = compilePublished()
	if err != nil {
		t.Fatalf("非法 ID 时预览编译失败: %v", err)
	}
	if strings.Contains(string(html), "googletagmanager") || strings.Contains(string(html), "alert(1)") {
		t.Fatalf("非法测量 ID 不该进产物:\n%s", html)
	}
}
