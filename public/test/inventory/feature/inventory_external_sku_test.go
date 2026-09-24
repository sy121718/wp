// Package feature inventory 模块 feature 测试 —— 仓库 SKU 外部编码映射（迁移 251）。
//
// 口径（docs/14-product-sku-and-cost-model.md §9.3，2026-09-19 用户补充确认）：
// 属性属于**商品**，仓库侧只回答「这条货在这个仓叫什么」—— 那就是本列的语义。
// 映射是 **N:1**（多个 variant → 一个外部码）：
//
//	· 同一个 external_sku 在同一仓库下**允许**挂在同一商品的多个变体上
//	  （十几个口味在仓库侧共用一条 SKU / 一个价格就是这个形态），且按它可查询到多行；
//	· 跨两个不同**商品**时被拒（ErrExternalSKUProductConflict）—— 弱校验在 service，
//	  DDL 上**没有**唯一索引（把 N:1 写成 1:1 会把合法数据判成冲突）；
//	· 空串不参与校验（= 该仓用我们自己的 SKU）；
//	· UNIQUE (warehouse_id, sku_code)（我们自己的 SKU 仓内唯一，迁移 244）保持不动。
//
// 断言一律直查真源列（inventory_stocks.external_sku / pg_indexes / pg_constraint / sys_i18n），
// 不走 service 自己返回的响应 —— 那只能证明「service 以为自己写了」。
package feature

import (
	"context"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// externalSKUOf 直读真源里的外部编码（空串 = 该仓用我们自己的 SKU）。
func externalSKUOf(t *testing.T, f *invFixture, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT external_sku FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读 external_sku 失败: %v", err)
	}
	return code
}

// mustSecondVariant 给商品再加一个无规格变体（同一商品的第二个变体；归属默认仓）。
func mustSecondVariant(t *testing.T, f *invFixture, productID string) *productdto.VariantResp {
	t.Helper()
	v, err := f.products.CreateVariant(context.Background(), &productdto.CreateVariantReq{ProductID: productID})
	if err != nil {
		t.Fatalf("给商品 %s 加第二个变体失败: %v", productID, err)
	}
	return v
}

// mustBindExternalSKU 绑定外码，失败即中止。
func mustBindExternalSKU(t *testing.T, f *invFixture, warehouseID, variantID, code string) {
	t.Helper()
	if _, err := f.inventory.BindExternalSKU(context.Background(), &inventorydto.BindExternalSKUReq{
		ProjectID: f.projectID, WarehouseID: warehouseID, VariantID: variantID, ExternalSKU: code,
	}); err != nil {
		t.Fatalf("绑定外码 %q 失败: %v", code, err)
	}
}

// TestExternalSKUNToManyPerProduct N:1 的**合法形态**：同一个外部编码在同一仓库下挂在
// 同一商品的多个变体上不报错、可查询到多行；清空（空串）也是合法操作。
func TestExternalSKUNToManyPerProduct(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProductPriced(t, f, "多口味商品", 9.9)
	v1 := f.firstVariant(t, p.ID)
	v2 := mustSecondVariant(t, f, p.ID)

	// 两个变体共用同一个外码：这正是用户补充的场景（十几个口味共用一条仓库 SKU）。
	mustBindExternalSKU(t, f, wh.ID, v1.ID, "TASTE-9")
	mustBindExternalSKU(t, f, wh.ID, v2.ID, "TASTE-9")
	for _, v := range []*productdto.VariantResp{v1, v2} {
		if got := externalSKUOf(t, f, v.ID, wh.ID); got != "TASTE-9" {
			t.Fatalf("变体 %s 的外码应为 TASTE-9，实际 %q", v.ID, got)
		}
	}

	// 按外码可查询到多行 —— 「这个外码在本仓有多少条货」的查询口径。
	rows, err := f.inventory.ListStocks(context.Background(), &inventorydto.ListStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, ExternalSKU: "TASTE-9", Size: 50,
	})
	if err != nil {
		t.Fatalf("按外码查库存失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("同一外码应查到同一商品的两行，实际 %d 行", len(rows))
	}
	for _, r := range rows {
		if r.ExternalSKU != "TASTE-9" {
			t.Fatalf("查到的那行外码应为 TASTE-9，实际 %q", r.ExternalSKU)
		}
	}

	// 清空是合法操作：空串 = 该仓改回用我们自己的 SKU（不是「删除失败」）。
	mustBindExternalSKU(t, f, wh.ID, v2.ID, "   ")
	if got := externalSKUOf(t, f, v2.ID, wh.ID); got != "" {
		t.Fatalf("清空后应为空串，实际 %q", got)
	}
}

