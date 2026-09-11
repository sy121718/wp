// Package feature inventory 模块 feature 测试 —— 仓库与库存记录（issue #15）。
//
// 覆盖本票四条验收（真实 PostgreSQL + 生产 DDL + 真实 service）：
//  1. 可建仓库并设置一个默认仓；未指定仓库时自动兜底到默认仓；
//  2. 新建变体时可指定仓库（不选则默认仓），并自动生成对应库存记录（初始为 0）；
//  3. 库存以「SKU × 仓库」为维度，同一 SKU 可在多个仓各有一行；
//  4. 后台可查看某 SKU 的各仓库存（真实 Jet 渲染 + 原生表单写链路）。
//
// 另覆盖三条容易踩的边界：
//
//	· 可用量只读真源：把 product_variants.stock_total（冗余缓存）改成离谱的值，
//	  经契约读到的仍是 inventory_stocks 的真值 —— 缓存绝不参与判断；
//	· 默认仓是「必须存在」的兜底：不能删、不能停用、不能取消默认；缺默认仓即显式报错；
//	· 仓库短码是 SKU 编码前缀：工程内唯一、大小写归一、非法字符拒绝。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// invFixture 隔离 PG schema + 生产迁移 + 真实工程行 + 真实 inventory / product service。
//
// 装配与生产一致：inventory 的实现作为变体库存端口注入 product
// （依赖方向 inventory → product），因此「建变体即生成库存记录」走的是真实链路。
type invFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
}

func newInvFixture(t *testing.T) *invFixture {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "库存测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetVariantStock(inv)
	return &invFixture{
		inventory: inv, products: products, db: db,
		projects: projects, projectID: project.ID,
	}
}

// createWarehouse 建仓小工具（返回 id）。
func (f *invFixture) createWarehouse(t *testing.T, code, name string, isDefault bool) *inventorydto.WarehouseResp {
	t.Helper()
	w, err := f.inventory.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: code, Name: name, IsDefault: isDefault,
	})
	if err != nil {
		t.Fatalf("建仓 %s 失败: %v", code, err)
	}
	return w
}

// firstVariant 取商品的首个变体（商品恒有至少一个变体）。
func (f *invFixture) firstVariant(t *testing.T, productID string) *productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if len(detail.Variants) == 0 {
		t.Fatalf("商品 %s 没有任何变体", productID)
	}
	return detail.Variants[0]
}

// stockQty 直读真源表的数量（断言落库结果，不经 service）。
func (f *invFixture) stockQty(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var qty int
	err := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&qty).Error
	if err != nil {
		t.Fatalf("读库存真源失败: %v", err)
	}
	return qty
}

