package core

import "testing"

// TestIconInnerStripsWrapper 内部元素必须剥掉外层 <svg> 包裹。
func TestIconInnerStripsWrapper(t *testing.T) {
	inner, ok := IconInner("chevron-right")
	if !ok || inner == "" {
		t.Fatalf("chevron-right 应可取出内部元素")
	}
	if inner[0] == '<' && len(inner) > 4 && inner[:4] == "<svg" {
		t.Fatalf("内部元素不该含 <svg> 包裹: %s", inner[:40])
	}
	full, ok2 := IconSVG("chevron-right")
	if !ok2 || full == "" {
		t.Fatalf("完整 svg 应可取到")
	}
	if !hasSubstr(full, inner) {
		t.Fatalf("内部元素应来自同一份图标库")
	}
}

func hasSubstr(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
