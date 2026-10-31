package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/pkg/facing"
)

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

func (f *variantListFixture) seedVariants(t *testing.T, p *productdto.ProductResp,
	pv *productdto.PreviewVariantResp) {
	t.Helper()
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	f.save(t, p.ID, rows)
}

func (f *variantListFixture) editableRow(t *testing.T, p *productdto.ProductResp,
	optionValues []byte) *productdto.VariantResp {
	t.Helper()
	product, err := f.products.Get(context.Background(), &productdto.GetReq{
		ProjectID: f.projectID, ID: p.ID,
	})
	if err != nil {
		t.Fatalf("读编辑页失败: %v", err)
	}
	if product == nil {
		t.Fatal("编辑页应带回商品行")
	}
	want := optionKeyOf(t, optionValues)
	for _, row := range product.Variants {
		if optionKeyOf(t, []byte(row.OptionValues)) == want {
			return row
		}
	}
	t.Fatalf("编辑页里没有组合 %s 的变体行", want)
	return nil
}

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
	if row.StockTotal != group.Rows[0].Quantity {
		t.Fatalf("变体汇总与已读取明细必须同源：total=%d row=%d", row.StockTotal, group.Rows[0].Quantity)
	}
	if !group.Rows[0].TrackQuantity {
		t.Fatal("填了数字的行必须是被跟踪的（变动路径写回时两列同一条 UPDATE）")
	}
}

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

	row := f.editableRow(t, p, v.OptionValues)
	for _, g := range row.Stocks {
		for _, r := range g.Rows {
			if r.Quantity != 0 || r.TrackQuantity {
				t.Fatalf("被拒绝的提交不该写任何数量：%+v", r)
			}
		}
	}
}

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
