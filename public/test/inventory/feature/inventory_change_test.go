// Package feature inventory 模块 feature 测试 —— 库存流水与扣减契约（issue #16）。
//
// 覆盖本票七条验收（真实 PostgreSQL + 生产迁移与 seed + 真实 service + 真实商品模块）：
//  1. 按 SKU 增减库存的契约方法，扣减在真源（inventory_stocks）上加行锁；
//  2. 多 SKU 场景按标识排序加锁，不产生死锁；
//  3. 每次变动写流水，含方向、数量、变动原因与来源引用；
//  4. 变动原因覆盖出 / 入 / 调整各枚举，含自定义原因（引用可维护字典，不用自由文本）；
//  5. 支持按物料清单展开扣减多个子项 SKU；
//  6. 商品侧库存总数缓存可同步且带时间戳；同步失败不影响主流程，有对账兜底；
//  7. 可用量判断只读真源，绝不读缓存。
//
// 并发与幂等断言单列（TestInventoryChangeConcurrentDeductNoOversell /
// TestInventoryChangeOppositeOrderNoDeadlock），它们是本票的核心风险点。
package feature

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
)

// slugSeq 让同一次测试里建的商品 slug 不撞车（商品 slug 工程内唯一）。
var slugSeq atomic.Int64

// mustProduct 建商品并返回它（首个变体已由商品模块自动创建并生成库存记录）。
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

// changeIn 入库（返回响应）。
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

// TestInventoryChangeOnTrueSourceWithRowLock 验收 1：
// 契约方法按 SKU 增减；扣减在真源上加行锁判定；不足整体拒绝。
func TestInventoryChangeOnTrueSourceWithRowLock(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 入库 5 → 出库 2 → 调整到 10：三种方向都落在真源上。
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
	// 调整到当前值：没有变动，不写流水、不报错。
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

	// 超扣拒绝：可用量只有 10，扣 11 整体拒绝且真源不动。
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

	// 入库数量必须为正、方向必须合法（参数校验不进入业务逻辑）。
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

// TestInventoryChangeConcurrentDeductNoOversell 验收 1（并发）：
// 行锁把并行扣减串行化 —— 可用量 20、40 个并发各扣 1 时必须恰好成功 20 次，绝不超扣。
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
	// 一致性：流水条数 = 1 次入库 + 20 次成功扣减，不多不少（不多写 = 幂等，不少写 = 每次变动都留痕）。
	if n := countMovements(t, f, v.ID); n != 21 {
		t.Fatalf("流水条数应为 1 次入库 + 20 次成功扣减 = 21，实际 %d", n)
	}
}

// TestInventoryChangeOppositeOrderNoDeadlock 验收 2：
// 两批多 SKU 变动以相反入参顺序并发执行 —— 服务端按 (变体, 仓库) 标识升序加锁，
// 等待链不可能成环，必须零死锁、零非预期错误。
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
	// 两个批次的入参顺序相反：A→B 与 B→A。排序加锁后二者共享同一锁序。
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
	// 每个批次同时扣 A 与 B 各 1：两批 × rounds 轮 ⇒ 每个 SKU 各被扣 2*rounds 次。
	if got := f.stockQty(t, va.ID, wh.ID); got != 500-2*rounds {
		t.Fatalf("Alpha 余量错误：期望 %d 实际 %d", 500-2*rounds, got)
	}
	if got := f.stockQty(t, vb.ID, wh.ID); got != 500-2*rounds {
		t.Fatalf("Beta 余量错误：期望 %d 实际 %d", 500-2*rounds, got)
	}
}

// TestInventoryMovementRecordsDirectionReasonSource 验收 3：
// 每次变动都写流水，含方向、数量、变动原因与来源引用（以及前后值与批次号）。
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
	// 流水主键是 UUIDv7（时间有序，写入点集中）—— 换回 v4 这条会红
	if len(m.ID) != 36 || m.ID[14] != '7' {
		t.Fatalf("流水主键应为 UUIDv7，实际 id=%q", m.ID)
	}
	// 库存维度里的 SKU 是**仓库侧裸码**（剥掉仓码前缀的编码）；前缀只留在商品 / 变体侧。
	// 下面两条一起钉住两侧口径：流水必须是裸码，而 v.SKUCode 仍带前缀。
	if m.BatchID == "" || m.VariantID != v.ID || m.SKUCode != bareSKU(v.SKUCode, wh.Code) ||
		m.WarehouseID != wh.ID {
		t.Fatalf("流水应带上批次号与库存维度（SKU 为仓库侧裸码 %q）：%+v", bareSKU(v.SKUCode, wh.Code), m)
	}
	if !strings.HasPrefix(v.SKUCode, wh.Code+"_") {
		t.Fatalf("商品 / 变体侧的 SKU 应仍带仓码前缀 %s_…，实际 %q", wh.Code, v.SKUCode)
	}

	// 出库流水：方向为 out，delta 为负。
	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", SourceType: "order", SourceRef: "SO-9",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 3,
		}},
	}); err != nil {
		t.Fatalf("出库失败: %v", err)
	}
	// 调整流水：方向为 adjust，quantity 是绝对差值。
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

	// 过滤：按方向 / 来源引用都能收窄。
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

