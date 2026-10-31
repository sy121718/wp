package feature

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	productdto "go_wp/internal/module/product/dto"
)

var slugSeq atomic.Int64

func mustProduct(t *testing.T, f *invFixture, name string) *productdto.ProductResp {
	t.Helper()
	slug := fmt.Sprintf("%s-%d", strings.ToLower(name), slugSeq.Add(1))
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return p
}

func changeIn(t *testing.T, f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, quantity int, reasonCode string) *inventorydto.StockChangeResp {
	t.Helper()
	res, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: reasonCode,
		SourceType: "purchase", SourceRef: "PO-1",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: warehouseID, Quantity: quantity,
		}},
	})
	if err != nil {
		t.Fatalf("入库 %d 失败: %v", quantity, err)
	}
	return res
}

func TestInventoryChangeOnTrueSourceWithRowLock(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("入库 5 后真源应为 5，实际 %d", got)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "sale_out",
		SourceType: "order", SourceRef: "SO-1",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 2,
		}},
	}); err != nil {
		t.Fatalf("出库 2 失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 3 {
		t.Fatalf("出库 2 后真源应为 3，实际 %d", got)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 10,
		}},
	}); err != nil {
		t.Fatalf("盘点调整到 10 失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("调整到 10 后真源应为 10，实际 %d", got)
	}

	before := countMovements(t, f, v.ID)
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 10,
		}},
	}); err != nil {
		t.Fatalf("调整到当前值应成功（无变动）：%v", err)
	}
	if after := countMovements(t, f, v.ID); after != before {
		t.Fatalf("无变动的调整不应写流水：前 %d 后 %d", before, after)
	}

	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 11,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("超扣应返回 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("超扣被拒后真源不应变化，实际 %d", got)
	}

	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{VariantID: v.ID, WarehouseID: wh.ID, Quantity: 0}},
	}); err == nil || err.Error() != inventoryenums.ErrStockQuantityInvalid {
		t.Fatalf("入库数量 0 应返回 ErrStockQuantityInvalid，实际 %v", err)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: "sideways", ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{VariantID: v.ID, WarehouseID: wh.ID, Quantity: 1}},
	}); err == nil || err.Error() != inventoryenums.ErrStockDirectionInvalid {
		t.Fatalf("非法方向应返回 ErrStockDirectionInvalid，实际 %v", err)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{ProjectID: f.projectID}); err == nil {
		t.Fatalf("空清单应被拒绝")
	}
}

func TestInventoryChangeConcurrentDeductNoOversell(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Limited")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 20, "purchase_in")

	const workers = 40
	var ok, insufficient, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
				ProjectID: f.projectID, ReasonCode: "sale_out",
				Lines: []inventorydto.StockChangeLineReq{{
					VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
				}},
			})
			switch {
			case err == nil:
				ok.Add(1)
			case err.Error() == inventoryenums.ErrStockInsufficient:
				insufficient.Add(1)
			default:
				other.Add(1)
				t.Errorf("并发扣减出现非预期错误: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if other.Load() != 0 {
		t.Fatalf("并发扣减不应出现死锁 / 其它错误，实际 %d 次", other.Load())
	}
	if ok.Load() != 20 {
		t.Fatalf("可用量 20 时应恰好成功 20 次，实际 %d（不足 %d）", ok.Load(), insufficient.Load())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("全部扣完后真源应为 0，实际 %d", got)
	}

	if n := countMovements(t, f, v.ID); n != 21 {
		t.Fatalf("流水条数应为 1 次入库 + 20 次成功扣减 = 21，实际 %d", n)
	}
}

