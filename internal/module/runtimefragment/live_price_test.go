package runtimefragment

// live_price_test.go — 商品实时价格核对片段单测（BIZ-2）。
//
// 覆盖四类结论：价已变（提示当前价）/ 价一致（沉默）/ 规格已下架（提示）/ 无从对比（沉默），
// 外加端口未接入、参数为空、非法 id 形状三条边界 —— 后三者都必须是**空片段**，
// 而不是报错或 500。

import (
	"context"
	"errors"
	"strings"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
)

// 变体 id 用真实 uuid 形状：片段层会丢弃非 uuid 形状的 id（否则 PostgreSQL 的
// uuid 列解析会报 22P02，页面直接 500），所以测试数据必须与线上形状一致。
const (
	liveVariantA = "11111111-1111-1111-1111-111111111111"
	liveVariantB = "22222222-2222-2222-2222-222222222222"
)

// stubSnapshots 变体快照桩：只回声请求里出现的那些 id（与真实实现「查不到就不出现」同形），
// 并记录实际传给端口的 id 列表（用于断言非法 id 没有流进去）。
type stubSnapshots struct {
	all  []*productcontract.VariantSnapshot
	err  error
	seen []string
}

func (s *stubSnapshots) VariantSnapshots(_ context.Context, ids []string) ([]*productcontract.VariantSnapshot, error) {
	s.seen = ids
	if s.err != nil {
		return nil, s.err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]*productcontract.VariantSnapshot, 0, len(ids))
	for _, snap := range s.all {
		if want[snap.VariantID] {
			out = append(out, snap)
		}
	}
	return out, nil
}

// TestRenderProductLivePriceChanged 构建期价与当前价不一致 → 明确交代当前价。
func TestRenderProductLivePriceChanged(t *testing.T) {
	SetVariantSnapshotProvider(&stubSnapshots{all: []*productcontract.VariantSnapshot{
		{VariantID: liveVariantA, Price: 12900, Enabled: true},
	}})
	defer SetVariantSnapshotProvider(nil)

	out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA, "prices": "9900", "currency": "币",
	}})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// 12900 分 = 129 元；格式与商品字段解析器同一口径（不补两位小数）。
	if !strings.Contains(out, "价格已更新为 币129，以结算为准") {
		t.Fatalf("缺少价格变更提示:\n%s", out)
	}
	if !strings.Contains(out, "data-variant-id=") {
		t.Fatalf("应回声变体 id 便于前端定位:\n%s", out)
	}
}

// TestRenderProductLivePriceUnchanged 价格一致时不说话：目标节点留空，页面保留产物里的价。
func TestRenderProductLivePriceUnchanged(t *testing.T) {
	SetVariantSnapshotProvider(&stubSnapshots{all: []*productcontract.VariantSnapshot{
		{VariantID: liveVariantA, Price: 9900, Enabled: true},
	}})
	defer SetVariantSnapshotProvider(nil)

	out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA, "prices": "9900",
	}})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("价格一致时不该输出任何内容，实际: %q", out)
	}
}

// TestRenderProductLivePriceFallbacks 四种「说不清」的情况都要沉默且不报错。
func TestRenderProductLivePriceFallbacks(t *testing.T) {
	// ① 端口未接入。
	SetVariantSnapshotProvider(nil)
	if out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA, "prices": "9900",
	}}); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("端口未接入应沉默且不报错，out=%q err=%v", out, err)
	}
	// ② 参数为空。
	if out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{}}); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("无参数应沉默且不报错，out=%q err=%v", out, err)
	}
	// ③ 非 uuid 形状的 id：入口丢弃，绝不能流进 uuid 列的查询。
	stub := &stubSnapshots{}
	SetVariantSnapshotProvider(stub)
	defer SetVariantSnapshotProvider(nil)
	if out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": "not-a-uuid,12345", "prices": "9900,9900",
	}}); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("非法 id 形状应沉默，out=%q err=%v", out, err)
	}
	if len(stub.seen) != 0 {
		t.Fatalf("非法 id 不得传给端口（会让 PG 报 22P02 打成 500），实际传了 %+v", stub.seen)
	}
	// ④ 没烘构建期价（prices 缺失）→ 无从对比，沉默（展示当前价会让页面上出现两个价）。
	SetVariantSnapshotProvider(&stubSnapshots{all: []*productcontract.VariantSnapshot{
		{VariantID: liveVariantA, Price: 12900, Enabled: true},
	}})
	if out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA,
	}}); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("无构建期价应沉默，out=%q err=%v", out, err)
	}
	// ⑤ 空请求。
	if _, err := renderProductLivePrice(context.Background(), nil); err == nil {
		t.Fatalf("空请求应报错（端点转 400）")
	}
}