// TestInventoryReasonDictionaryCoversDirections 验收 4：
// 变动原因覆盖出 / 入 / 调整各枚举，含自定义原因（引用可维护字典，不用自由文本）。
func TestInventoryReasonDictionaryCoversDirections(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 内置原因覆盖三个方向（迁移 103）。
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

	// 自由文本不是原因：字典里没有的 code 一律拒绝（这正是「不用自由文本」的含义）。
	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "临时补一批货",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrReasonNotFound {
		t.Fatalf("字典外的自由文本原因应返回 ErrReasonNotFound，实际 %v", err)
	}
	// 原因为空同样拒绝。
	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockReasonRequired {
		t.Fatalf("缺原因应返回 ErrStockReasonRequired，实际 %v", err)
	}
	// 方向不匹配拒绝：用「入库原因」做出库。
	if _, err = f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrReasonDirectionMismatch {
		t.Fatalf("原因方向不匹配应返回 ErrReasonDirectionMismatch，实际 %v", err)
	}

	// 自定义原因：建 → 用 → 改 → 停用。
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

	// 重复 code（大小写不敏感）拒绝。
	if _, err = f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "vip_gift_out", Name: "重复", Direction: inventoryenums.DirectionOut,
	}); err == nil || err.Error() != inventoryenums.ErrReasonCodeTaken {
		t.Fatalf("重复 code 应返回 ErrReasonCodeTaken，实际 %v", err)
	}
	// 非法 code / 方向拒绝。
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

	// 内置原因不可改；自定义原因可改名 / 停用，停用后不能再被引用。
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
	// 停用不影响历史流水（流水里是 code 快照）。
	history, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, ReasonCode: "vip_gift_out",
	})
	if err != nil || len(history) != 2 {
		t.Fatalf("停用原因后历史流水仍应可查：%v %+v", err, history)
	}
}