func TestInventoryChangeOppositeOrderNoDeadlock(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	pa := mustProduct(t, f, "Alpha")
	pb := mustProduct(t, f, "Beta")
	va := f.firstVariant(t, pa.ID)
	vb := f.firstVariant(t, pb.ID)
	changeIn(t, f, pa, va, wh.ID, 500, "purchase_in")
	changeIn(t, f, pb, vb, wh.ID, 500, "purchase_in")

	line := func(p *productdto.ProductResp, v *productdto.VariantResp) inventorydto.StockChangeLineReq {
		return inventorydto.StockChangeLineReq{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}
	}

	forward := []inventorydto.StockChangeLineReq{line(pa, va), line(pb, vb)}
	backward := []inventorydto.StockChangeLineReq{line(pb, vb), line(pa, va)}

	const rounds = 30
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
				ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "sale_out",
				Lines: append([]inventorydto.StockChangeLineReq{}, forward...),
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
				ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "sale_out",
				Lines: append([]inventorydto.StockChangeLineReq{}, backward...),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("反向顺序的并发多 SKU 变动不应报错（死锁会让整批失败）：%v", err)
		}
	}

	if got := f.stockQty(t, va.ID, wh.ID); got != 500-2*rounds {
		t.Fatalf("Alpha 余量错误：期望 %d 实际 %d", 500-2*rounds, got)
	}
	if got := f.stockQty(t, vb.ID, wh.ID); got != 500-2*rounds {
		t.Fatalf("Beta 余量错误：期望 %d 实际 %d", 500-2*rounds, got)
	}
}

func TestInventoryMovementRecordsDirectionReasonSource(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	in := changeIn(t, f, p, v, wh.ID, 7, "purchase_in")
	if len(in.Movements) != 1 {
		t.Fatalf("一次入库应恰好一条流水，实际 %d", len(in.Movements))
	}
	m := in.Movements[0]
	if m.Direction != inventoryenums.DirectionIn || m.Quantity != 7 || m.Delta != 7 {
		t.Fatalf("入库流水方向/数量错误：%+v", m)
	}
	if m.QuantityBefore != 0 || m.QuantityAfter != 7 {
		t.Fatalf("流水应记录变动前后值：%+v", m)
	}
	if m.ReasonCode != "purchase_in" || m.ReasonName == "" {
		t.Fatalf("流水应带上变动原因（code + 名称）：%+v", m)
	}
	if m.SourceType != "purchase" || m.SourceRef != "PO-1" {
		t.Fatalf("流水应带上来源引用：%+v", m)
	}

	if len(m.ID) != 36 || m.ID[14] != '7' {
		t.Fatalf("流水主键应为 UUIDv7，实际 id=%q", m.ID)
	}

	if m.BatchID == "" || m.VariantID != v.ID || m.SKUCode != bareSKU(v.SKUCode, wh.Code) ||
		m.WarehouseID != wh.ID {
		t.Fatalf("流水应带上批次号与库存维度（SKU 为仓库侧裸码 %q）：%+v", bareSKU(v.SKUCode, wh.Code), m)
	}
	if !strings.HasPrefix(v.SKUCode, wh.Code+"_") {
		t.Fatalf("商品 / 变体侧的 SKU 应仍带仓码前缀 %s_…，实际 %q", wh.Code, v.SKUCode)
	}

	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", SourceType: "order", SourceRef: "SO-9",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 3,
		}},
	}); err != nil {
		t.Fatalf("出库失败: %v", err)
	}

	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "manual_adjust",
		Remark: "盘点复核",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 20,
		}},
	}); err != nil {
		t.Fatalf("调整失败: %v", err)
	}

	list, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, VariantID: v.ID,
	})
	if err != nil {
		t.Fatalf("查流水失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("应有 3 条流水（入 / 出 / 调整），实际 %d", len(list))
	}
	byDirection := map[string]*inventorydto.MovementResp{}
	for _, row := range list {
		byDirection[row.Direction] = row
	}
	if out := byDirection[inventoryenums.DirectionOut]; out == nil || out.Delta != -3 || out.Quantity != 3 ||
		out.SourceRef != "SO-9" || out.ReasonCode != "sale_out" {
		t.Fatalf("出库流水错误：%+v", out)
	}
	if adj := byDirection[inventoryenums.DirectionAdjust]; adj == nil || adj.Quantity != 16 ||
		adj.QuantityBefore != 4 || adj.QuantityAfter != 20 || adj.Remark != "盘点复核" {
		t.Fatalf("调整流水错误：%+v", adj)
	}

	onlyOut, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, VariantID: v.ID, Direction: inventoryenums.DirectionOut,
	})
	if err != nil || len(onlyOut) != 1 || onlyOut[0].Direction != inventoryenums.DirectionOut {
		t.Fatalf("按方向过滤流水失败：%v %+v", err, onlyOut)
	}
	bySource, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, SourceRef: "PO-1",
	})
	if err != nil || len(bySource) != 1 || bySource[0].ReasonCode != "purchase_in" {
		t.Fatalf("按来源引用过滤流水失败：%v %+v", err, bySource)
	}
}

