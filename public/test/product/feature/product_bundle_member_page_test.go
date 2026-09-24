// product_bundle_member_page_test.go — 捆绑配置页的「成员来源」面板与解析端点（批次 C 的页面链路）。
//
// 服务端链路（service / dto）已有独立用例；这里钉的是**页面协议**这一层，
// 它最容易被误判为「服务端没问题」却整块失效：
//
//  1. 面板渲染：三种来源、候选勾选清单（仓库 SKU / 属性值）、解析按钮与来源列；
//  2. 解析端点回 JSON（不落库）：成员带来源快照，跳过项逐条带原因与可读文案；
//  3. 保存：表单的**并行数组字段名**与 handler 的 PostFormArray 逐字对齐 ——
//     名称写错一个，来源快照就会静默丢失（保存成功、配置里却没有来源）。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/internal/templates"
)

// memberPanelExternalSKU 本文件里登记的外部编码（数据，不是文案）。
const memberPanelExternalSKU = "EXT-10"

// newBundleMemberPageEngine 装配捆绑配置页 + 保存 + 成员来源解析三个端点（真实 Jet 模板 + 真实 service）。
//
// 与既有 newBundlePageEngine 的差别只有一处：注入库存契约（仓库下拉与仓库 SKU 候选要用它），
// 未注入时面板的仓库来源整块降级为空 —— 那正是这条用例要排除的形态。
func newBundleMemberPageEngine(t *testing.T) (*gin.Engine, *bundleFixture) {
	t.Helper()
	f := newBundleFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products/bundle", handle.ProductBundlePage)
	engine.POST("/admin/products/bundle/save", handle.ProductBundleSave)
	engine.POST("/admin/products/bundle/members/resolve", handle.ProductsBundleMembersResolve)
	return engine, f
}

// getPage 取页面 HTML（失败即测试失败）。
func getPage(t *testing.T, engine *gin.Engine, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应 200，实际 %d", rec.Code)
	}
	return rec.Body.String()
}

// jsonMap 把响应体解成 map（断言只认协议里的键名，不引一整套匿名结构体）。
func jsonMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（%s）", err, string(body))
	}
	return out
}

// asMap 取 map 里的一层对象（缺失即测试失败）。
func asMap(t *testing.T, value any, what string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s 应是对象，实际 %#v", what, value)
	}
	return m
}

// asList 取 map 里的一层数组（缺失即测试失败）。
func asList(t *testing.T, value any, what string) []any {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s 应是数组，实际 %#v", what, value)
	}
	return list
}

// TestBundleMemberSourcePanelRenders 面板渲染：三种来源、来源列与解析按钮都在。
func TestBundleMemberSourcePanelRenders(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 120.0
	bundle := f.mkProduct(t, "面板套餐", "member-panel-bundle", &price)
	body := getPage(t, engine, "/admin/products/bundle?project="+f.projectID+"&product="+bundle.ID)
	for _, want := range []string{
		"成员来源",
		"从商品导入（该商品的启用变体）",
		"从仓库选（按仓挑选仓库 SKU）",
		"自选属性值组合（服务端重算笛卡尔积）",
		"来源",
		"解析并追加成员",
		"移除",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("成员来源面板缺少 %q", want)
		}
	}
	for _, want := range []string{
		"name=\"source\"",
		"data-bundle-resolve",
		"data-url=\"/admin/products/bundle/members/resolve\"",
		"name=\"sourceKind\"",
		"name=\"memberWarehouseId\"",
		"name=\"memberWarehouseSku\"",
		"name=\"memberExternalSku\"",
		"data-bundle-member-tpl",
		"data-bundle-member-list",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("成员来源面板缺少 %q", want)
		}
	}
}

