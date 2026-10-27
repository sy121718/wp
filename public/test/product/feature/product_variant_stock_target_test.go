package feature

// product_variant_stock_target_test.go — B8「仓库明细」的端到端判据（真实 PG + 真实 product /
// inventory service）。
//
// 五条口径各钉一条，都是运营真会踩到的形状：
//   1. 明细读得回来：编辑页按仓分组给出当前在库（含名称与图），StocksKnown=true；
//   2. 数量是**目标值**：填 12 就校准成 12，不是 7+12；
//   3. 关联照旧落在 product_variants.warehouse_skus（不新建关联表），删行保存后关联也消失；
//   4. 同仓同 SKU 在同一个变体里重复出现 → 写前拒绝（一条都不写）；
//   5. 不跟踪（无限）的行填了数字 → 改为跟踪并校准到该数（留空则一动不动）。

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/pkg/facing"
)

// saveWithStocks 按商品表单的形状保存一行清单（带仓库明细；qty 是目标值）。
func (f *variantListFixture) saveWithStocks(t *testing.T, p *productdto.ProductResp,
	v *productdto.VariantResp, sku string, qty int) {
	t.Helper()
	f.save(t, p.ID, []productdto.VariantListResp{{
		VariantID: v.ID, SKUCode: v.SKUCode, OptionValues: v.OptionValues,
		Stocks: []productdto.VariantStockRowResp{{
			WarehouseID: f.warehouse.ID, SKUCode: sku, Quantity: qty,
		}},
	}})
}

// seedVariants 生成并保存笛卡尔积（返回第一个变体）。
func (f *variantListFixture) seedVariants(t *testing.T, p *productdto.ProductResp,
	pv *productdto.PreviewVariantResp) {
	t.Helper()
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	f.save(t, p.ID, rows)
}

// editableRow 读回编辑页那一行（Get + fillVariantListStocks 的对外等价物）。
func (f *variantListFixture) editableRow(t *testing.T, p *productdto.ProductResp,
	optionValues []byte) productdto.VariantListRowResp {
	t.Helper()
	view, err := f.products.ProductEditPage(context.Background(), &productdto.ProductEditPageReq{
		ProjectID: f.projectID, ProductID: p.ID,
	})
	if err != nil {
		t.Fatalf("读编辑页失败: %v", err)
	}
	if view.Product == nil {
		t.Fatal("编辑页应带回商品行")
	}
	want := optionKeyOf(t, optionValues)
	for _, row := range view.Product.Variants {
		if optionKeyOf(t, []byte(row.OptionValues)) == want {
			return row
		}
	}
	t.Fatalf("编辑页里没有组合 %s 的变体行", want)
	return productdto.VariantListRowResp{}
}

// TestVariantStockRowsReadBack 明细读得回来：分组按仓、数量是当前在库、StocksKnown=true。
func TestVariantStockRowsReadBack(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	p := f.mkProduct(t, "围巾", "scarf-stock", []string{color.ID})
	pv := f.previewAll(t, p.ID)
	f.seedVariants(t, p, pv)
	v := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	f.saveWithStocks(t, p, v, v.SKUCode, 7)

	row := f.editableRow(t, p, v.OptionValues)
	if !row.StocksKnown {
		t.Fatal("明细读到了就必须把 StocksKnown 置真（否则前端会以为「没挂货」而裁掉关联）")
	}
	if len(row.Stocks) != 1 {
		t.Fatalf("应有一个仓库分组，实际 %d：%+v", len(row.Stocks), row.Stocks)
	}
	group := row.Stocks[0]
	if group.WarehouseID != f.warehouse.ID {
		t.Fatalf("分组的仓应是 %s，实际 %s", f.warehouse.ID, group.WarehouseID)
	}
	if len(group.Rows) != 1 || group.Rows[0].Quantity != 7 {
		t.Fatalf("组内应有一条数量 7 的货行：%+v", group.Rows)
	}
	if group.Rows[0].SKUCode == "" {
		t.Fatal("货行要有编码（模板显示与判重键都靠它）")
	}
	if !group.Rows[0].TrackQuantity {
		t.Fatal("填了数字的行必须是被跟踪的（变动路径写回时两列同一条 UPDATE）")
	}
}

// TestVariantStockQuantityIsTargetNotDelta 数量是目标值：12 就是 12，不是 5+12。
func TestVariantStockQuantityIsTargetNotDelta(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red"})
	p := f.mkProduct(t, "帽子", "cap-target", []string{color.ID})
	pv := f.previewAll(t, p.ID)
	f.seedVariants(t, p, pv)
	v := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	f.saveWithStocks(t, p, v, v.SKUCode, 5)
	f.saveWithStocks(t, p, v, v.SKUCode, 12)

	row := f.editableRow(t, p, v.OptionValues)
	if len(row.Stocks) != 1 || len(row.Stocks[0].Rows) != 1 {
		t.Fatalf("应有一个仓一条货行：%+v", row.Stocks)
	}
	if got := row.Stocks[0].Rows[0].Quantity; got != 12 {
		t.Fatalf("数量是目标值：期望 12，实际 %d（17 说明被当成增量累加了）", got)
	}
}