func TestInventoryReasonDictionaryCoversDirections(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	reasons, err := f.inventory.ListReasons(ctx, &inventorydto.ListReasonReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("查原因字典失败: %v", err)
	}
	perDirection := map[string]int{}
	for _, r := range reasons {
		if !r.IsBuiltin {
			t.Fatalf("内置原因应标记 isBuiltin：%+v", r)
		}
		perDirection[r.Direction]++
	}
	for _, direction := range []string{inventoryenums.DirectionIn, inventoryenums.DirectionOut, inventoryenums.DirectionAdjust} {
		if perDirection[direction] == 0 {
			t.Fatalf("内置原因应覆盖方向 %s，实际 %+v", direction, perDirection)
		}
	}

	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "临时补一批货",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrReasonNotFound {
		t.Fatalf("字典外的自由文本原因应返回 ErrReasonNotFound，实际 %v", err)
	}

	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockReasonRequired {
		t.Fatalf("缺原因应返回 ErrStockReasonRequired，实际 %v", err)
	}

	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrReasonDirectionMismatch {
		t.Fatalf("原因方向不匹配应返回 ErrReasonDirectionMismatch，实际 %v", err)
	}

	custom, err := f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "Vip_Gift_Out", Name: "会员赠品出库", Direction: inventoryenums.DirectionOut,
	})
	if err != nil {
		t.Fatalf("新建自定义原因失败: %v", err)
	}
	if custom.Code != "vip_gift_out" || custom.IsBuiltin || custom.Status != "active" {
		t.Fatalf("自定义原因应归一 code 并标记非内置：%+v", custom)
	}
	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "vip_gift_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 2,
		}},
	}); err != nil {
		t.Fatalf("用自定义原因出库失败: %v", err)
	}
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "vip_gift_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err != nil {
		t.Fatalf("自定义原因应可重复使用: %v", err)
	}

	if _, err = f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "vip_gift_out", Name: "重复", Direction: inventoryenums.DirectionOut,
	}); err == nil || err.Error() != inventoryenums.ErrReasonCodeTaken {
		t.Fatalf("重复 code 应返回 ErrReasonCodeTaken，实际 %v", err)
	}

	if _, err = f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "含中文 code", Name: "非法", Direction: inventoryenums.DirectionOut,
	}); err == nil || err.Error() != inventoryenums.ErrReasonCodeInvalid {
		t.Fatalf("非法 code 应返回 ErrReasonCodeInvalid，实际 %v", err)
	}
	if _, err = f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "weird_dir", Name: "非法方向", Direction: "sideways",
	}); err == nil || err.Error() != inventoryenums.ErrReasonDirectionInvalid {
		t.Fatalf("非法方向应返回 ErrReasonDirectionInvalid，实际 %v", err)
	}

	builtinList, err := f.inventory.ListReasons(ctx, &inventorydto.ListReasonReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("查原因字典失败: %v", err)
	}
	builtinID := ""
	for _, r := range builtinList {
		if r.IsBuiltin && r.Code == "sale_out" {
			builtinID = r.ID
		}
	}
	newName := "改名"
	if _, err = f.inventory.UpdateReason(ctx, &inventorydto.UpdateReasonReq{ID: builtinID, Name: &newName}); err == nil ||
		err.Error() != inventoryenums.ErrReasonBuiltin {
		t.Fatalf("内置原因不可改，应返回 ErrReasonBuiltin，实际 %v", err)
	}
	disabled := "disabled"
	updated, err := f.inventory.UpdateReason(ctx, &inventorydto.UpdateReasonReq{ID: custom.ID, Status: &disabled})
	if err != nil || updated.Status != "disabled" {
		t.Fatalf("停用自定义原因失败：%v %+v", err, updated)
	}
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "vip_gift_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrReasonNotFound {
		t.Fatalf("停用后的原因不能再被引用，实际 %v", err)
	}

	history, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, ReasonCode: "vip_gift_out",
	})
	if err != nil || len(history) != 2 {
		t.Fatalf("停用原因后历史流水仍应可查：%v %+v", err, history)
	}
}