// TestExternalSKUCrossProductRejected 跨商品共用一个外码**必须被拒**（N:1 弱校验），
// 且被拒后真源一字不动；同一个外码出现在**另一个仓库**则是允许的。
func TestExternalSKUCrossProductRejected(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	pa := mustProductPriced(t, f, "商品 A", 9.9)
	pb := mustProductPriced(t, f, "商品 B", 19.9)
	va := f.firstVariant(t, pa.ID)
	vb := f.firstVariant(t, pb.ID)

	mustBindExternalSKU(t, f, wh.ID, va.ID, "EXT-A")

	_, err := f.inventory.BindExternalSKU(ctx, &inventorydto.BindExternalSKUReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, VariantID: vb.ID, ExternalSKU: "EXT-A",
	})
	if err == nil {
		t.Fatal("两个不同商品共用同一外码应被拒绝")
	}
	if !strings.HasPrefix(err.Error(), inventoryenums.ErrExternalSKUProductConflict) {
		t.Fatalf("错误应以 %s 开头（可行动的业务错误），实际 %v", inventoryenums.ErrExternalSKUProductConflict, err)
	}
	if got := externalSKUOf(t, f, vb.ID, wh.ID); got != "" {
		t.Fatalf("被拒后商品 B 的那行不该被写入，实际 %q", got)
	}

	// 同一个外码在**另一个仓库**里属于另一个商品：合法（SKU 的身份范围下沉到仓库）。
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	if _, err := f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: sh.ID, ProductID: pb.ID, VariantID: vb.ID, SKUCode: "SH_B",
	}); err != nil {
		t.Fatalf("在上海仓建商品 B 的库存行失败: %v", err)
	}
	mustBindExternalSKU(t, f, sh.ID, vb.ID, "EXT-A")
	if got := externalSKUOf(t, f, vb.ID, sh.ID); got != "EXT-A" {
		t.Fatalf("另一个仓的外码应为 EXT-A，实际 %q", got)
	}
}

// TestExternalSKUEmptyDoesNotParticipate 空串不参与校验：同一仓里可以有任意多行
// external_sku = ”（自营仓的常态），互相不构成冲突，也不挡任何写入。
func TestExternalSKUEmptyDoesNotParticipate(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	pa := mustProductPriced(t, f, "空码商品 A", 9.9)
	pb := mustProductPriced(t, f, "空码商品 B", 19.9)
	va := f.firstVariant(t, pa.ID)
	vb := f.firstVariant(t, pb.ID)

	// 两个商品的两行都停在空串（各自建商品时就是空串）→ 再显式写空串也不报错。
	mustBindExternalSKU(t, f, wh.ID, va.ID, "")
	mustBindExternalSKU(t, f, wh.ID, vb.ID, "")

	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks "+
		"WHERE warehouse_id = ? AND external_sku = ''", wh.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计空码行失败: %v", err)
	}
	if n < 2 {
		t.Fatalf("同一仓应允许多行空外码，实际 %d 行", n)
	}
	// 空码不参与 N:1 弱校验：商品 B 的变体没有被商品 A 的「空码」挡住。
	if got := externalSKUOf(t, f, vb.ID, wh.ID); got != "" {
		t.Fatalf("商品 B 的外码应为空串，实际 %q", got)
	}
}