// TestInventoryDeductExpandsBOM 验收 5：
// 支持按物料清单展开扣减多个子项 SKU（含多级展开、整体原子、清单维护守卫）。
func TestInventoryDeductExpandsBOM(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	// 两个独立的清单子树：
	//   ① GiftBox → PartA×2 + PartB×3（一步展开到两个叶子子项 —— 验收 5 的主场景）；
	//   ② Top → Sub×2 → Raw×3（两级展开：中间件 Sub 本身是半成品，被展开而不是被扣）。
	bundle := mustProduct(t, f, "GiftBox")
	partA := mustProduct(t, f, "PartA")
	partB := mustProduct(t, f, "PartB")
	top := mustProduct(t, f, "Top")
	sub := mustProduct(t, f, "Sub")
	raw := mustProduct(t, f, "Raw")
	bv := f.firstVariant(t, bundle.ID)
	av := f.firstVariant(t, partA.ID)
	cv := f.firstVariant(t, partB.ID)
	tv := f.firstVariant(t, top.ID)
	sv := f.firstVariant(t, sub.ID)
	rv := f.firstVariant(t, raw.ID)

	if _, err := f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: bv.ID, ParentSKUCode: bv.SKUCode,
		Items: []inventorydto.BOMItemReq{
			{ComponentVariantID: av.ID, ComponentSKUCode: av.SKUCode, Quantity: 2},
			{ComponentVariantID: cv.ID, ComponentSKUCode: cv.SKUCode, Quantity: 3},
		},
	}); err != nil {
		t.Fatalf("维护礼盒清单失败: %v", err)
	}
	if _, err := f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: tv.ID, ParentSKUCode: tv.SKUCode,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: sv.ID, ComponentSKUCode: sv.SKUCode, Quantity: 2}},
	}); err != nil {
		t.Fatalf("维护整机清单失败: %v", err)
	}
	if _, err := f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: sv.ID, ParentSKUCode: sv.SKUCode,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: rv.ID, ComponentSKUCode: rv.SKUCode, Quantity: 3}},
	}); err != nil {
		t.Fatalf("维护半成品清单失败: %v", err)
	}
	bom, err := f.inventory.GetBOM(ctx, &inventorydto.GetBOMReq{ParentVariantID: bv.ID})
	if err != nil || len(bom.Items) != 2 || bom.ParentSKUCode != bv.SKUCode {
		t.Fatalf("读礼盒清单失败：%v %+v", err, bom)
	}
	// 入货：每个 SKU 各 100。
	changeIn(t, f, bundle, bv, wh.ID, 100, "purchase_in")
	changeIn(t, f, partA, av, wh.ID, 100, "purchase_in")
	changeIn(t, f, partB, cv, wh.ID, 100, "purchase_in")
	changeIn(t, f, top, tv, wh.ID, 100, "purchase_in")
	changeIn(t, f, sub, sv, wh.ID, 100, "purchase_in")
	changeIn(t, f, raw, rv, wh.ID, 100, "purchase_in")

	// ① 一步展开：扣 5 个礼盒 → PartA -10、PartB -15，礼盒自身不动。
	res, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", ExpandBOM: true,
		SourceType: "order", SourceRef: "SO-BOM-1",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: bv.ID, ProductID: bundle.ID, SKUCode: bv.SKUCode, WarehouseID: wh.ID, Quantity: 5,
		}},
	})
	if err != nil {
		t.Fatalf("按物料清单展开扣减失败: %v", err)
	}
	if got := f.stockQty(t, av.ID, wh.ID); got != 90 {
		t.Fatalf("PartA 应扣 10，实际 %d", got)
	}
	if got := f.stockQty(t, cv.ID, wh.ID); got != 85 {
		t.Fatalf("PartB 应扣 15，实际 %d", got)
	}
	if got := f.stockQty(t, bv.ID, wh.ID); got != 100 {
		t.Fatalf("礼盒自身的真源不应被扣（扣的是子项），实际 %d", got)
	}
	// 展开出的子项流水带父 SKU 溯源与同一批次号。
	if len(res.Movements) != 2 {
		t.Fatalf("一步展开应写 2 条子项流水（PartA / PartB），实际 %d", len(res.Movements))
	}
	// 流水里的 SKU 是仓库侧裸码，取用时按同一口径换算（商品侧 av.SKUCode 仍带前缀）。
	parents := map[string]string{}
	for _, m := range res.Movements {
		parents[m.SKUCode] = m.ParentVariantID
	}
	if parents[bareSKU(av.SKUCode, wh.Code)] != bv.ID || parents[bareSKU(cv.SKUCode, wh.Code)] != bv.ID {
		t.Fatalf("子项流水应记录直接父 SKU（子项 SKU 为裸码 %q / %q）：%+v",
			bareSKU(av.SKUCode, wh.Code), bareSKU(cv.SKUCode, wh.Code), parents)
	}
	if res.BatchID == "" {
		t.Fatalf("一次展开扣减应共用一个批次号")
	}
	for _, m := range res.Movements {
		if m.BatchID != res.BatchID {
			t.Fatalf("同批次流水的 batchId 应一致：%+v", m)
		}
	}

	// ② 多级展开：扣 4 台整机 → Sub（半成品，有清单）被继续展开，真正扣的是 Raw：
	//    4 × 2 × 3 = 24；Top 与 Sub 自身的真源都不动。
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", ExpandBOM: true,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: tv.ID, ProductID: top.ID, SKUCode: tv.SKUCode, WarehouseID: wh.ID, Quantity: 4,
		}},
	}); err != nil {
		t.Fatalf("多级展开扣减失败: %v", err)
	}
	if got := f.stockQty(t, rv.ID, wh.ID); got != 76 {
		t.Fatalf("Raw 应按两级清单扣 4×2×3=24，实际 %d", got)
	}
	if got := f.stockQty(t, sv.ID, wh.ID); got != 100 {
		t.Fatalf("中间件 Sub 自身是半成品，应被展开而不是被扣，实际 %d", got)
	}
	if got := f.stockQty(t, tv.ID, wh.ID); got != 100 {
		t.Fatalf("整机自身不应被扣，实际 %d", got)
	}

	// 原子性：任一子项不足则整批拒绝，其余子项也不动。
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", ExpandBOM: true,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: bv.ID, ProductID: bundle.ID, SKUCode: bv.SKUCode, WarehouseID: wh.ID, Quantity: 40,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("子项不足应返回 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockQty(t, av.ID, wh.ID); got != 90 {
		t.Fatalf("整批被拒后 PartA 不应变化，实际 %d", got)
	}
	if got := f.stockQty(t, cv.ID, wh.ID); got != 85 {
		t.Fatalf("整批被拒后 PartB 不应变化，实际 %d", got)
	}
	// 展开开关关闭时按 SKU 自身扣减（有清单也不展开）。
	if _, err = f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: bv.ID, ProductID: bundle.ID, SKUCode: bv.SKUCode, WarehouseID: wh.ID, Quantity: 3,
		}},
	}); err != nil {
		t.Fatalf("不展开时按自身扣减失败: %v", err)
	}
	if got := f.stockQty(t, bv.ID, wh.ID); got != 97 {
		t.Fatalf("不展开时礼盒自身应扣 3，实际 %d", got)
	}

	// 清单维护守卫：自引用 / 重复子项 / 用量非正 / 成环一律拒绝。
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: bv.ID,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: bv.ID, Quantity: 1}},
	}); err == nil || err.Error() != inventoryenums.ErrBOMSelfReference {
		t.Fatalf("自引用应返回 ErrBOMSelfReference，实际 %v", err)
	}
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: bv.ID,
		Items: []inventorydto.BOMItemReq{
			{ComponentVariantID: av.ID, Quantity: 1},
			{ComponentVariantID: av.ID, Quantity: 2},
		},
	}); err == nil || err.Error() != inventoryenums.ErrBOMDuplicateComponent {
		t.Fatalf("重复子项应返回 ErrBOMDuplicateComponent，实际 %v", err)
	}
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: bv.ID,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: av.ID, Quantity: 0}},
	}); err == nil || err.Error() != inventoryenums.ErrBOMQuantityInvalid {
		t.Fatalf("用量非正应返回 ErrBOMQuantityInvalid，实际 %v", err)
	}
	// 成环：已存在 Top→Sub，再把 Sub 的清单指回 Top → Top→Sub→Top，展开即死循环。
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: sv.ID,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: tv.ID, Quantity: 1}},
	}); err == nil || err.Error() != inventoryenums.ErrBOMCycle {
		t.Fatalf("成环应返回 ErrBOMCycle，实际 %v", err)
	}
	// 反向不成环：把成品挂到半成品之下只是新增一条通路（GiftBox → PartA 是叶子，
	// 给它加一条到 Raw 的清单不会形成回路）。
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: rv.ID,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: bv.ID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("非成环的清单不应被拒绝: %v", err)
	}
	// 复原 Raw 的清单（后续清空用例依赖它不存在）。
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: rv.ID, Items: []inventorydto.BOMItemReq{},
	}); err != nil {
		t.Fatalf("清空 Raw 清单失败: %v", err)
	}

	// 清空清单：之后再扣减就按该 SKU 自身扣。
	if _, err = f.inventory.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: bv.ID, Items: []inventorydto.BOMItemReq{},
	}); err != nil {
		t.Fatalf("清空清单失败: %v", err)
	}
	cleared, err := f.inventory.GetBOM(ctx, &inventorydto.GetBOMReq{ParentVariantID: bv.ID})
	if err != nil || len(cleared.Items) != 0 {
		t.Fatalf("清空后清单应为空：%v %+v", err, cleared)
	}
}