type failingCachePort struct{}

func (failingCachePort) SyncVariantStockTotal(ctx context.Context, variantID string, total int) error {
	return errors.New("stub: 商品侧缓存写入不可用")
}

func (failingCachePort) ListVariantStockTotals(ctx context.Context, variantIDs []string) (map[string]int, error) {
	return nil, errors.New("stub: 商品侧缓存读取不可用")
}

func TestInventoryAvailabilityReadsTrueSourceNotCache(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")

	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 3,
		}},
	}); err != nil {
		t.Fatalf("按真源扣 3 应成功: %v", err)
	}
	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("真源已为 0，缓存 1000 不得放行，实际 %v", err)
	}

	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: wh.ID})
	if err != nil || row.Quantity != 0 {
		t.Fatalf("库存读取必须走真源（0）：%v %+v", err, row)
	}

	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: bareSKU(v.SKUCode, wh.Code),
	})
	if err != nil || len(rows) != 1 || rows[0].Quantity != 0 {
		t.Fatalf("列表也必须读真源：%v %+v", err, rows)
	}
	if rows[0].SKUCode != bareSKU(v.SKUCode, wh.Code) {
		t.Fatalf("库存行应存仓库侧裸码 %q，实际 %q", bareSKU(v.SKUCode, wh.Code), rows[0].SKUCode)
	}

	if total := trueSourceTotal(t, f, v.ID); total != 0 {
		t.Fatalf("真源应被扣到 0，实际 %d", total)
	}
}