// TestWarehouseCreateAndDefaultFallback 验收 1：
// 可建仓库并设置一个默认仓；未指定仓库时自动兜底到默认仓。
func TestWarehouseCreateAndDefaultFallback(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 工程内第一个仓自动成为默认仓（「必须有一个默认仓」）。
	sz := f.createWarehouse(t, "SZ", "苏州仓", false)
	if !sz.IsDefault {
		t.Fatalf("工程内第一个仓应自动成为默认仓，实际 isDefault=false")
	}
	// 短码归一为大写；第二个仓不指定即不是默认仓。
	sh := f.createWarehouse(t, "sh", "上海仓", false)
	if sh.Code != "SH" {
		t.Fatalf("短码应归一为大写，实际 %q", sh.Code)
	}
	if sh.IsDefault {
		t.Fatalf("第二个仓不应自动成为默认仓")
	}

	// 未指定仓库时的兜底：ensure 不传 warehouseId → 落在默认仓 SZ。
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)
	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sz.ID})
	if err != nil {
		t.Fatalf("默认仓兜底未生成库存记录: %v", err)
	}
	if row.Quantity != 0 {
		t.Fatalf("新生成的库存记录应为 0，实际 %d", row.Quantity)
	}

	// 切换默认仓：SH 成为唯一默认仓，SZ 自动让位。
	yes := true
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, IsDefault: &yes}); err != nil {
		t.Fatalf("切换默认仓失败: %v", err)
	}
	list, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("仓库列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 个仓库，实际 %d", len(list))
	}
	if !list[0].IsDefault || list[0].ID != sh.ID {
		t.Fatalf("默认仓应排在列表最前且为 SH，实际 %+v", list[0])
	}
	for _, w := range list {
		if w.ID == sz.ID && w.IsDefault {
			t.Fatalf("切换默认仓后 SZ 不应再是默认仓")
		}
	}

	// 兜底解析跟着默认仓走：再建一个变体（不指定仓库）→ 落在 SH。
	v2, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if _, err = f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v2.ID, WarehouseID: sh.ID}); err != nil {
		t.Fatalf("切换默认仓后应兜底到 SH: %v", err)
	}
	if _, err = f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v2.ID, WarehouseID: sz.ID}); err == nil {
		t.Fatalf("变体只应落在归属仓，SZ 不应有它的库存记录")
	}

	// 默认仓不可删 / 不可停用 / 不可取消默认。
	if err = f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{ID: sh.ID}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("删除默认仓应返回 ErrWarehouseIsDefault，实际 %v", err)
	}
	disabled := inventoryenums.StatusDisabled
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, Status: &disabled}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("停用默认仓应返回 ErrWarehouseIsDefault，实际 %v", err)
	}
	no := false
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, IsDefault: &no}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("取消默认标记应返回 ErrWarehouseIsDefault，实际 %v", err)
	}

	// 非默认仓可以删（库存行为空）。
	if err = f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{ID: sz.ID}); err != nil {
		t.Fatalf("删除非默认仓失败: %v", err)
	}

	// 短码校验：重复（大小写不敏感）与非法字符一律拒绝。
	if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "sh", Name: "重复短码",
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseCodeTaken {
		t.Fatalf("重复短码应返回 ErrWarehouseCodeTaken，实际 %v", err)
	}
	for _, bad := range []string{"S_1", "苏州", "TOOLONGCODE"} {
		if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
			ProjectID: f.projectID, Code: bad, Name: "非法短码",
		}); err == nil || err.Error() != inventoryenums.ErrWarehouseCodeInvalid {
			t.Fatalf("非法短码 %q 应返回 ErrWarehouseCodeInvalid，实际 %v", bad, err)
		}
	}

	// 缺默认仓：另一个工程没有任何仓库 → 未指定仓库时显式报错（不静默挑一个仓）。
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "没有仓库的工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: other.ID, VariantID: v.ID,
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseDefaultMissing {
		t.Fatalf("缺默认仓应返回 ErrWarehouseDefaultMissing，实际 %v", err)
	}
}