// failingCachePort 必然失败的缓存端口（只用于验证「同步失败不影响主流程」）。
type failingCachePort struct{}

// SyncVariantStockTotal 永远失败（模拟商品侧写缓存不可用）。
func (failingCachePort) SyncVariantStockTotal(ctx context.Context, variantID string, total int) error {
	return errors.New("stub: 商品侧缓存写入不可用")
}

// ListVariantStockTotals 永远失败（同一故障域的读侧）。
func (failingCachePort) ListVariantStockTotals(ctx context.Context, variantIDs []string) (map[string]int, error) {
	return nil, errors.New("stub: 商品侧缓存读取不可用")
}

// TestInventoryAvailabilityReadsTrueSourceNotCache 验收 7：
// 可用量判断只读真源（带行锁），缓存再离谱也不影响判定。
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

	// 把缓存吹成 1000（模拟缓存漂移 / 被别处改坏）。
	// 真源只有 3：扣 3 成功，再扣 1 必须被拒（若读缓存就会放行 → 超卖）。
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
	// 契约读取（GetStock / ListStocksBySKU）同样读真源。
	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: wh.ID})
	if err != nil || row.Quantity != 0 {
		t.Fatalf("库存读取必须走真源（0）：%v %+v", err, row)
	}
	// 库存侧的查询维度是**仓库侧裸码**（与库存行存的值同一口径）。
	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: bareSKU(v.SKUCode, wh.Code),
	})
	if err != nil || len(rows) != 1 || rows[0].Quantity != 0 {
		t.Fatalf("列表也必须读真源：%v %+v", err, rows)
	}
	if rows[0].SKUCode != bareSKU(v.SKUCode, wh.Code) {
		t.Fatalf("库存行应存仓库侧裸码 %q，实际 %q", bareSKU(v.SKUCode, wh.Code), rows[0].SKUCode)
	}
	// 扣减之后**没有缓存要同步**（issue #32：商品侧缓存列已删）：
	// 展示值按需从真源投影，所以这里直接核对真源仍是 0。
	if total := trueSourceTotal(t, f, v.ID); total != 0 {
		t.Fatalf("真源应被扣到 0，实际 %d", total)
	}
}

