package feature

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"go_wp/public/schema"
)

type wstockRow struct {
	WarehouseID   string `gorm:"column:warehouse_id"`
	SKUCode       string `gorm:"column:sku_code"`
	TrackQuantity bool   `gorm:"column:track_quantity"`
	Quantity      int    `gorm:"column:quantity"`
}

func (f *wpickFixture) wstockRows(t *testing.T, productID string) []wstockRow {
	t.Helper()
	var rows []wstockRow
	if err := f.db.Raw("SELECT warehouse_id, sku_code, track_quantity, quantity FROM inventory_stocks "+
		"WHERE product_id = ? ORDER BY warehouse_id", productID).Scan(&rows).Error; err != nil {
		t.Fatalf("读库存行失败: %v", err)
	}
	return rows
}

func (f *wpickFixture) wdropStockRows(t *testing.T, productID string) {
	t.Helper()
	if err := f.db.Exec("DELETE FROM inventory_stocks WHERE product_id = ?", productID).Error; err != nil {
		t.Fatalf("清库存行失败: %v", err)
	}
}

func (f *wpickFixture) wstockRowCount(t *testing.T, warehouseID, skuCode string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		warehouseID, skuCode).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

func (f *wpickFixture) wvariantCount(t *testing.T, productID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ?", productID).Scan(&n).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	return n
}

func assertNoTraceOfSKU(t *testing.T, f *wpickFixture, containerSKU, bareCode string) {
	t.Helper()
	var products, variants, stocks int
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE sku_code = ?", containerSKU).Scan(&products).Error; err != nil {
		t.Fatalf("统计商品失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE sku_code IN ?",
		[]string{containerSKU, bareCode}).Scan(&variants).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE sku_code = ?", bareCode).Scan(&stocks).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if products != 0 || variants != 0 || stocks != 0 {
		t.Fatalf("失败的商品不该留任何痕迹：products=%d product_variants=%d inventory_stocks=%d",
			products, variants, stocks)
	}
}

type failingChangesPort struct {
	masterdatacontract.MasterDataService
	err error
}

func (p *failingChangesPort) RecordChanges(ctx context.Context, inputs []*masterdatacontract.ChangeInput) error {
	return p.err
}

func (p *failingChangesPort) RecordChangesTx(ctx context.Context, tx *gorm.DB, inputs []*masterdatacontract.ChangeInput) error {
	return p.err
}

