package runtimefragment

// variant_availability_test.go — 商品变体可用量片段单测（issue #24）。
//
// 覆盖四类结论：充足 / 少量 / 缺货 / 未知（降级），以及参数缺失与 provider 未接入两种边界。

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubAvailability 固定可用量映射的桩（替代 product → inventory 的端口链）。
type stubAvailability struct {
	avail       map[string]int
	err         error
	seen        []string
	seenProject string
}

func (s *stubAvailability) VariantAvailabilities(_ context.Context, projectID string, ids []string) (map[string]int, error) {
	s.seenProject = projectID
	s.seen = ids
	if s.err != nil {
		return nil, s.err
	}
	return s.avail, nil
}

// projectID 测试用的工程 id（片段参数形状要求合法 uuid）。
const projectID = "3f0b1c62-9d5a-4a1e-8f77-2c1d0e5a7b41"

// TestRenderVariantAvailability 四种结论各出现一次，且按请求顺序输出。
func TestRenderVariantAvailability(t *testing.T) {
	stub := &stubAvailability{avail: map[string]int{"v1": 9, "v2": 3, "v3": 0}}
	deps.VariantAvailabilityProvider = stub
	defer func() { deps.VariantAvailabilityProvider = nil }()

	out, err := renderVariantAvailability(context.Background(), &Request{
		Type:   "productVariantAvailability",
		Params: map[string]string{"variantIds": "v1, v2,v3,v4", "projectId": projectID},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// 工程作用域由片段参数给出（DB-009）：端口必须收到它，否则库存真源查不到。
	if stub.seenProject != projectID {
		t.Fatalf("工程作用域应透传给端口，实际 %q", stub.seenProject)
	}
	// 去重与去空在片段侧也做一遍（参数可能来自手写 URL）。
	if len(stub.seen) != 4 || stub.seen[0] != "v1" || stub.seen[3] != "v4" {
		t.Fatalf("传给端口的 id 列表不符: %+v", stub.seen)
	}
	for _, want := range []string{"库存充足", "仅剩 3 件", "暂时缺货", "以结算时库存为准"} {
		if !strings.Contains(out, want) {
			t.Fatalf("片段缺少结论 %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, `class="sky-variant-stock-msg is-out"`) {
		t.Fatalf("缺货项应带 is-out 标记（样式与读屏据此区分）\n%s", out)
	}
	if !strings.Contains(out, `data-variant-id="v3"`) {
		t.Fatalf("应回声变体 id 便于前端定位\n%s", out)
	}
}

// TestRenderVariantAvailabilityDegraded 端口未接入：整体降级为「以结算时库存为准」，不报错。
func TestRenderVariantAvailabilityDegraded(t *testing.T) {
	deps.VariantAvailabilityProvider = nil
	out, err := renderVariantAvailability(context.Background(), &Request{
		Params: map[string]string{"variantIds": "v1", "projectId": projectID},
	})
	if err != nil {
		t.Fatalf("端口未接入不应报错（静态页仍要可读）: %v", err)
	}
	if !strings.Contains(out, "以结算时库存为准") {
		t.Fatalf("应给出降级文案: %s", out)
	}
}

// TestRenderVariantAvailabilityErrors 参数缺失与端口报错都要上抛（片段端点转 400/500）。
func TestRenderVariantAvailabilityErrors(t *testing.T) {
	deps.VariantAvailabilityProvider = nil
	defer func() { deps.VariantAvailabilityProvider = nil }()
	if _, err := renderVariantAvailability(context.Background(), &Request{Params: map[string]string{}}); err == nil {
		t.Fatalf("缺参数应报错")
	}
	if _, err := renderVariantAvailability(context.Background(), nil); err == nil {
		t.Fatalf("空请求应报错")
	}
	// 只有 variantIds、没有 projectId：DB-009 之后工程 id 是必填参数（与 productList 同口径）。
	if _, err := renderVariantAvailability(context.Background(), &Request{
		Params: map[string]string{"variantIds": "v1"},
	}); err == nil {
		t.Fatalf("缺 projectId 应报错")
	}
	deps.VariantAvailabilityProvider = &stubAvailability{err: errors.New("数据库不可用")}
	if _, err := renderVariantAvailability(context.Background(), &Request{
		Params: map[string]string{"variantIds": "v1", "projectId": projectID},
	}); err == nil {
		t.Fatalf("端口报错应上抛")
	}
}

// TestSplitVariantIDs 去空、去重、限量。
func TestSplitVariantIDs(t *testing.T) {
	got := splitVariantIDs(" a , ,a,b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("解析结果不符: %+v", got)
	}
	if ids := splitVariantIDs(strings.Repeat("x,", variantAvailabilityMaxIDs+10)); len(ids) != 1 {
		t.Fatalf("重复值应被去重成 1 个，实际 %d", len(ids))
	}
}