// TestVariantStockRowsPruneOnRemoval 删掉明细行再保存 → 关联从引用清单里消失。
func TestVariantStockRowsPruneOnRemoval(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red"})
	p := f.mkProduct(t, "手套", "glove-prune", []string{color.ID})
	pv := f.previewAll(t, p.ID)
	f.seedVariants(t, p, pv)
	v := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	f.saveWithStocks(t, p, v, v.SKUCode, 4)
	if row := f.editableRow(t, p, v.OptionValues); len(row.Stocks) != 1 {
		t.Fatalf("前提：应已挂一条货行，实际 %+v", row.Stocks)
	}
	// 保存时空明细 = 这个变体不挂任何货行（表单是权威清单）。
	f.save(t, p.ID, []productdto.VariantListResp{{
		VariantID: v.ID, SKUCode: v.SKUCode, OptionValues: v.OptionValues,
	}})
	row := f.editableRow(t, p, v.OptionValues)
	if len(row.Stocks) != 0 {
		t.Fatalf("删掉明细再保存后不该还挂着货行：%+v", row.Stocks)
	}
	if !row.StocksKnown {
		t.Fatal("「确实没挂货」与「读失败」要分开：这里应是 known 且空")
	}
}

// TestVariantStockRowsRejectDuplicate 同仓同 SKU 在同一变体里重复 → 写前拒绝。
func TestVariantStockRowsRejectDuplicate(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red"})
	p := f.mkProduct(t, "腰带", "belt-dup", []string{color.ID})
	pv := f.previewAll(t, p.ID)
	f.seedVariants(t, p, pv)
	v := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)

	_, err := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: p.ID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID, OperatorID: "tester",
		Rows: []productdto.VariantListResp{{
			VariantID: v.ID, SKUCode: v.SKUCode, OptionValues: v.OptionValues,
			Stocks: []productdto.VariantStockRowResp{
				{WarehouseID: f.warehouse.ID, SKUCode: v.SKUCode, Quantity: 3},
				{WarehouseID: f.warehouse.ID, SKUCode: v.SKUCode, Quantity: 9},
			},
		}},
	})
	if err == nil {
		t.Fatal("同仓同 SKU 重复应被拒绝")
	}
	fact, ok := facing.FactOf(err)
	if !ok || fact.Key != productenums.ErrVariantStockDuplicated {
		t.Fatalf("期望 %s，实际 %v", productenums.ErrVariantStockDuplicated, err)
	}
	// 写前拒绝：数量一个字节都不该动（库里那一行仍是新建时的不跟踪 0）。
	row := f.editableRow(t, p, v.OptionValues)
	for _, g := range row.Stocks {
		for _, r := range g.Rows {
			if r.Quantity != 0 || r.TrackQuantity {
				t.Fatalf("被拒绝的提交不该写任何数量：%+v", r)
			}
		}
	}
}

// TestVariantStockUntrackedRowStartsTracking 不跟踪（无限）的行填了数字 → 改为跟踪并校准。
//
// 这是这条面板的**第一次入库**路径：SaveVariantList 建行时 quantity 传 nil ⇒ 新行
// 天生不跟踪；运营在仓库明细里填的第一个数字就是它的第一次声明。留空则一动不动。
func TestVariantStockUntrackedRowStartsTracking(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red"})
	p := f.mkProduct(t, "钱包", "wallet-untracked", []string{color.ID})
	pv := f.previewAll(t, p.ID)
	f.seedVariants(t, p, pv)
	v := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)

	// 先只挂货行、不给数量（目标 0 且行不存在 ⇒ 建一条不跟踪行，不动库存）。
	f.saveWithStocks(t, p, v, v.SKUCode, 0)
	row := f.editableRow(t, p, v.OptionValues)
	if len(row.Stocks) != 1 || len(row.Stocks[0].Rows) != 1 {
		t.Fatalf("目标 0 也应把货行挂上（关联与数量是两件事）：%+v", row.Stocks)
	}
	if row.Stocks[0].Rows[0].TrackQuantity {
		t.Fatal("前提：新建行应是不跟踪（无限）")
	}
	if row.Stocks[0].Rows[0].Quantity != 0 {
		t.Fatalf("前提：不跟踪行的当前值应为 0，实际 %d", row.Stocks[0].Rows[0].Quantity)
	}

	// 填 5 ⇒ 切成跟踪并校准到 5。
	f.saveWithStocks(t, p, v, v.SKUCode, 5)
	row = f.editableRow(t, p, v.OptionValues)
	if len(row.Stocks) != 1 || len(row.Stocks[0].Rows) != 1 {
		t.Fatalf("应仍是一个仓一条货行：%+v", row.Stocks)
	}
	got := row.Stocks[0].Rows[0]
	if !got.TrackQuantity || got.Quantity != 5 {
		t.Fatalf("填数字应改为跟踪并校准到 5：%+v", got)
	}
}
