// Package feature product 模块 feature 测试 —— 按变体查可用量（issue #24）。
//
// 覆盖访问面依赖的那条链路：变体 id（静态产物里烘的值）→ product 反查工程 → inventory 真源。
// 片段层只做渲染，工程上下文与真源读取都在这里落定。
package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// stubAvailabilityPort 记录被查询工程的桩端口（替代 inventory 实现）。
type stubAvailabilityPort struct {
	data  map[string]int
	calls []string
}

// AvailableQuantities 实现 productcontract.VariantAvailabilityPort。
func (s *stubAvailabilityPort) AvailableQuantities(_ context.Context, projectID string, variantIDs []string) (map[string]int, error) {
	s.calls = append(s.calls, projectID)
	out := map[string]int{}
	for _, id := range variantIDs {
		if n, ok := s.data[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// TestVariantAvailabilitiesLookup 验收（issue #24）：
// 降级契约（端口未注入返回空结果不报错）+ 正常链路（去重后按变体反查到的工程查一次）。
func TestVariantAvailabilitiesLookup(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	pid := f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	detail, derr := f.products.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: pid})
	if derr != nil {
		t.Fatalf("读商品失败: %v", derr)
	}
	if len(detail.Variants) < 2 {
		t.Fatalf("夹具应建出 2 个变体，实际 %d", len(detail.Variants))
	}
	v1, v2 := detail.Variants[0].ID, detail.Variants[1].ID

	// 降级：未注入可用量端口（inventory 未装配 / 纯商品单测）→ 空结果且不报错。
	// 片段层据此渲染「以结算时库存为准」；静态页面不该因为读不到库存而 500。
	got, err := f.products.VariantAvailabilities(ctx, []string{v1})
	if err != nil {
		t.Fatalf("端口未注入不该报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("端口未注入应返回空结果，实际 %+v", got)
	}

	// 正常链路：注入桩端口，去重后按工程查一次。
	port := &stubAvailabilityPort{data: map[string]int{v1: 7}}
	f.products.SetAvailabilityPort(port)
	got, err = f.products.VariantAvailabilities(ctx, []string{v1, v1, " ", v2, "does-not-exist"})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got[v1] != 7 {
		t.Fatalf("v1 可用量应为 7，实际 %+v", got)
	}
	if len(port.calls) != 1 {
		t.Fatalf("同一工程的多个变体应合并成一次查询，实际 %d 次（%+v）", len(port.calls), port.calls)
	}
	if port.calls[0] != f.projectID {
		t.Fatalf("应按变体反查到的工程查询，期望 %s，实际 %q", f.projectID, port.calls[0])
	}
	// 桩里没有的变体（含未知 id）不出现：调用方按「未知」渲染兜底文案，
	// 而不是把未知说成缺货。
	if _, ok := got[v2]; ok {
		t.Fatalf("桩里没有的变体不该凭空出现: %+v", got)
	}
	if _, ok := got["does-not-exist"]; ok {
		t.Fatalf("未知 id 不该出现在结果里: %+v", got)
	}
}