func TestProductCreateMultiWarehouseBareCodes(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	price := 12.5
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "多仓商品", Slug: "multi-wh",
		DefaultPrice: &price, SKUCode: "DRAWERSMOKE_001",

		WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err != nil {
		t.Fatalf("多仓创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SZ_DRAWERSMOKE_001" {
		t.Fatalf("主体 SKU 应带**前缀仓**（第一个勾选的仓）前缀，实际 %q", got)
	}
	if len(p.Variants) != 1 || p.Variants[0].SKUCode != "SZ_DRAWERSMOKE_001" {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", p.Variants)
	}
	rows := f.wstockRows(t, p.ID)
	if len(rows) != 2 {
		t.Fatalf("勾了两个仓应各建一行，实际 %d 行：%+v", len(rows), rows)
	}
	for _, row := range rows {

		if row.SKUCode != "DRAWERSMOKE_001" {
			t.Fatalf("仓库侧编码应是裸码 DRAWERSMOKE_001，实际 %q（仓 %s）", row.SKUCode, row.WarehouseID)
		}

		if row.TrackQuantity || row.Quantity != 0 {
			t.Fatalf("不传数量应是不跟踪（无限），实际 track=%v qty=%d", row.TrackQuantity, row.Quantity)
		}
	}
	if got := f.wstockRowCount(t, f.sz.ID, "DRAWERSMOKE_001"); got != 1 {
		t.Fatalf("默认仓应有且只有一行，实际 %d", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "DRAWERSMOKE_001"); got != 1 {
		t.Fatalf("第二仓应有且只有一行，实际 %d", got)
	}
}

func TestProductCreateClaimWarehouseDecidesPrefix(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "上海认领", Slug: "claim-sh",
		SKUCode: "TEE-9001", WarehouseIDs: []string{f.sh.ID, f.sz.ID},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SH_TEE-9001" {
		t.Fatalf("前缀仓应是列表第一个仓（SH），实际主体 SKU %q", got)
	}

	p2, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "单值兼容", Slug: "claim-single",
		SKUCode: "TEE-9002", WarehouseID: f.sh.ID,
	})
	if err != nil {
		t.Fatalf("单值创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p2.ID); got != "SH_TEE-9002" {
		t.Fatalf("单值形态应等价于只勾一个仓，实际主体 SKU %q", got)
	}
	if len(f.wstockRows(t, p2.ID)) != 1 {
		t.Fatalf("单值形态应只建一行库存，实际 %+v", f.wstockRows(t, p2.ID))
	}
}

func TestProductCreateReusesExistingWarehouseCode(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.wpickProduct(t, "已有货的宿主", "reuse-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "REUSE-1", host.ID, host.Variants[0].ID)

	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "认领已有编码", Slug: "reuse-claim",

		SKUCode: "REUSE-1", WarehouseIDs: []string{f.sh.ID},
	})
	if err != nil {
		t.Fatalf("认领已有编码应成功（复用那一行）: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SH_REUSE-1" {
		t.Fatalf("主体 SKU 应带前缀仓前缀，实际 %q", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "REUSE-1"); got != 1 {
		t.Fatalf("同仓同码必须只有一行（复用不新建），实际 %d 行", got)
	}

	var owner string
	if err := f.db.Raw("SELECT variant_id FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		f.sh.ID, "REUSE-1").Scan(&owner).Error; err != nil {
		t.Fatalf("读宿主库存行失败: %v", err)
	}
	if owner != host.Variants[0].ID {
		t.Fatalf("复用不该改写既有行的归属：应为宿主变体 %s，实际 %s", host.Variants[0].ID, owner)
	}
	if _, rows := f.wpickExternalSKU(t, p.Variants[0].ID, f.sh.ID); rows != 0 {
		t.Fatalf("认领不该为新变体建行，实际 %d 行", rows)
	}
}

func TestProductCreateQuantityTracking(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	mk := func(name, slug, sku string, qty *int) *productdto.ProductResp {
		t.Helper()
		p, err := f.products.Create(ctx, &productdto.CreateReq{
			ProjectID: f.projectID, Name: name, Slug: slug, SKUCode: sku,
			WarehouseIDs: []string{f.sz.ID}, Quantity: qty,
		})
		if err != nil {
			t.Fatalf("建商品 %s 失败: %v", name, err)
		}
		return p
	}

	untracked := mk("不跟踪", "qty-none", "QTY-NONE", nil)
	if rows := f.wstockRows(t, untracked.ID); len(rows) != 1 || rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("不传数量应是不跟踪（无限），实际 %+v", rows)
	}

	zero := 0
	trackedZero := mk("跟踪零", "qty-zero", "QTY-ZERO", &zero)
	if rows := f.wstockRows(t, trackedZero.ID); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("显式填 0 应是「跟踪且 0」（明确没货），实际 %+v", rows)
	}

	five := 5
	trackedFive := mk("跟踪五", "qty-five", "QTY-FIVE", &five)
	if rows := f.wstockRows(t, trackedFive.ID); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 5 {
		t.Fatalf("显式填 5 应是「跟踪且 5」，实际 %+v", rows)
	}
}

func TestProductListStockAggregationThreeStates(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	nonePrice := 99.0
	none, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "未入库商品", Slug: "agg-none",
		DefaultPrice: &nonePrice, SKUCode: "AGG-NONE",
	})
	if err != nil {
		t.Fatalf("建未入库商品失败: %v", err)
	}
	f.wdropStockRows(t, none.ID)
	got, err := f.products.Get(ctx, &productdto.GetReq{ID: none.ID})
	if err != nil {
		t.Fatalf("读未入库商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateNone || got.StockTotal != 0 {
		t.Fatalf("没有任何库存行应显示未入库，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}

	five := 5
	tracked, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "全部跟踪", Slug: "agg-tracked", SKUCode: "AGG-TRACKED",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID}, Quantity: &five,
	})
	if err != nil {
		t.Fatalf("建跟踪商品失败: %v", err)
	}
	got, err = f.products.Get(ctx, &productdto.GetReq{ID: tracked.ID})
	if err != nil {
		t.Fatalf("读跟踪商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateTracked || got.StockTotal != 10 {
		t.Fatalf("两仓各 5 应求和成 10，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}
	if len(got.StockWarehouses) != 2 {
		t.Fatalf("分仓明细应有两行，实际 %+v", got.StockWarehouses)
	}

	mixed, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "混合状态", Slug: "agg-mixed", SKUCode: "AGG-MIXED",
		WarehouseIDs: []string{f.sz.ID},
	})
	if err != nil {
		t.Fatalf("建混合商品失败: %v", err)
	}
	ref, rerr := f.inventory.ResolveWarehouse(ctx, f.projectID, f.sh.ID)
	if rerr != nil {
		t.Fatalf("解析第二仓失败: %v", rerr)
	}
	if _, err := f.inventory.EnsureVariantStock(ctx, ref, mixed.ID, mixed.Variants[0].ID, "AGG-MIXED", &five); err != nil {
		t.Fatalf("为第二仓建跟踪行失败: %v", err)
	}
	got, err = f.products.Get(ctx, &productdto.GetReq{ID: mixed.ID})
	if err != nil {
		t.Fatalf("读混合商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateInfinite {
		t.Fatalf("任一仓不跟踪就应显示无限，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}
	if got.StockTotal != 0 {
		t.Fatalf("混合状态**绝不求和**：total 应是 0（页面按 ∞ 显示），实际 %d", got.StockTotal)
	}
	var shQty int
	var shState string
	for _, row := range got.StockWarehouses {
		if row.WarehouseID == f.sh.ID {
			shQty, shState = row.Quantity, row.State
		}
	}
	if shState != productenums.StockStateTracked || shQty != 5 {
		t.Fatalf("分仓明细要能区分三态：SH 应是跟踪 5，实际 state=%q qty=%d", shState, shQty)
	}
}

func TestProductCreateReuseInSecondWarehouse(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.wpickProduct(t, "第二仓的既有货", "second-wh-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "SHARE-1", host.ID, host.Variants[0].ID)

	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "两仓一旧一新", Slug: "second-wh-reuse",
		SKUCode: "SHARE-1", WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err != nil {
		t.Fatalf("一半新建一半复用应成功: %v", err)
	}

	if got := f.wstockRowCount(t, f.sz.ID, "SHARE-1"); got != 1 {
		t.Fatalf("第一个仓应新建一行，实际 %d 行", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "SHARE-1"); got != 1 {
		t.Fatalf("已有同裸码的仓必须复用（仍是一行），实际 %d 行", got)
	}
	if got := f.wstockRows(t, p.ID); len(got) != 1 {
		t.Fatalf("该商品自己的库存行只有 SZ 那一条，实际 %+v", got)
	}
}

func TestProductCreateContainerSKUTakenIsBusinessError(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "占位商品", Slug: "taken-holder", SKUCode: "TAKEN-1",
	}); err != nil {
		t.Fatalf("建占位商品失败: %v", err)
	}

	if _, err := cfCreateWith(f.products, url.Values{
		"projectId": {f.projectID}, "name": {"重复编码的商品"}, "slug": {"taken-dup"},
		"type": {"variant"}, "sku": {"TAKEN-1"},
	}); err == nil || err.Error() != productenums.ErrContainerSKUTaken {
		t.Fatalf("撞别人主体编码应返回 %s，实际 %v", productenums.ErrContainerSKUTaken, err)
	}
}

func TestProductUpdateSKUPrecheckRejectsDuplicate(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "别人", Slug: "upd-other", SKUCode: "TAKEN-2",
	})
	if err != nil {
		t.Fatalf("建「别人」失败: %v", err)
	}
	mine, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "我自己", Slug: "upd-mine", SKUCode: "MINE-1",
	})
	if err != nil {
		t.Fatalf("建「我自己」失败: %v", err)
	}
	foreign := f.wpickContainerSKU(t, other.ID)
	_, err = f.products.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, SKUCode: &foreign,
	})
	if err == nil || err.Error() != productenums.ErrContainerSKUTaken {
		t.Fatalf("撞别人的主体编码应返回 %s，实际 %v", productenums.ErrContainerSKUTaken, err)
	}

	own := f.wpickContainerSKU(t, mine.ID)
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, SKUCode: &own,
	}); err != nil {
		t.Fatalf("改回自己的编码不该被拒: %v", err)
	}
	if got := f.wpickContainerSKU(t, mine.ID); got != own {
		t.Fatalf("编码应保持 %q，实际 %q", own, got)
	}
}

