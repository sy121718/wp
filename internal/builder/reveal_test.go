package builder

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

func TestApplyScrollReveal(t *testing.T) {

	empty := &core.InteractionProps{}
	applyScrollReveal(empty, "fade-in-up")
	if empty.Entrance != "fade-in-up" || empty.ScrollReveal != "reveal" {
		t.Errorf("空配置应注入: %+v", empty)
	}

	explicit := &core.InteractionProps{Entrance: "bounce-in"}
	applyScrollReveal(explicit, "fade-in-up")
	if explicit.Entrance != "bounce-in" || explicit.ScrollReveal != "" {
		t.Errorf("显式 entrance 不应被覆盖: %+v", explicit)
	}

	story := &core.InteractionProps{ScrollStory: "zoom"}
	applyScrollReveal(story, "fade-in-up")
	if story.Entrance != "" {
		t.Errorf("scrollStory 存在时不应注入: %+v", story)
	}

	other := &core.InteractionProps{HoverEffect: "lift"}
	applyScrollReveal(other, "fade-in-up")
	if other.HoverEffect != "lift" || other.Entrance != "fade-in-up" {
		t.Errorf("应保留既有字段并注入: %+v", other)
	}
}

const revealDoc = `{"settings":{"layout":{"mode":"full"},"base":{}},"root":[	{"id":"sec","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","gap":"16px"}},"styleEx":{"reveal":"off"}},"children":[		{"id":"inner","type":"core.divider","props":{}}	]},	{"id":"out","type":"core.divider","props":{}}]}`

func TestScrollRevealLayered(t *testing.T) {
	theme := &ThemeSettings{Motion: ThemeMotion{ScrollRevealDefault: true, DefaultEntrance: "fade-in-up"}}
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

	for _, want := range []string{"animation-timeline: view()", "animation: sky-fade-in-up 0.6s ease backwards", "@keyframes sky-fade-in-up"} {
		if !strings.Contains(compiled.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, compiled.CSS)
		}
	}

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

func countScope(scopes []string, want string) int {
	n := 0
	for _, s := range scopes {
		if s == want {
			n++
		}
	}
	return n
}

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
