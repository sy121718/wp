package core

import (
	"errors"
	"testing"
)

// stubResolver 固定返回值的 ContentResolver（页面级绑定）。
type stubResolver struct {
	value string
	err   error
}

func (s stubResolver) ResolveString(string) (string, error) { return s.value, s.err }

// TestItemScope 集合项作用域：item.* 取当前项，其余原样委托内层解析器。
func TestItemScope(t *testing.T) {
	item := map[string]any{"title": "卡片标题", "price": 299.0, "images": []any{"a.jpg", "b.jpg"}}
	scope := ItemScope{Inner: stubResolver{value: "页面值"}, Item: item}

	if got, _ := scope.ResolveString("item.title"); got != "卡片标题" {
		t.Errorf("item.title 应取当前项，got %q", got)
	}
	if got, _ := scope.ResolveString("item.price"); got != "299" {
		t.Errorf("整数数值应去掉小数尾巴，got %q", got)
	}
	if got, _ := scope.ResolveString("post.title"); got != "页面值" {
		t.Errorf("非 item.* 应委托内层解析器，got %q", got)
	}

	// 不在集合里：item.* 解析为空串（由组件 fallback 兜底），不报错。
	if got, err := (&ItemScope{}).ResolveString("item.title"); got != "" || err != nil {
		t.Errorf("无作用域时 item.* 应为空串且无错，got %q / %v", got, err)
	}
	// 内层未注入：页面绑定解析为空串。
	if got, err := (&ItemScope{}).ResolveString("post.title"); got != "" || err != nil {
		t.Errorf("无内层解析器时应为空串且无错，got %q / %v", got, err)
	}
	// 内层错误照常透传。
	wantErr := errors.New("解析失败")
	if _, err := (&ItemScope{Inner: stubResolver{err: wantErr}}).ResolveString("post.title"); !errors.Is(err, wantErr) {
		t.Errorf("内层错误应透传，got %v", err)
	}
}

// TestItemFieldText 集合项字段取值：数组取首元素、缺失字段为空、布尔与未知类型兜底。
func TestItemFieldText(t *testing.T) {
	item := map[string]any{
		"images": []any{"a.jpg", "b.jpg"},
		"empty":  []any{},
		"flag":   true,
		"off":    false,
		"count":  12.5,
	}
	cases := []struct {
		field, want string
	}{
		{"images", "a.jpg"},
		{"empty", ""},
		{"flag", "是"},
		{"off", "否"},
		{"count", "12.5"},
		{"missing", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ItemFieldText(item, c.field); got != c.want {
			t.Errorf("ItemFieldText(%q) = %q，期望 %q", c.field, got, c.want)
		}
	}
	if got := ItemFieldText(nil, "title"); got != "" {
		t.Errorf("nil 项应返回空串，got %q", got)
	}
}