func TestProductCreateRollsBackWhenChangeRecordFails(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	f.rewireProducts(&failingChangesPort{err: errors.New("留痕端口故障（故障注入）")})
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "留痕失败的商品", Slug: "rollback-1", SKUCode: "ROLLBACK-1",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err == nil {
		t.Fatal("留痕失败时创建必须整体失败")
	}
	var bySlug int
	if derr := f.db.Raw("SELECT COUNT(*) FROM products WHERE slug = ?", "rollback-1").Scan(&bySlug).Error; derr != nil {
		t.Fatalf("统计商品失败: %v", derr)
	}
	if bySlug != 0 {
		t.Fatalf("事务应整体回滚：products 里不该有这一行，实际 %d", bySlug)
	}
	assertNoTraceOfSKU(t, f, "SZ_ROLLBACK-1", "ROLLBACK-1")
}

func TestSaveVariantListRollsBackOnFailure(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	yes := true
	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: &yes,
		Values: []productdto.AttributeValueReq{{Label: "红", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "清单回滚", Slug: "list-rollback",
		SKUCode: "LIST-1", AttributeIDs: []string{color.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	originalSKU := f.wpickContainerSKU(t, p.ID)
	if got := f.wvariantCount(t, p.ID); got != 1 {
		t.Fatalf("初始应有一个变体，实际 %d", got)
	}

	f.rewireProducts(&failingChangesPort{err: errors.New("留痕端口故障（故障注入）")})
	_, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: p.ID, ProjectID: f.projectID,
		Rows: []productdto.VariantListResp{

			{SKUCode: "LIST-1_RED_V", OptionValues: json.RawMessage(`{"color":"red"}`)},
		},
	})
	if err == nil {
		t.Fatal("留痕失败时保存清单必须整体失败")
	}
	if got := f.wvariantCount(t, p.ID); got != 1 {
		t.Fatalf("事务应整体回滚：既不该新增也不该删除，变体数仍应是 1，实际 %d", got)
	}
	var kept int
	if derr := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ? AND sku_code = ?",
		p.ID, originalSKU).Scan(&kept).Error; derr != nil {
		t.Fatalf("统计变体失败: %v", derr)
	}
	if kept != 1 {
		t.Fatalf("原有的那个变体应还在（清单外的删除也回滚），实际 %d", kept)
	}
}