// TestBundleMemberSourcePanelCandidates 候选清单由服务端给出：
// 仓库来源列该仓的仓库 SKU；属性来源列来源商品参与变体的属性值。
func TestBundleMemberSourcePanelCandidates(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 130.0
	bundle := f.mkProduct(t, "候选套餐", "member-cand-bundle", &price)
	item := f.mkProduct(t, "候选子项", "member-cand-item", nil)
	v := f.firstVariant(t, item.ID)
	// 在默认仓登记一条**另一个编码**的仓库 SKU（(仓库, 变体) 一行；SKU 仓内唯一）。
	if _, err := f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		ProductID: item.ID, VariantID: v.ID, SKUCode: "PANEL-SKU-9",
	}); err != nil {
		t.Fatalf("登记仓库 SKU 失败: %v", err)
	}
	// 上面的 EnsureStock 会命中既有行（同一个变体在同一个仓只有一行），
	// 因此再取一次该行真实的 sku_code 作为勾选清单的期望值。
	rows, err := f.inventory.ListWarehouseSKUs(ctx, &inventorydto.ListWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: f.warehouse.ID, Size: 50,
	})
	if err != nil || len(rows) == 0 {
		t.Fatalf("读仓库 SKU 列表失败: %v", err)
	}
	panelSKU := rows[0].SKUCode

	// 仓库来源：勾选清单里出现该仓的仓库 SKU。
	body := getPage(t, engine, "/admin/products/bundle?project="+f.projectID+"&product="+bundle.ID+
		"&source="+productenums.BundleSourceWarehouse+"&sourceWarehouse="+f.warehouse.ID)
	if !strings.Contains(body, "name=\"warehouseSku\"") || !strings.Contains(body, "value=\""+panelSKU+"\"") {
		t.Fatalf("仓库来源应列出该仓的仓库 SKU %s，实际 %s", panelSKU, oneLine(body))
	}

	// 属性来源：来源商品的属性值以 attr:<属性组 id> 勾选（与组合生成抽屉同一套字段名）。
	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
		Values: []productdto.AttributeValueReq{
			{Label: "红", Key: "red"}, {Label: "蓝", Key: "blue"},
		},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	srcPrice := 9.0
	src, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "属性来源商品", Slug: "member-attr-src",
		AttributeIDs: []string{color.ID}, DefaultPrice: &srcPrice,
	})
	if err != nil {
		t.Fatalf("建来源商品失败: %v", err)
	}
	body = getPage(t, engine, "/admin/products/bundle?project="+f.projectID+"&product="+bundle.ID+
		"&source="+productenums.BundleSourceAttributes+"&sourceProduct="+src.ID)
	if !strings.Contains(body, "name=\"attr:"+color.ID+"\"") || !strings.Contains(body, "红") {
		t.Fatalf("属性来源应列出来源商品的属性值，实际 %s", oneLine(body))
	}
	// 未选来源商品时给一句可行动的提示（而不是空白）。
	body = getPage(t, engine, "/admin/products/bundle?project="+f.projectID+"&product="+bundle.ID+
		"&source="+productenums.BundleSourceAttributes)
	if !strings.Contains(body, "先选一个来源商品") {
		t.Fatalf("未选来源商品应给提示，实际 %s", oneLine(body))
	}
}