// TestVariantCreateGeneratesStockRow 验收 2：
// 新建变体时可指定仓库（不选则默认仓），并自动生成对应库存记录（初始为 0）。
func TestVariantCreateGeneratesStockRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	// 建商品不指定仓库 → 首个变体落在默认仓，SKU 前缀是默认仓短码。
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v1 := f.firstVariant(t, p.ID)
	if !strings.HasPrefix(v1.SKUCode, "SZ_TEE_") {
		t.Fatalf("SKU 编码应以归属仓短码开头（SZ_TEE_），实际 %q", v1.SKUCode)
	}
	if got := f.stockQty(t, v1.ID, sz.ID); got != 0 {
		t.Fatalf("首个变体应在默认仓有一条 0 库存记录，实际 %d", got)
	}

	// 新增变体时指定仓库 → 落在指定仓（不是默认仓），SKU 前缀跟着变。
	v2, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, WarehouseID: sh.ID})
	if err != nil {
		t.Fatalf("指定仓库新增变体失败: %v", err)
	}
	if !strings.HasPrefix(v2.SKUCode, "SH_TEE_") {
		t.Fatalf("指定仓库后 SKU 前缀应为 SH_TEE_，实际 %q", v2.SKUCode)
	}
	if got := f.stockQty(t, v2.ID, sh.ID); got != 0 {
		t.Fatalf("变体应在指定仓有一条 0 库存记录，实际 %d", got)
	}
	var n int64
	if err = f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v2.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("变体应恰好有一条库存记录，实际 %d", n)
	}

	// 组合生成：勾选两个值生成 2 个组合，每个新变体都在归属仓有库存记录且 SKU 不重复。
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "尺寸", Key: "size",
		Values: []productdto.AttributeValueReq{{Label: "S", Key: "s"}, {Label: "M", Key: "m"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{ID: p.ID, AttributeIDs: []string{attr.ID}}); err != nil {
		t.Fatalf("给商品挂属性组失败: %v", err)
	}
	// 生成前的既有 SKU 集合：用于区分「本批新建」与「既有变体承接组合」。
	beforeSKUs := map[string]bool{}
	if detail, derr := f.products.Get(ctx, &productdto.GetReq{ID: p.ID}); derr == nil {
		for _, v := range detail.Variants {
			beforeSKUs[v.SKUCode] = true
		}
	}
	gen, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
		ProductID: p.ID, WarehouseID: sh.ID,
	})
	if err != nil {
		t.Fatalf("生成变体组合失败: %v", err)
	}
	// 两个组合里，既有的「无规格占位变体」就地承接一个（#8 的既有语义），其余新建。
	if gen.Created+gen.Adopted != 2 {
		t.Fatalf("应覆盖 2 个组合（新建 + 承接），实际 created=%d adopted=%d", gen.Created, gen.Adopted)
	}
	if gen.Created == 0 {
		t.Fatalf("至少应新建 1 个组合变体，实际 %d", gen.Created)
	}
	seen := map[string]bool{}
	newOnes := 0
	for _, v := range gen.Variants {
		if seen[v.SKUCode] {
			t.Fatalf("SKU 编码重复：%s", v.SKUCode)
		}
		seen[v.SKUCode] = true
		if beforeSKUs[v.SKUCode] {
			// 既有变体（承接组合的占位变体、以及先前已建的变体）的库存记录一条不动：
			// 生成路径只为本批**新建**的变体在指定仓生成库存记录。
			var cnt int64
			if qerr := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v.ID).Scan(&cnt).Error; qerr != nil {
				t.Fatalf("统计既有变体库存行失败: %v", qerr)
			}
			if cnt != 1 {
				t.Fatalf("既有变体 %s 的库存记录数不应变化，实际 %d", v.SKUCode, cnt)
			}
			continue
		}
		if !strings.HasPrefix(v.SKUCode, "SH_TEE_") {
			t.Fatalf("生成路径的 SKU 前缀应为指定仓 SH_TEE_，实际 %q", v.SKUCode)
		}
		newOnes++
		if _, gerr := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sh.ID}); gerr != nil {
			t.Fatalf("生成的变体 %s 缺少归属仓库存记录: %v", v.SKUCode, gerr)
		}
	}
	if newOnes != gen.Created {
		t.Fatalf("指定仓库生成的新变体应全部落在该仓：new=%d created=%d", newOnes, gen.Created)
	}

	// 指定的仓库不存在 / 跨工程 / 已停用时拒绝，且不留下半截变体。
	if _, err = f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, WarehouseID: "00000000-0000-0000-0000-000000000000",
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseNotFound {
		t.Fatalf("不存在的仓库应返回 ErrWarehouseNotFound，实际 %v", err)
	}
	before := countVariants(t, f, p.ID)
	mismatch := f.createWarehouse(t, "BJ", "北京仓", false)
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: mismatch.ID, Status: strPtr(inventoryenums.StatusDisabled),
	}); err != nil {
		t.Fatalf("停用仓库失败: %v", err)
	}
	if _, err = f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, WarehouseID: mismatch.ID,
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseDisabled {
		t.Fatalf("停用仓库应返回 ErrWarehouseDisabled，实际 %v", err)
	}
	if after := countVariants(t, f, p.ID); after != before {
		t.Fatalf("归属仓解析失败时不应落库变体：前 %d 后 %d", before, after)
	}
}