// TestRenderProductLivePriceDisabled 已下架是产物里没有的事实，独立提示。
func TestRenderProductLivePriceDisabled(t *testing.T) {
	SetVariantSnapshotProvider(&stubSnapshots{all: []*productcontract.VariantSnapshot{
		{VariantID: liveVariantA, Price: 9900, Enabled: false},
	}})
	defer SetVariantSnapshotProvider(nil)

	out, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA, "prices": "9900",
	}})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "该规格已下架，以结算为准") {
		t.Fatalf("缺少下架提示:\n%s", out)
	}
}

// TestRenderProductLivePricePortError 端口报错要上抛（端点转 500，而不是静默假装价格一致）。
func TestRenderProductLivePricePortError(t *testing.T) {
	SetVariantSnapshotProvider(&stubSnapshots{err: errors.New("数据库不可用")})
	defer SetVariantSnapshotProvider(nil)
	if _, err := renderProductLivePrice(context.Background(), &Request{Params: map[string]string{
		"variantIds": liveVariantA, "prices": "9900",
	}}); err == nil {
		t.Fatalf("端口报错应上抛")
	}
}

// TestLivePricePairsKeepsPosition 非法 id 被丢弃时，**价格与 id 的位置配对不能错位**。
//
// 这是本片段最容易写错的一处：prices 与 variantIds 是同序的两个数组，
// 过滤掉一个 id 却把它的价格留下，后面每个变体都会拿到上一个人的价 ——
// 页面不报错，只是所有提示都是假的。
func TestLivePricePairsKeepsPosition(t *testing.T) {
	pairs := livePricePairs(&Request{Params: map[string]string{
		"variantIds": "not-a-uuid," + liveVariantA + "," + liveVariantB,
		"prices":     "1111,2222,3333",
	}})
	if len(pairs) != 2 {
		t.Fatalf("应保留 2 个合法 id，实际 %+v", pairs)
	}
	if pairs[0].id != liveVariantA || !pairs[0].hasPrice || pairs[0].declared != 2222 {
		t.Fatalf("第一个变体应拿到自己那份价 2222，实际 %+v", pairs[0])
	}
	if pairs[1].id != liveVariantB || !pairs[1].hasPrice || pairs[1].declared != 3333 {
		t.Fatalf("第二个变体应拿到自己那份价 3333，实际 %+v", pairs[1])
	}
	// 价格数组短于 id 时，多出来的 id 视为「没给价」。
	pairs = livePricePairs(&Request{Params: map[string]string{
		"variantIds": liveVariantA + "," + liveVariantB, "prices": "9900",
	}})
	if len(pairs) != 2 || pairs[0].declared != 9900 || pairs[1].hasPrice {
		t.Fatalf("价格数组短的项应视为未提供，实际 %+v", pairs)
	}
}

// TestSplitDeclaredPrices 构建期价格解析：非法项标记为「没给」，不影响同批其他项。
func TestSplitDeclaredPrices(t *testing.T) {
	got := splitDeclaredPrices(" 9900 , ,abc,1200")
	if len(got) != 4 {
		t.Fatalf("解析长度不符: %+v", got)
	}
	if cents, ok := declaredPriceAt(got, 0); !ok || cents != 9900 {
		t.Fatalf("第 0 项应为 9900，实际 %d ok=%v", cents, ok)
	}
	for _, i := range []int{1, 2} {
		if _, ok := declaredPriceAt(got, i); ok {
			t.Fatalf("第 %d 项应视为未提供", i)
		}
	}
	if cents, ok := declaredPriceAt(got, 3); !ok || cents != 1200 {
		t.Fatalf("第 3 项应为 1200，实际 %d ok=%v", cents, ok)
	}
	if _, ok := declaredPriceAt(got, 9); ok {
		t.Fatalf("越界索引应返回 false")
	}
}

// TestCentsToYuanText 与商品字段解析器 formatPrice 同一口径（99.50 元读作 "99.5"）。
func TestCentsToYuanText(t *testing.T) {
	for _, tc := range []struct {
		cents int64
		want  string
	}{{0, "0"}, {9900, "99"}, {9950, "99.5"}, {12999, "129.99"}} {
		if got := centsToYuanText(tc.cents); got != tc.want {
			t.Errorf("centsToYuanText(%d) = %q, want %q", tc.cents, got, tc.want)
		}
	}
}
