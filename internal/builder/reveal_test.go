package builder

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// reveal_test.go — 滚动显现分层开关测试（H5「滚动过去才出内容」）：
// 注入单元（结构体层）/ 分层集成（主题开→顶层注入、容器 off→子树豁免）/ 默认关闭零产物。

// TestApplyScrollReveal 注入单元：未配置时注入默认入场+滚动触发；
// 显式配置（entrance/scrollStory/scrollReveal 任一）不覆盖；其他动效字段不受影响。
func TestApplyScrollReveal(t *testing.T) {
	// 空配置：注入。
	empty := &core.InteractionProps{}
	applyScrollReveal(empty, "fade-up")
	if empty.Entrance != "fade-up" || empty.ScrollReveal != "reveal" {
		t.Errorf("空配置应注入: %+v", empty)
	}

	// 显式 entrance：不覆盖。
	explicit := &core.InteractionProps{Entrance: "bounce-in"}
	applyScrollReveal(explicit, "fade-up")
	if explicit.Entrance != "bounce-in" || explicit.ScrollReveal != "" {
		t.Errorf("显式 entrance 不应被覆盖: %+v", explicit)
	}

	// 滚动叙事：不注入（story 独占 animation 声明）。
	story := &core.InteractionProps{ScrollStory: "zoom"}
	applyScrollReveal(story, "fade-up")
	if story.Entrance != "" {
		t.Errorf("scrollStory 存在时不应注入: %+v", story)
	}

	// 其他动效字段保留（注入为字段级合并，不触碰悬浮等）。
	other := &core.InteractionProps{HoverEffect: "lift"}
	applyScrollReveal(other, "fade-up")
	if other.HoverEffect != "lift" || other.Entrance != "fade-up" {
		t.Errorf("应保留既有字段并注入: %+v", other)
	}
}

// revealDoc 分层集成用例：容器 off 豁免子树 + 顶层组件正常注入。
// 组件选 core.divider（Atom 基座必带 AdvancedProps、空 props 合法且必有 CSS 输出；
// heading 无 Advanced 层、spacer 空 props 会被宽容校验跳过——均不适合本用例）。
const revealDoc = `{"settings":{"layout":{"mode":"full"},"base":{}},"root":[	{"id":"sec","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","gap":"16px"}},"styleEx":{"reveal":"off"}},"children":[		{"id":"inner","type":"core.divider","props":{}}	]},	{"id":"out","type":"core.divider","props":{}}]}`

// TestScrollRevealLayered 分层集成：主题全站开启 → 顶层组件注入、容器 off 子树豁免。
func TestScrollRevealLayered(t *testing.T) {
	theme := &ThemeSettings{Motion: ThemeMotion{ScrollRevealDefault: true, DefaultEntrance: "fade-up"}}
	page, err := ParsePage([]byte(revealDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set: %v", err)
	}
	compiled, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	for _, want := range []string{"animation-timeline: view()", "animation: sky-fade-up 0.6s ease backwards", "@keyframes sky-fade-up"} {
		if !strings.Contains(compiled.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, compiled.CSS)
		}
	}
	// 注入断言：容器自身与顶层组件各一份（共 2 个实例）；容器 off 仅豁免其子树。
	// （容器交互已统一走 core.CompileInteraction，自身同样支持滚动触发。）
	//
	// 断言口径是「覆盖到的实例作用域」，不是声明文本的出现次数：两个实例的 reveal 规则
	// 只差作用域类名、声明完全相同且相邻，会被并列合并成一条规则（PERF-016），
	// 文本次数随之从 2 变 1 —— 数次数会把等价产物误判成回归。
	scopes := revealInjectedScopes(compiled.CSS)
	for _, want := range []string{".sky-c-sec", ".sky-c-out"} {
		if got := countScope(scopes, want); got != 1 {
			t.Errorf("滚动显现未覆盖 %s（期望 1 个实例规则），实际覆盖=%v\n%s", want, scopes, compiled.CSS)
		}
	}
	if len(scopes) != 2 {
		t.Errorf("滚动显现覆盖的实例数不符（期望 2：容器自身 + 顶层组件），实际 %d：%v", len(scopes), scopes)
	}
}

// revealInjectedScopes 收集所有「带滚动触发声明」的规则覆盖到的实例作用域选择器。
func revealInjectedScopes(css string) []string {
	var scopes []string
	lines := strings.Split(css, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasSuffix(line, "{") || strings.HasPrefix(line, "@") {
			continue
		}
		sel := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		hasView := false
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) != "}" {
			if strings.Contains(lines[j], "animation-timeline: view()") {
				hasView = true
			}
			j++
		}
		if hasView {
			for _, s := range strings.Split(sel, ",") {
				scopes = append(scopes, strings.TrimSpace(s))
			}
		}
		i = j
	}
	return scopes
}

// countScope 统计选择器在切片里的出现次数。
func countScope(scopes []string, want string) int {
	n := 0
	for _, s := range scopes {
		if s == want {
			n++
		}
	}
	return n
}

// TestScrollRevealOffByDefault 默认关闭：主题未开启时零注入，产物字节不变。
func TestScrollRevealOffByDefault(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"},"base":{}},"root":[{"id":"h","type":"core.heading","props":{"text":"标题"}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set: %v", err)
	}
	compiled, err := Compile(page, WithComponentSet(set), WithThemeSettings(&ThemeSettings{}))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if strings.Contains(compiled.CSS, "animation-timeline") {
		t.Errorf("未开启滚动显现时不应注入 view()\n%s", compiled.CSS)
	}
}