// TestStockPerSkuPerWarehouse 验收 3：
// 库存以「SKU × 仓库」为维度，同一 SKU 可在多个仓各有一行。
func TestStockPerSkuPerWarehouse(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)

	// 归属仓之外再显式生成一行：同一 SKU 在两个仓各有一行。
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode, WarehouseID: sh.ID,
	}); err != nil {
		t.Fatalf("在第二个仓生成库存记录失败: %v", err)
	}
	// 幂等：重复 ensure 不产生第二行。
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode, WarehouseID: sh.ID,
	}); err != nil {
		t.Fatalf("重复生成库存记录失败: %v", err)
	}
	var n int64
	if err = f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("同一 SKU 应恰好有两行（SZ + SH），实际 %d", n)
	}
	// 数据库唯一约束按 (变体, 仓库)：直接插重复行必须被拒绝。
	if err = f.db.Exec("INSERT INTO inventory_stocks (project_id, warehouse_id, product_id, variant_id, sku_code) VALUES (?,?,?,?,?)",
		f.projectID, sh.ID, p.ID, v.ID, v.SKUCode).Error; err == nil {
		t.Fatalf("(变体, 仓库) 重复插入应被唯一约束拒绝")
	}

	// 按 SKU 查各仓：两行，带仓库名与短码，默认仓在最前。
	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: v.SKUCode,
	})
	if err != nil {
		t.Fatalf("按 SKU 查库存失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("某 SKU 应有 2 条仓库维度记录，实际 %d", len(rows))
	}
	if rows[0].WarehouseID != sz.ID || rows[1].WarehouseID != sh.ID {
		t.Fatalf("默认仓应排在最前：%+v", rows)
	}
	if rows[0].WarehouseName == "" || rows[1].WarehouseCode != "SH" {
		t.Fatalf("库存行应带上仓库名与短码：%+v", rows)
	}
	for _, r := range rows {
		if r.Quantity != 0 {
			t.Fatalf("初始库存应为 0，实际 %d", r.Quantity)
		}
		if r.SKUCode != v.SKUCode || r.VariantID != v.ID {
			t.Fatalf("库存维度应指向同一 SKU / 变体：%+v", r)
		}
	}
}

// TestStockReadsTrueSourceNotCache 死线：
// 可用量只认真源（inventory_stocks），product_variants.stock_total 只是展示缓存。
func TestStockReadsTrueSourceNotCache(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)

	// 把冗余缓存改成离谱的值（模拟缓存漂移 / 陈旧）。
	if err = f.db.Exec("UPDATE product_variants SET stock_total = 999 WHERE id = ?", v.ID).Error; err != nil {
		t.Fatalf("写缓存字段失败: %v", err)
	}
	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sz.ID})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	if row.Quantity != 0 {
		t.Fatalf("库存必须读真源（0），实际读了缓存？得到 %d", row.Quantity)
	}
	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: v.SKUCode,
	})
	if err != nil {
		t.Fatalf("按 SKU 查库存失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Quantity != 0 {
		t.Fatalf("列表也必须读真源，实际 %+v", rows)
	}
	// 缓存字段原样保留（本票不改它：同步是 issue #16 的职责）。
	var cache int
	if err = f.db.Raw("SELECT stock_total FROM product_variants WHERE id = ?", v.ID).Scan(&cache).Error; err != nil {
		t.Fatalf("读缓存字段失败: %v", err)
	}
	if cache != 999 {
		t.Fatalf("本票不应改写商品侧缓存（同步属 issue #16），实际 %d", cache)
	}
}

