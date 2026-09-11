package product

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// propsOf 把 props 键值编码为节点 props JSON（避免测试里手写 JSON 字符串）。
func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

// nodeOf 构造商品组件节点。
func nodeOf(t *testing.T, kv map[string]any) *core.Node {
	t.Helper()
	return &core.Node{ID: "p1", Type: Type, Props: propsOf(t, kv)}
}

// decodePropsOf 解码 props 为组件 Props。
func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

// stubResolver 固定字段值映射的解析器（替代构建期的商品解析器）。
type stubResolver struct {
	values map[string]string
	errOn  string
}

// ResolveString 实现 core.ContentResolver。
func (s stubResolver) ResolveString(field string) (string, error) {
	if field == s.errOn {
		return "", errors.New("越界字段")
	}
	return s.values[field], nil
}

// TestValidateRequiresField 未声明任何商品字段（全空槽位）必须被拒绝。
func TestValidateRequiresField(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "至少需要声明一个商品字段") {
		t.Fatalf("空槽位应被拒绝: %v", err)
	}
}

// TestValidateRejectsForeignSource 声明其它数据源的字段必须被拒绝。
func TestValidateRejectsForeignSource(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "article.title"}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "不属于数据源") {
		t.Fatalf("跨数据源字段应被拒绝: %v", err)
	}
}

// TestValidateRejectsBadPath 非法字段路径必须被拒绝。
func TestValidateRejectsBadPath(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "product."}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "无效的字段路径") {
		t.Fatalf("非法字段路径应被拒绝: %v", err)
	}
}

// TestValidateAcceptsDeclaredFields 合法声明通过校验（槽位留空 = 不需要该字段）。
func TestValidateAcceptsDeclaredFields(t *testing.T) {
	node := nodeOf(t, map[string]any{
		"titleField":   "product.name",
		"priceField":   "product.priceRange",
		"galleryField": "product.images",
	})
	if err := Widget.Validate(node, map[string]bool{}); err != nil {
		t.Fatalf("合法声明应通过: %v", err)
	}
}

// TestFieldBindingsReportsDeclared 组件自报声明的字段绑定（供注册表白名单校验）。
func TestFieldBindingsReportsDeclared(t *testing.T) {
	node := nodeOf(t, map[string]any{
		"titleField":       "product.name",
		"descriptionField": "product.description",
	})
	refs, err := Widget.FieldBindings(node)
	if err != nil {
		t.Fatalf("自报字段绑定失败: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("应自报 2 条绑定，实际 %d: %+v", len(refs), refs)
	}
	for _, ref := range refs {
		if ref.EntityType != "product" || ref.Field == "" {
			t.Fatalf("绑定应为 product.<字段>: %+v", ref)
		}
	}
}

// TestBuildViewRendersDeclaredFields 声明字段经解析器静态填入视图：价格带货币符号、
// 图集解析 JSON 数组、描述走富文本清洗。
func TestBuildViewRendersDeclaredFields(t *testing.T) {
	images, err := json.Marshal([]string{"/storage/a.jpg", "/storage/b.jpg"})
	if err != nil {
		t.Fatalf("图集编码失败: %v", err)
	}
	p := decodePropsOf(t, map[string]any{
		"titleField":        "product.name",
		"subtitleField":     "product.subtitle",
		"mediaField":        "product.defaultImage",
		"galleryField":      "product.images",
		"priceField":        "product.priceRange",
		"comparePriceField": "product.comparePrice",
		"descriptionField":  "product.description",
	})
	resolver := stubResolver{values: map[string]string{
		"product.name":         "夏季衬衫",
		"product.subtitle":     "轻薄透气",
		"product.defaultImage": "/storage/a.jpg",
		"product.images":       string(images),
		"product.priceRange":   "99 ~ 199",
		"product.comparePrice": "259",
		"product.description":  "<p>纯棉</p>",
	}}
	view, err := BuildView(&p, resolver)
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if !view.HasTitle || view.Title != "夏季衬衫" {
		t.Fatalf("标题未填入: %+v", view)
	}
	if view.Price != "¥99 ~ 199" || view.ComparePrice != "¥259" {
		t.Fatalf("价格未按货币符号填入: %q / %q", view.Price, view.ComparePrice)
	}
	if len(view.Gallery) != 2 || view.TitleTag != "h2" {
		t.Fatalf("图集 / 标题层级不符: %+v", view)
	}
	if !view.HasDescription || !strings.Contains(view.DescriptionHTML, "纯棉") {
		t.Fatalf("描述未填入: %+v", view)
	}
}

// TestBuildViewRequiresResolver 声明了字段却没有解析器时必须报错（不静默出空块）。
func TestBuildViewRequiresResolver(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.name"})
	if _, err := BuildView(&p, nil); err == nil {
		t.Fatal("缺少内容解析器应报错")
	}
}

// TestBuildViewPropagatesResolveError 解析器报错（越界字段）必须上抛终止构建。
func TestBuildViewPropagatesResolveError(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.bogus"})
	_, err := BuildView(&p, stubResolver{errOn: "product.bogus"})
	if err == nil || !strings.Contains(err.Error(), "product.bogus") {
		t.Fatalf("解析失败应上抛: %v", err)
	}
}
