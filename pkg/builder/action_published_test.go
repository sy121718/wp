package builder

import (
	"errors"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestPublishedActionsCompileOnlyResolvedPaths(t *testing.T) {
	for _, kind := range []core.ActionKind{core.ActionPage, core.ActionPresentation} {
		t.Run(string(kind), func(t *testing.T) {
			target := core.ActionTarget{Kind: kind}
			if kind == core.ActionPage {
				target.PageID = "page-id"
			} else {
				target.InstanceID = "instance-id"
			}
			page := &Page{Settings: PageSettings{Layout: PageLayout{Mode: "full"}}, Root: []*core.Node{{ID: "cart", Type: "core.cartIcon", Actions: map[string]core.ActionTarget{"checkout": target}}}}
			calls := 0
			lookup := func(id string) (string, error) {
				calls++
				if id != "page-id" && id != "instance-id" {
					t.Fatalf("不能改写明确引用: %s", id)
				}
				return "/zh/selected-checkout", nil
			}
			compiled, err := Compile(page, checkoutCompileOptions(t, WithCompileMode(CompileModePublish), WithPublishedActionPaths(lookup, lookup))...)
			if err != nil || calls != 1 || !strings.Contains(compiled.HTML, `href="/zh/selected-checkout"`) {
				t.Fatalf("必须输出同工程精确语言的既有路径: calls=%d err=%v", calls, err)
			}
			_, err = Compile(page, checkoutCompileOptions(t, WithCompileMode(CompileModePublish), WithPublishedActionPaths(func(string) (string, error) { return "", nil }, func(string) (string, error) { return "", nil }))...)
			if !errors.Is(err, core.ErrIncompleteNode) {
				t.Fatalf("未发布不得猜路径: %v", err)
			}
			failure := errors.New("跨工程或目标不存在")
			_, err = Compile(page, checkoutCompileOptions(t, WithCompileMode(CompileModePreview), WithPublishedActionPaths(func(string) (string, error) { return "", failure }, func(string) (string, error) { return "", failure }))...)
			if !errors.Is(err, failure) || !errors.Is(err, ErrPublishedActionUnavailable) {
				t.Fatalf("预览不能容忍越界: %v", err)
			}
		})
	}
}

func TestPublishedActionsSnapshotRepeatedTargetsPerCompile(t *testing.T) {
	roots := []*core.Node{{ID: "a", Type: "core.cartIcon", Actions: map[string]core.ActionTarget{"checkout": {Kind: core.ActionPage, PageID: "target"}}}, {ID: "b", Type: "core.cartIcon", Actions: map[string]core.ActionTarget{"checkout": {Kind: core.ActionPage, PageID: "target"}}}}
	reads := 0
	resolver := func(*core.Node, string) (core.ResolvedAction, error) {
		reads++
		return core.ResolvedAction{Kind: core.ActionPage, URL: "/current"}, nil
	}
	if err := configureActions(roots, &core.RenderContext{}, resolver); err != nil || reads != 1 {
		t.Fatalf("单次编译同一引用必须固定同一事实: reads=%d err=%v", reads, err)
	}
	if err := configureActions(roots, &core.RenderContext{}, resolver); err != nil || reads != 2 {
		t.Fatalf("下一次编译不可复用旧路由缓存: reads=%d err=%v", reads, err)
	}
}