// TestInventoryAdminPage 验收 4：后台可查看某 SKU 的各仓库存（真实模板渲染 + 写链路）。
func TestInventoryAdminPage(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()

	// 表单建仓 → 302 回列表。
	rec := postForm(engine, "/admin/inventory/warehouse/create", url.Values{
		"projectId": {f.projectID}, "code": {"SZ"}, "name": {"苏州仓"}, "isDefault": {"1"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建仓应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	rec = postForm(engine, "/admin/inventory/warehouse/create", url.Values{
		"projectId": {f.projectID}, "code": {"SH"}, "name": {"上海仓"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建第二个仓应 302，实际 %d", rec.Code)
	}

	// 商品 + 变体（不指定仓库 → 默认仓 SZ），并在第二个仓补一行 —— 制造「多仓」场景。
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)
	warehouses, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil || len(warehouses) != 2 {
		t.Fatalf("仓库列表异常：%v %+v", err, warehouses)
	}
	shID := ""
	for _, w := range warehouses {
		if w.Code == "SH" {
			shID = w.ID
		}
	}
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode, WarehouseID: shID,
	}); err != nil {
		t.Fatalf("在第二个仓生成库存记录失败: %v", err)
	}

	// 页面：仓库列表 + 默认仓徽标 + SKU 各仓库存表。
	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID+"&sku="+url.QueryEscape(v.SKUCode))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"库存管理", "新建仓库", "苏州仓", "上海仓", "SZ", "默认仓",
		v.SKUCode, "某 SKU 的各仓库存", "查看各仓库存",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("库存页缺少 %q", want)
		}
	}
	// 两行仓库维度（同一 SKU 在两个仓各一行）。
	if strings.Count(body, v.SKUCode) < 3 {
		t.Fatalf("页面应同时列出该 SKU 在两个仓的库存行：%s", body)
	}

	// 不带 sku 时页面给出选择入口，不报错。
	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("无 SKU 查询时页面应 200，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "选一个 SKU 后") {
		t.Fatalf("无 SKU 时应给出查询提示")
	}

	// 设为默认仓（表单链路）：SH 成为默认仓。
	rec = postForm(engine, "/admin/inventory/warehouse/default", url.Values{
		"projectId": {f.projectID}, "id": {shID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 设默认仓应 302，实际 %d", rec.Code)
	}
	after, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil || !after[0].IsDefault || after[0].ID != shID {
		t.Fatalf("设默认仓未生效：%v %+v", err, after)
	}

	// 删除默认仓被拒绝（错误经 ?err= 回显）。
	rec = postForm(engine, "/admin/inventory/warehouse/delete", url.Values{
		"projectId": {f.projectID}, "id": {shID},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("删除默认仓应回列表并带错误提示，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestProductPageWarehouseSelect 验收 2（后台表单路径）：
// 商品页的「归属仓」下拉来自仓库模块，提交后变体落在所选仓、SKU 带该仓短码前缀。
func TestProductPageWarehouseSelect(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := dashboardhttp.NewProductPageHandle(f.products, f.projects)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", handle.ProductsPage)
	engine.POST("/admin/products/variant/create", handle.ProductsVariantCreate)

	rec := httptestGet(engine, "/admin/products?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"归属仓", "苏州仓（SZ）", "上海仓（SH）", "（默认仓）"} {
		if !strings.Contains(body, want) {
			t.Fatalf("商品页缺少归属仓下拉内容 %q", want)
		}
	}

	// 表单提交时指定 SH → 变体落在 SH，SKU 前缀为 SH_TEE_。
	rec = postForm(engine, "/admin/products/variant/create", url.Values{
		"projectId": {f.projectID}, "productId": {p.ID}, "warehouseId": {sh.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 新增变体应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	detail, err := f.products.Get(ctx, &productdto.GetReq{ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	var picked *productdto.VariantResp
	for _, v := range detail.Variants {
		if strings.HasPrefix(v.SKUCode, "SH_TEE_") {
			picked = v
		}
	}
	if picked == nil {
		t.Fatalf("表单指定仓库后应生成 SH_ 前缀的变体，实际 %+v", detail.Variants)
	}
	if got := f.stockQty(t, picked.ID, sh.ID); got != 0 {
		t.Fatalf("表单路径也应在归属仓生成 0 库存记录，实际 %d", got)
	}
}

// TestInventoryPermissionsAndMenusSeeded 迁移 100/101：
// 权限点与后台菜单已 seed（未 seed 时 Casbin 无策略 → 含超管全员 403）。
func TestInventoryPermissionsAndMenusSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE module = 'inventory'").Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if n != 9 {
		t.Fatalf("迁移 100 应 seed 9 个 inventory 权限点，实际 %d", n)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '库存管理' AND deleted_time IS NULL").Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 101 应 seed 「库存管理」后台菜单，实际 %d", n)
	}
}

// newInventoryPageEngine 装配只挂库存管理页的测试引擎（真实 Jet 模板 + 真实 service）。
func newInventoryPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := dashboardhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", handle.InventoryPage)
	engine.POST("/admin/inventory/warehouse/create", handle.InventoryWarehouseCreate)
	engine.POST("/admin/inventory/warehouse/update", handle.InventoryWarehouseUpdate)
	engine.POST("/admin/inventory/warehouse/default", handle.InventoryWarehouseDefault)
	engine.POST("/admin/inventory/warehouse/delete", handle.InventoryWarehouseDelete)
	return engine, f
}

// templateRoot 模板根目录（测试进程工作目录在 public/test/inventory/feature）。
func templateRoot() string {
	return "../../../../internal/templates"
}

// httptestGet 发一个 GET 请求（页面渲染断言用）。
func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// postForm 发送 application/x-www-form-urlencoded 表单。
func postForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// countVariants 商品的变体数量（直查，用于断言「失败不留半截」）。
func countVariants(t *testing.T, f *invFixture, productID string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ?", productID).Scan(&n).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	return n
}

// strPtr 取字符串指针（可空入参）。
func strPtr(s string) *string { return &s }