func TestProductPageCreateMultiWarehouseForm(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", pageview.UseRenderer(pageview.RenderPage), handle.ProductsPage)

	engine.GET("/admin/products/new", pageview.UseRenderer(pageview.RenderPage), handle.ProductNewPage)

	pageRec := httptest.NewRecorder()
	engine.ServeHTTP(pageRec, httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+f.projectID, nil))
	page := pageRec.Body.String()
	for _, want := range []string{
		`name="warehouseIds"`, `data-warehouse-check`, `name="trackQuantity"`,
		`data-quantity-input`, `data-sku-candidates`, `data-sku-input`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建抽屉缺少 %q（多仓 / 数量 / SKU 候选控件没渲染出来）", want)
		}
	}

	created, err := cfCreateWith(f.products, url.Values{
		"projectId": {f.projectID}, "name": {"表单多仓"}, "slug": {"form-multi-wh"},
		"type": {"variant"}, "sku": {"FORM-1"},

		"warehouseIds":  {f.sz.ID, f.sh.ID},
		"trackQuantity": {"1"}, "quantity": {"3"},
	})
	if err != nil {
		t.Fatalf("建商品失败（字段名分叉会让勾的仓与数量静默丢掉）: %v", err)
	}
	productID := created.ID
	rows := f.wstockRows(t, productID)
	if len(rows) != 2 {
		t.Fatalf("勾了两个仓应各建一行（表单字段名分叉会让数量静默丢掉），实际 %+v", rows)
	}
	for _, row := range rows {
		if row.SKUCode != "FORM-1" || !row.TrackQuantity || row.Quantity != 3 {
			t.Fatalf("数量 3 应写成跟踪行（裸码 FORM-1），实际 %+v", row)
		}
	}
}