// TestBundleMemberSourceResolveEndpoint 解析端点：回 JSON、带来源快照、跳过项逐条带原因；
// 保存（并行数组）把来源快照落库并可回显 —— 字段名对不上就会在这里炸。
func TestBundleMemberSourceResolveEndpoint(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 140.0
	bundle := f.mkProduct(t, "解析套餐", "member-resolve-bundle", &price)
	item := f.mkProduct(t, "解析子项", "member-resolve-item", nil)
	v := f.firstVariant(t, item.ID)

	// 解析：一条命中（默认仓里那条真实的仓库 SKU）+ 一条不存在（逐条原因），响应是 JSON。
	// 仓库侧那条编码是**裸码**（不带仓码前缀，docs/14 §1.1）：解析端点按 (仓, 裸码) 定位，
	// 传商品侧的带前缀编码会定位不到（那是另一条编码）。所以这里直接读真源里那一行的值。
	stockRows, lerr := f.inventory.ListWarehouseSKUs(ctx, &inventorydto.ListWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: f.warehouse.ID, Size: 50,
	})
	if lerr != nil || len(stockRows) == 0 {
		t.Fatalf("读仓库 SKU 失败: %v", lerr)
	}
	// 取**这个变体**那一条：同一个仓里还有别的商品的货，随便取一条可能取到别的变体
	//（解析出来会指向另一个商品，断言会失败在一个与协议无关的地方）。
	warehouseSKU := ""
	for _, row := range stockRows {
		if row.VariantID == v.ID {
			warehouseSKU = row.SKUCode
			break
		}
	}
	if warehouseSKU == "" {
		t.Fatalf("该仓里没有变体 %s 的货（创建商品时应自动建行）", v.ID)
	}
	rec := postForm(engine, "/admin/products/bundle/members/resolve", url.Values{
		"projectId":       {f.projectID},
		"productId":       {bundle.ID},
		"source":          {productenums.BundleSourceWarehouse},
		"sourceWarehouse": {f.warehouse.ID},
		"warehouseSku":    {warehouseSKU, "NOPE-10"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("解析应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	payload := jsonMap(t, rec.Body.Bytes())
	if payload["ok"] != true {
		t.Fatalf("解析应回 ok=true，实际 %+v", payload)
	}
	members := asList(t, payload["members"], "members")
	skips := asList(t, payload["skips"], "skips")
	if len(members) != 1 || len(skips) != 1 {
		t.Fatalf("应 1 成功 / 1 跳过，实际 members=%d skips=%d", len(members), len(skips))
	}
	m := asMap(t, members[0], "members[0]")
	// 成员身份 = 变体；skuCode 是**商品侧**编码（带认领仓前缀），
	// warehouseSku 是**仓库侧**裸码（来源快照）。两者刻意不同：前缀只属于商品侧。
	if m["variantId"] != v.ID || m["skuCode"] != v.SKUCode {
		t.Fatalf("成员身份应是那条货的变体：%+v", m)
	}
	// 来源快照在响应里是**平铺**的（与页面 JS 的取值一致）：sourceKind / warehouseId /
	// warehouseSku / externalSku。
	if m["sourceKind"] != productenums.BundleSourceWarehouse || m["warehouseId"] != f.warehouse.ID ||
		m["warehouseSku"] != warehouseSKU {
		t.Fatalf("成员来源快照不对：%+v", m)
	}
	skip := asMap(t, skips[0], "skips[0]")
	if skip["reason"] != productenums.BundleMemberWarehouseSKUMissing {
		t.Fatalf("跳过项应带原因 key，实际 %+v", skip)
	}
	if text, _ := skip["text"].(string); strings.TrimSpace(text) == "" {
		t.Fatalf("跳过项应带可读文案，实际 %+v", skip)
	}

	// 保存：并行数组（含四个来源字段）→ 落库并可回显。
	written := postForm(engine, "/admin/products/bundle/save", url.Values{
		"productId":          {bundle.ID},
		"maxOptions":         {"20"},
		"minTotalQty":        {"1"},
		"maxTotalQty":        {"0"},
		"variantId":          {v.ID},
		"required":           {"1"},
		"defaultQty":         {"1"},
		"minQty":             {"1"},
		"maxQty":             {"0"},
		"sourceKind":         {productenums.BundleSourceWarehouse},
		"memberWarehouseId":  {f.warehouse.ID},
		"memberWarehouseSku": {"PANEL-SKU-10"},
		"memberExternalSku":  {memberPanelExternalSKU},
	})
	if written.Code != http.StatusFound {
		t.Fatalf("保存应 302，实际 %d：%s", written.Code, written.Body.String())
	}
	detail, err := f.products.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{
		ProductID: bundle.ID, ProjectID: f.projectID,
	})
	if err != nil {
		t.Fatalf("读回配置失败: %v", err)
	}
	if len(detail.Options) != 1 {
		t.Fatalf("应落 1 个成员，实际 %d", len(detail.Options))
	}
	got := detail.Options[0]
	if got.SourceKind != productenums.BundleSourceWarehouse || got.WarehouseID != f.warehouse.ID ||
		got.WarehouseSKU != "PANEL-SKU-10" || got.ExternalSKU != memberPanelExternalSKU {
		t.Fatalf("来源快照应从表单落库：%+v", got.BundleOption)
	}
	// 回显：配置页把来源标签渲染进成员表的来源列。
	page := getPage(t, engine, "/admin/products/bundle?project="+f.projectID+"&product="+bundle.ID)
	if !strings.Contains(page, "从仓库选 · 仓库 SKU PANEL-SKU-10 · 外部编码 "+memberPanelExternalSKU) {
		t.Fatalf("配置页应回显成员来源标签，实际 %s", oneLine(page))
	}

	// 属性来源的组合不存在时：JSON 里逐条说明原因（页面据此提示先去生成变体）。
	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "口味", Key: "taste",
		Values: []productdto.AttributeValueReq{{Label: "香草", Key: "vanilla"}, {Label: "巧克力", Key: "choco"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	srcPrice := 11.0
	tasteSrc, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "口味商品", Slug: "member-attr-taste",
		AttributeIDs: []string{color.ID}, DefaultPrice: &srcPrice,
	})
	if err != nil {
		t.Fatalf("建来源商品失败: %v", err)
	}
	missing := postForm(engine, "/admin/products/bundle/members/resolve", url.Values{
		"projectId":        {f.projectID},
		"productId":        {bundle.ID},
		"source":           {productenums.BundleSourceAttributes},
		"sourceProductId":  {tasteSrc.ID},
		"attr:" + color.ID: {color.Values[0].ID},
	})
	if missing.Code != http.StatusOK {
		t.Fatalf("属性来源解析应 200，实际 %d：%s", missing.Code, missing.Body.String())
	}
	body := missing.Body.String()
	if !strings.Contains(body, productenums.BundleMemberNotOnProduct) || !strings.Contains(body, "没有对应变体") {
		t.Fatalf("不存在的组合应逐条说明原因，实际 %s", body)
	}
}
