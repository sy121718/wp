package core

import "testing"

func TestIconSVGClassInjectsClass(t *testing.T) {
	svg, ok := IconSVGClass("chevron-right", "my-arrow")
	if !ok {
		t.Fatal("chevron-right 应可取到")
	}
	for _, want := range []string{`class="my-arrow"`, `aria-hidden="true"`, `viewBox="0 0 24 24"`, `stroke="currentColor"`, "<path"} {
		if !hasSubstr(svg, want) {
			t.Fatalf("svg 缺少 %q: %s", want, svg)
		}
	}
	if hasSubstr(svg, "<svg class=\"\"") {
		t.Fatalf("不该有空 class: %s", svg)
	}
}