func TestProductPageCreateUntrackedQuantityNeverPrefilled(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	post := func(name, slug, sku string, form url.Values) {
		t.Helper()
		form.Set("projectId", f.projectID)
		form.Set("name", name)
		form.Set("slug", slug)
		form.Set("sku", sku)
		form.Set("type", "variant")
		if _, err := cfCreateWith(f.products, form); err != nil {
			t.Fatalf("创建 %s 失败：%v", name, err)
		}
	}
	productIDOf := func(slug string) string {
		t.Helper()
		var id string
		if err := f.db.Raw("SELECT id FROM products WHERE slug = ?", slug).Scan(&id).Error; err != nil {
			t.Fatalf("读商品失败: %v", err)
		}
		return id
	}

	post("不跟踪", "form-untracked", "FORM-U1", url.Values{"warehouseIds": {f.sz.ID}})
	if rows := f.wstockRows(t, productIDOf("form-untracked")); len(rows) != 1 || rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("不跟踪应是不传数量（无限），实际 %+v", rows)
	}

	post("跟踪零", "form-zero", "FORM-Z1", url.Values{
		"warehouseIds": {f.sz.ID}, "trackQuantity": {"1"}, "quantity": {""},
	})
	if rows := f.wstockRows(t, productIDOf("form-zero")); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("跟踪但留空应写 0（明确没货），实际 %+v", rows)
	}

	if _, err := cfCreateWith(f.products, url.Values{
		"projectId": {f.projectID}, "name": {"负数"}, "slug": {"form-neg"},
		"type": {"variant"}, "sku": {"FORM-N1"}, "warehouseIds": {f.sz.ID},
		"trackQuantity": {"1"}, "quantity": {"-1"},
	}); err == nil || err.Error() != inventoryenums.ErrStockQuantityInvalid {
		t.Fatalf("负数数量应返回 %s，实际 %v", inventoryenums.ErrStockQuantityInvalid, err)
	}
	if id := productIDOf("form-neg"); id != "" {
		t.Fatalf("被拒的请求不该落库，实际商品 %s", id)
	}
}

func TestProductsPageStockColumnRendersThreeStates(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	five := 5
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "有货商品", Slug: "col-tracked", SKUCode: "COL-TRACKED",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID}, Quantity: &five,
	}); err != nil {
		t.Fatalf("建有货商品失败: %v", err)
	}
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "无限商品", Slug: "col-infinite", SKUCode: "COL-INFINITE",
		WarehouseIDs: []string{f.sz.ID},
	}); err != nil {
		t.Fatalf("建无限商品失败: %v", err)
	}
	nonePrice := 9.0
	colNone, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "未入库商品", Slug: "col-none",
		DefaultPrice: &nonePrice, SKUCode: "COL-NONE",
	})
	if err != nil {
		t.Fatalf("建未入库商品失败: %v", err)
	}

	f.wdropStockRows(t, colNone.ID)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", pageview.UseRenderer(pageview.RenderPage), handle.ProductsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	if got := strings.Count(page, `class="stock-cell"`); got != 3 {
		t.Fatalf("三个商品应各有一个库存单元格，实际 %d 个", got)
	}
	for _, want := range []string{
		"∞ 无限",
		"未入库",
		"stock-rows",
		"10",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("库存列缺少 %q", want)
		}
	}
}

type stockFailurePort struct {
	inventorycontract.ProductStockPort
	err error
}

func (s stockFailurePort) WarehouseStocksByProducts(context.Context, string, []string) ([]inventorydto.ProductWarehouseStock, error) {
	return nil, s.err
}

func TestProductsPageStockUnknownRendersFailure(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "故障可见商品", Slug: "col-unknown", SKUCode: "COL-UNKNOWN",
	}); err != nil {
		t.Fatalf("建商品失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	grantProductPerms(engine)

	f.products.SetInventoryService(stockFailurePort{ProductStockPort: f.inventory, err: errors.New("injected: 库存读失败")})
	handle := producthttp.NewProductPageHandle(f.products, f.products)

	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", pageview.UseRenderer(pageview.RenderPage), handle.ProductsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存读取失败不得让列表整体失败，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "故障可见商品") {
		t.Fatal("库存故障不该吞掉商品行（其余列仍应可读）")
	}
	if !strings.Contains(page, "库存读取失败") {
		t.Fatal("库存读取失败必须如实显示，不能按「未入库」渲染")
	}
	if strings.Contains(page, "stock-rows") {
		t.Fatal("故障时不得展开分仓明细：那一屏的每个仓都是伪造的「未入库」")
	}
	if strings.Contains(page, "未入库") {
		t.Fatal("故障不是「未入库」：两种事实必须显示成不同的话")
	}
}

func TestProductStockUnknownI18nSeeded(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	if err := schema.Apply(f.db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'pStockUnknown' AND lang = ?", lang).Scan(&n).Error; err != nil {
			t.Fatalf("查词条失败: %v", err)
		}
		if n == 0 {
			t.Fatalf("%s 的 pStockUnknown 词条缺失（迁移 698 未生效）", lang)
		}
	}
}

func TestContainerSKUTakenI18nSeeded(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	if err := schema.Apply(f.db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'pBoxSkuTaken' AND lang = ?", lang).Scan(&n).Error; err != nil {
			t.Fatalf("查词条失败: %v", err)
		}
		if n == 0 {
			t.Fatalf("%s 的 ErrContainerSKUTaken 词条缺失（迁移 264 未生效）", lang)
		}
	}
}