// TestExternalSKUInvalidRejected 显式给了非法外码时明确拒绝，真源不动。
func TestExternalSKUInvalidRejected(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProductPriced(t, f, "非法外码商品", 9.9)
	v := f.firstVariant(t, p.ID)

	for _, bad := range []string{strings.Repeat("X", 129), "BAD\nCODE", "BAD\x00CODE"} {
		_, err := f.inventory.BindExternalSKU(context.Background(), &inventorydto.BindExternalSKUReq{
			ProjectID: f.projectID, WarehouseID: wh.ID, VariantID: v.ID, ExternalSKU: bad,
		})
		if err == nil || err.Error() != inventoryenums.ErrExternalSKUInvalid {
			t.Fatalf("非法外码 %q 应返回 %s，实际 %v", bad, inventoryenums.ErrExternalSKUInvalid, err)
		}
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("被拒后真源不该有值，实际 %q", got)
	}
}

// TestExternalSKUSchemaShape 结构口径（迁移 251）：
//
//	① 列在、NOT NULL、默认空串；
//	② 索引是**普通索引**（不带 UNIQUE）—— N:1 的映射不能被唯一约束扭曲；
//	③ 仓库 SKU 列表 / 定位可用（「从仓库选」的两条只读入口）。
func TestExternalSKUSchemaShape(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	var col struct {
		Nullable string `gorm:"column:is_nullable"`
		Default  string `gorm:"column:column_default"`
	}
	if err := f.db.Raw("SELECT is_nullable, COALESCE(column_default, '') AS column_default " +
		"FROM information_schema.columns WHERE table_schema = current_schema() " +
		"AND table_name = 'inventory_stocks' AND column_name = 'external_sku'").Scan(&col).Error; err != nil {
		t.Fatalf("查 external_sku 列失败: %v", err)
	}
	if col.Nullable == "" {
		t.Fatal("迁移 251 未建 external_sku 列")
	}
	if col.Nullable != "NO" {
		t.Fatalf("external_sku 应为 NOT NULL，实际 is_nullable=%s", col.Nullable)
	}
	if !strings.Contains(col.Default, "''") {
		t.Fatalf("external_sku 默认值应为空串，实际 %q", col.Default)
	}

	var indexdef string
	if err := f.db.Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() " +
		"AND tablename = 'inventory_stocks' " +
		"AND indexname = 'idx_inventory_stocks_warehouse_external_sku'").Scan(&indexdef).Error; err != nil {
		t.Fatalf("查外码索引失败: %v", err)
	}
	if indexdef == "" {
		t.Fatal("迁移 251 未建 idx_inventory_stocks_warehouse_external_sku 索引")
	}
	if strings.Contains(strings.ToUpper(indexdef), "UNIQUE") {
		t.Fatalf("外码索引必须是普通索引（N:1 映射），实际定义 %q", indexdef)
	}

	// 我们自己的 SKU 仓内唯一（迁移 244）不受本批影响。
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM pg_constraint WHERE conrelid = 'inventory_stocks'::regclass " +
		"AND conname = 'uq_inventory_stocks_warehouse_sku'").Scan(&n).Error; err != nil {
		t.Fatalf("查仓内 SKU 唯一约束失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("UNIQUE (warehouse_id, sku_code) 应仍在位，实际 %d", n)
	}
}