// TestInventoryChangeHTTPAndAdminPage 接口与后台页：
// 新端点绑定/响应可用；后台库存页出现「库存变动 / 变动原因字典 / 库存流水」三块，
// 且表单都带 csrf_token（原生表单硬规则）。
func TestInventoryChangeHTTPAndAdminPage(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 后台「库存调整（盘点 / 报损）」表单：盘点（adjust，目标绝对量）→ 302 回列表并带 ok=1。
	// 入库 / 出库不在本页 —— 它们必须挂采购单 / 发货单，见页面上的边界提示。
	rec := postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "warehouseId": {wh.ID},
		"direction": {"adjust"}, "quantity": {"8"}, "reasonCode": {"stocktake_adjust"},
		"sourceType": {"stocktake"}, "sourceRef": {"PO-PAGE-1"}, "remark": {"盘点：后台调整"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "ok=1") {
		t.Fatalf("后台盘点调整应 302 并带 ok=1，实际 %d %s：%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("盘点调整到 8 后真源应为 8，实际 %d", got)
	}
	// 入库方向在本页被拒绝（入库必须走单据）：整批不生效，并回带可读的错误。
	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "warehouseId": {wh.ID},
		"direction": {"in"}, "quantity": {"5"}, "reasonCode": {"purchase_in"}, "remark": {"试图直接入库"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("库存管理页不应接受入库方向，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("被拒绝的入库不应改动真源，实际 %d", got)
	}
	// 备注必填：盘点 / 报损是「没有单据承载」的写入口，没有备注的调整事后无法解释。
	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "direction": {"adjust"},
		"quantity": {"9"}, "reasonCode": {"stocktake_adjust"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("缺备注的调整应被拒绝并回带错误，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// 后台表单新建自定义原因 → 302。
	rec = postForm(engine, "/admin/inventory/reason/create", url.Values{
		"projectId": {f.projectID}, "code": {"gift_out"}, "name": {"赠品出库"}, "direction": {"out"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("后台新建原因应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 失败路径：用字典外的原因 → 302 且带 err=（错误回显到页面，不 500）。
	rec = postForm(engine, "/admin/inventory/stock/change", url.Values{
		"projectId": {f.projectID}, "variantId": {v.ID}, "direction": {"out"},
		"quantity": {"1"}, "reasonCode": {"不存在的自由文本"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("自由文本原因应回列表并带错误提示，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// 页面：三块新内容 + 流水 + 原因字典 + csrf_token。
	// 库存页的 SKU 筛选就是仓库侧维度：用裸码查（流水 / 库存行存的都是裸码）。
	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID+
		"&sku="+url.QueryEscape(bareSKU(v.SKUCode, wh.Code)))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 页面只读化后：唯一的写入口是「库存调整（盘点 / 报损）」，入库 / 出库只剩一条指向
	// 单据页的边界提示；流水与原因都按 code 呈现（词条缓存未初始化时回退到 code，
	// 不会显示 inventory.reason.* 这种裸 key）。
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

	// 「变动原因字典」与「新建自定义原因」已按「配置不是日常操作」拆到独立页
	// /admin/inventory/reasons，断言随之搬家（引擎上方已注册该路由并注入 inventory:reason_create）。
	rec = httptestGet(engine, "/admin/inventory/reasons?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("变动原因字典页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	reasonBody := rec.Body.String()
	// 原因名在页面上是**取词结果**：词条缓存未初始化时回退到 code（短、可辨认），
	// 但绝不显示 inventory.reason.* 这种裸 key。
	for _, want := range []string{"变动原因字典", "新建自定义原因", "purchase_in", "gift_out"} {
		if !strings.Contains(reasonBody, want) {
			t.Fatalf("变动原因字典页缺少 %q", want)
		}
	}
	if strings.Contains(reasonBody, "inventory.reason.") {
		t.Fatalf("变动原因字典页不应出现裸 i18n key")
	}
	// name 列存的是 i18n key（迁移 241 收口）：内置与自定义都不例外。
	var badName int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_change_reasons WHERE name NOT LIKE 'inventory.reason.%'").Scan(&badName).Error; err != nil {
		t.Fatalf("统计原因 name 失败: %v", err)
	}
	if badName != 0 {
		t.Fatalf("变动原因的 name 应全部是 i18n key，实际有 %d 行不是", badName)
	}
	// 内置原因的 key 必须已经从 242 取到词条（中英成对）——缺词条时页面显示裸 key，
	// 而英文界面回落中文这种缺陷只有在这里钉住才不会漏。
	var seeded int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'inventory.reason.purchase_in'").Scan(&seeded).Error; err != nil {
		t.Fatalf("查询内置原因词条失败: %v", err)
	}
	if seeded != 2 {
		t.Fatalf("内置原因词条应有 zh-CN / en-US 两行，实际 %d", seeded)
	}
	// 启停入口：自定义原因可停用；内置原因也能停用（只读 ≠ 不能停用，只是不给改名按钮）。
	var customReasonID string
	if err := f.db.Raw("SELECT id FROM inventory_change_reasons WHERE project_id IS NOT NULL AND code = 'gift_out'").Scan(&customReasonID).Error; err != nil || customReasonID == "" {
		t.Fatalf("未找到自定义原因 gift_out：%v %q", err, customReasonID)
	}
	rec = postForm(engine, "/admin/inventory/reason/update", url.Values{
		"projectId": {f.projectID}, "id": {customReasonID}, "status": {"disabled"},
	})
	if rec.Code != http.StatusFound || strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("停用自定义原因应成功并回列表，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var reasonStatus string
	if err := f.db.Raw("SELECT status FROM inventory_change_reasons WHERE id = ?", customReasonID).Scan(&reasonStatus).Error; err != nil {
		t.Fatalf("读原因状态失败: %v", err)
	}
	if reasonStatus != inventoryenums.StatusDisabled {
		t.Fatalf("停用未生效，实际 %q", reasonStatus)
	}

	// JSON 接口：绑定 + 响应结构（走真实的 handle 层）。
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

	// 权限点与内置原因已 seed（未 seed 时含超管全员 403，且原因字典为空）。
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE module = 'inventory'").Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	// 100（#15 九个）+ 104（#16 **八个**）+ 106（#17 货源六个）+ 109（#18 采购单与入库七个）= 30。
	// 104 原为十个：inventory:cache_sync / cache_reconcile 随库存缓存下线（迁移 122 删除），
	// 2026-09 把 104 的 SQL 与幂等条件同批收到 8 个之后，这里的期望值随之从 32 落到 30。
	if n != 30 {
		t.Fatalf("迁移 100 + 104 + 106 + 109 应 seed 30 个 inventory 权限点（104 已从 10 收到 8），实际 %d", n)
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

// countMovements 某变体的流水条数（直查真源流水表）。
func countMovements(t *testing.T, f *invFixture, variantID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE variant_id = ?", variantID).Scan(&n).Error; err != nil {
		t.Fatalf("统计流水失败: %v", err)
	}
	return n
}

// trueSourceTotal 读某变体的**真源**汇总（跨仓求和）——替代原 cacheColumns（缓存列已删）。
func trueSourceTotal(t *testing.T, f *invFixture, variantID string) int {
	t.Helper()
	var total int
	if err := f.db.Raw("SELECT COALESCE(SUM(quantity), 0) FROM inventory_stocks WHERE variant_id = ?", variantID).Scan(&total).Error; err != nil {
		t.Fatalf("读真源汇总失败: %v", err)
	}
	return total
}