func TestInventoryChangeHTTPAndAdminPage(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	rec := postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "warehouseId": {wh.ID},
		"direction": {"adjust"}, "quantity": {"8"}, "reasonCode": {"stocktake_adjust"},
		"sourceType": {"stocktake"}, "sourceRef": {"PO-PAGE-1"}, "remark": {"盘点：后台调整"},
	})

	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("盘点调整到 8 后真源应为 8，实际 %d", got)
	}

	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "warehouseId": {wh.ID},
		"direction": {"in"}, "quantity": {"5"}, "reasonCode": {"purchase_in"}, "remark": {"试图直接入库"},
	})
	assertInventoryJump(t, rec, "err")
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("被拒绝的入库不应改动真源，实际 %d", got)
	}

	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "direction": {"adjust"},
		"quantity": {"9"}, "reasonCode": {"stocktake_adjust"},
	})
	assertInventoryJump(t, rec, "err")

	rec = postForm(engine, "/admin/inventory/reason/create", url.Values{
		"projectId": {f.projectID}, "code": {"gift_out"}, "name": {"赠品出库"}, "direction": {"out"},
	})
	assertInventoryJump(t, rec, "ok")

	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "direction": {"out"},
		"quantity": {"1"}, "reasonCode": {"不存在的自由文本"},
	})
	assertInventoryJump(t, rec, "err")

	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID+
		"&sku="+url.QueryEscape(bareSKU(v.SKUCode, wh.Code)))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		"库存调整（盘点 / 报损）", "库存流水", "stocktake_adjust",
		"提交变动", "PO-PAGE-1", "csrf_token", "生产入库",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("库存页缺少 %q", want)
		}
	}
	if strings.Contains(body, "inventory.reason.") {
		t.Fatalf("库存页不应出现裸 i18n key（原因名要翻译后渲染）")
	}
	if strings.Contains(body, "action=\"/admin/inventory/production\"") {
		t.Fatalf("库存管理页不应再有内联的生产入库表单")
	}

	rec = httptestGet(engine, "/admin/inventory/reasons?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("变动原因字典页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	reasonBody := rec.Body.String()

	for _, want := range []string{"变动原因字典", "新建自定义原因", "purchase_in", "gift_out"} {
		if !strings.Contains(reasonBody, want) {
			t.Fatalf("变动原因字典页缺少 %q", want)
		}
	}
	if strings.Contains(reasonBody, "inventory.reason.") {
		t.Fatalf("变动原因字典页不应出现裸 i18n key")
	}

	var badName int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_change_reasons WHERE name NOT LIKE 'inventory.reason.%'").Scan(&badName).Error; err != nil {
		t.Fatalf("统计原因 name 失败: %v", err)
	}
	if badName != 0 {
		t.Fatalf("变动原因的 name 应全部是 i18n key，实际有 %d 行不是", badName)
	}

	var seeded int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'inventory.reason.purchase_in'").Scan(&seeded).Error; err != nil {
		t.Fatalf("查询内置原因词条失败: %v", err)
	}
	if seeded != 2 {
		t.Fatalf("内置原因词条应有 zh-CN / en-US 两行，实际 %d", seeded)
	}

	var customReasonID string
	if err := f.db.Raw("SELECT id FROM inventory_change_reasons WHERE project_id IS NOT NULL AND code = 'gift_out'").Scan(&customReasonID).Error; err != nil || customReasonID == "" {
		t.Fatalf("未找到自定义原因 gift_out：%v %q", err, customReasonID)
	}
	rec = postForm(engine, "/admin/inventory/reason/update", url.Values{
		"projectId": {f.projectID}, "id": {customReasonID}, "status": {"disabled"},
	})
	assertInventoryJump(t, rec, "ok")
	var reasonStatus string
	if err := f.db.Raw("SELECT status FROM inventory_change_reasons WHERE id = ?", customReasonID).Scan(&reasonStatus).Error; err != nil {
		t.Fatalf("读原因状态失败: %v", err)
	}
	if reasonStatus != inventoryenums.StatusDisabled {
		t.Fatalf("停用未生效，实际 %q", reasonStatus)
	}

	gin.SetMode(gin.TestMode)
	api := gin.New()
	handle := inventoryhttp.NewHandle(f.inventory)
	api.POST("/api/inventory/stock/change", handle.ChangeStock)
	api.GET("/api/inventory/movement/list", handle.ListMovements)
	payload, _ := json.Marshal(&inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 2,
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/stock/change", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("库存变动接口应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Movements []inventorydto.MovementResp `json:"movements"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("解析接口响应失败: %v", err)
	}
	if envelope.Code != http.StatusOK || len(envelope.Data.Movements) != 1 {
		t.Fatalf("接口响应结构不符：%s", rec.Body.String())
	}
	rec = httptestGet(api, "/api/inventory/movement/list?variantId="+v.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "purchase_in") {
		t.Fatalf("流水接口应返回数据，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE module = 'inventory'").Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}

	if n != 28 {
		t.Fatalf("迁移 100 + 104 + 106 + 109 应 seed 28 个 inventory 权限点（104 已从 10 收到 6），实际 %d", n)
	}
	var builtin int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_change_reasons WHERE project_id IS NULL").Scan(&builtin).Error; err != nil {
		t.Fatalf("查询内置原因失败: %v", err)
	}
	if builtin < 9 {
		t.Fatalf("迁移 103 应 seed 至少 9 条内置变动原因，实际 %d", builtin)
	}
	_ = ctx
}

func countMovements(t *testing.T, f *invFixture, variantID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE variant_id = ?", variantID).Scan(&n).Error; err != nil {
		t.Fatalf("统计流水失败: %v", err)
	}
	return n
}

func trueSourceTotal(t *testing.T, f *invFixture, variantID string) int {
	t.Helper()
	var total int
	if err := f.db.Raw("SELECT COALESCE(SUM(quantity), 0) FROM inventory_stocks WHERE variant_id = ?", variantID).Scan(&total).Error; err != nil {
		t.Fatalf("读真源汇总失败: %v", err)
	}
	return total
}