// TestExternalSKUListAndLocate 只读入口：「从仓库选」的候选列表（按仓分组 + 关键字）
// 与最小定位查询（仓库 + 仓库 SKU → 那一行）。
func TestExternalSKUListAndLocate(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProductPriced(t, f, "候选列表商品", 9.9)
	v := f.firstVariant(t, p.ID)
	mustBindExternalSKU(t, f, wh.ID, v.ID, "EXT-LIST-1")

	rows, err := f.inventory.ListWarehouseSKUs(ctx, &inventorydto.ListWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, Size: 50,
	})
	if err != nil {
		t.Fatalf("列仓库 SKU 失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("该仓应只有一条货，实际 %d", len(rows))
	}
	got := rows[0]
	if got.WarehouseID != wh.ID || got.WarehouseCode != "SZ" {
		t.Fatalf("行应带仓库标识，实际 %+v", got)
	}
	// 仓库侧的行带的是**裸码**（剥掉仓码前缀的编码）；v.SKUCode 是商品 / 变体侧的带前缀编码。
	bare := bareSKU(v.SKUCode, wh.Code)
	if got.SKUCode != bare || got.ExternalSKU != "EXT-LIST-1" {
		t.Fatalf("行应带仓库侧裸码 %q 与外码，实际 sku=%q external=%q", bare, got.SKUCode, got.ExternalSKU)
	}
	// 今天 inventory_stocks.variant_id 是 NOT NULL + 外键：「是否有对应变体」恒为真。
	if !got.HasVariant || got.VariantID != v.ID || got.ProductID != p.ID {
		t.Fatalf("行应指明对应的变体与商品，实际 %+v", got)
	}

	// 关键字命中**外部**编码也要能过滤出来（运营手上可能只有对方那个号）。
	byExternal, err := f.inventory.ListWarehouseSKUs(ctx, &inventorydto.ListWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, Keyword: "EXT-LIST", Size: 50,
	})
	if err != nil {
		t.Fatalf("按外码关键字列仓库 SKU 失败: %v", err)
	}
	if len(byExternal) != 1 {
		t.Fatalf("按外码关键字应命中 1 条，实际 %d", len(byExternal))
	}

	// 最小定位查询：给定仓库 + 仓库 SKU → 那一行。
	one, err := f.inventory.GetWarehouseSKU(ctx, &inventorydto.GetWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: bare,
	})
	if err != nil {
		t.Fatalf("定位仓库 SKU 失败: %v", err)
	}
	if one.VariantID != v.ID || one.ExternalSKU != "EXT-LIST-1" {
		t.Fatalf("定位结果不对：%+v", one)
	}
	// 该仓没有这条编码：明确报业务错误（不返回空行、也不静默当作「没选」）。
	_, err = f.inventory.GetWarehouseSKU(ctx, &inventorydto.GetWarehouseSKUReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "NOT-IN-THIS-WAREHOUSE",
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseSKUNotFound {
		t.Fatalf("不存在的仓库 SKU 应返回 %s，实际 %v", inventoryenums.ErrWarehouseSKUNotFound, err)
	}
}

// TestExternalSKUErrorMessagesAreChinese 新哨兵的词条必须中英成对且中文可读
// —— enums 常量值就是 i18n key，缺词条时页面会原样显示裸 key。
func TestExternalSKUErrorMessagesAreChinese(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		inventoryenums.ErrExternalSKUInvalid,
		inventoryenums.ErrExternalSKUProductConflict,
		inventoryenums.ErrWarehouseSKUNotFound,
		inventoryenums.ErrWarehouseSKURequired,
		inventoryenums.ErrWarehouseSKUBundleNotAllowed,
		inventoryenums.ErrWarehouseSKUCodeTaken,
		inventoryenums.ErrSKUSourceInvalid,
	}
	for _, key := range keys {
		zh := i18nValue(t, f, key, "zh-CN")
		if zh == "" {
			t.Fatalf("缺 zh-CN 词条：%s（页面上会显示裸 key）", key)
		}
		if zh == key || !hasCJK(zh) {
			t.Fatalf("%s 的 zh-CN 文案应是可读中文，实际 %q", key, zh)
		}
		if en := i18nValue(t, f, key, "en-US"); en == "" {
			t.Fatalf("缺 en-US 词条：%s（中英必须成对）", key)
		}
	}
}

// i18nValue 取某个 key 在指定语言的词条（不存在返回空串）。
func i18nValue(t *testing.T, f *invFixture, key, lang string) string {
	t.Helper()
	var value string
	if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).
		Scan(&value).Error; err != nil {
		t.Fatalf("查词条 %s/%s 失败: %v", key, lang, err)
	}
	return value
}

// hasCJK 判断文本里是否含中日韩字符（用来证伪「词条其实是裸 key」）。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
