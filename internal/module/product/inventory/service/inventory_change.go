// inventory_change.go — 按 SKU 增减库存 + 流水（issue #16 验收 1/2/3/5）。
//
// 三条不可动摇的语义：
//
//  1. **判定只读真源**：可用量在 inventory_stocks 的行锁之内判定，绝不读
//     product_variants.stock_total 缓存 —— 缓存滞后会直接变成超卖；
//  2. **加锁顺序由标识决定**：同一批多 SKU / 多仓一律按 (variant_id, warehouse_id)
//     升序加锁（与入参顺序无关）。并发批次因此共享同一个全局锁序，
//     等待链不可能成环 —— 这是「不产生死锁」的实现依据，不是概率问题；
//  3. **有变动必有流水**：数量写回与流水写在同一事务里，一次变动一个 batch_id；
//     调整到与当前值相同的行不写流水（没有变动）。
//
// 缓存同步发生在事务**提交之后**（见 inventory_cache.go）：跨模块写不塞进同一事务。
package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
)

const (
	// maxBatchLines 单次变动的行数上限：一批锁住的行数必须有界，
	// 否则一次请求就能锁住整张真源表。
	maxBatchLines = 200
	// maxBOMDepth 物料清单展开层数上限（成环在维护入口就被拒绝，这里是第二道防线）。
	maxBOMDepth = 8
	// movementDefaultPageSize / movementMaxPageSize 流水列表分页。
	movementDefaultPageSize = 20
	movementMaxPageSize     = 200
)

// stockKey 库存真源的行标识（维度 = SKU × 仓库）。
type stockKey struct {
	variantID   string
	warehouseID string
}

// changeItem 一条待执行的变动项（同一 key 可以有多条：BOM 展开 / 多行入参）。
type changeItem struct {
	key       stockKey
	direction string
	// quantity：in / out 是正数增减量；adjust 是目标绝对量。
	quantity int
	// 落库快照（目标行不存在时用于建行）。
	projectID string
	productID string
	skuCode   string
	reason    *inventorymodel.ReasonEntity
	// parentVariantID 非空表示这条变动来自某个父 SKU 的物料清单展开。
	parentVariantID string
	warehouse       *inventorymodel.WarehouseEntity
	sourceType      string
	sourceRef       string
	remark          string
	operatorID      string
}

// changeOutcome 一次变动的执行结果（供响应组装与提交后的缓存同步使用）。
type changeOutcome struct {
	batchID   string
	movements []*inventorymodel.MovementEntity
	// variants：变体 id → SKU 快照，是提交后缓存同步的对象集合。
	variants map[string]string
	// warehouses / reasonNames：流水回执的展示信息（仓库短码名称、原因名）。
	warehouses  map[string]*inventorymodel.WarehouseEntity
	reasonNames map[string]string
}

// changeMeta 一次变动的公共元数据（来源引用 / 原因 / 备注 / 操作人）。
type changeMeta struct {
	reason     *inventorymodel.ReasonEntity
	sourceType string
	sourceRef  string
	remark     string
	operatorID string
}

// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：单行与多行走同一条加锁路径。
func (s *Service) ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	direction, err := normalizeDirection(req.Direction)
	if err != nil {
		return nil, err
	}
	if len(req.Lines) == 0 {
		return nil, errors.New(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, errors.New(inventoryenums.ErrStockLinesTooMany)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 变动原因必须是字典里的条目，且方向与本次变动一致（不接受自由文本）。
	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, direction)
	if err != nil {
		return nil, err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}
	items, err := s.buildChangeItems(ctx, projectID, direction, req.WarehouseID, req.Lines, meta, "")
	if err != nil {
		return nil, err
	}
	out, err := s.applyStockChanges(ctx, projectID, items)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, direction, out), nil
}

// DeductStock 按 SKU 扣减库存（验收 1/5）：不足即整体拒绝，可按物料清单展开多个子项 SKU。
func (s *Service) DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	if len(req.Lines) == 0 {
		return nil, errors.New(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, errors.New(inventoryenums.ErrStockLinesTooMany)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, inventoryenums.DirectionOut)
	if err != nil {
		return nil, err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}
	items, err := s.buildChangeItems(ctx, projectID, inventoryenums.DirectionOut, req.WarehouseID, req.Lines, meta, "")
	if err != nil {
		return nil, err
	}
	// 按物料清单展开：父 SKU → 子项 SKU × 用量 × 请求量（多级清单逐层展开）。
	if req.ExpandBOM {
		if items, err = s.expandBOM(ctx, items); err != nil {
			return nil, err
		}
	}
	out, err := s.applyStockChanges(ctx, projectID, items)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, inventoryenums.DirectionOut, out), nil
}

// ListMovements 库存流水列表（验收 3：方向 / 数量 / 原因 / 来源引用都可查可过滤）。
func (s *Service) ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	filter := inventorymodel.MovementFilter{
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		ProductID:   strings.TrimSpace(req.ProductID),
		VariantID:   strings.TrimSpace(req.VariantID),
		SKUCode:     strings.TrimSpace(req.SKUCode),
		ReasonCode:  strings.TrimSpace(req.ReasonCode),
		SourceType:  strings.TrimSpace(req.SourceType),
		SourceRef:   strings.TrimSpace(req.SourceRef),
		BatchID:     strings.TrimSpace(req.BatchID),
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return nil, err
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	page, size := movementPageArgs(req)
	rows, err := s.m.ListMovementRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.MovementResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toMovementResp(r))
	}
	return list, nil
}

// buildChangeItems 归一入参并解析每行的仓库 / 数量（方向语义在这里落地）。
func (s *Service) buildChangeItems(ctx context.Context, projectID, direction, defaultWarehouseID string,
	lines []inventorydto.StockChangeLineReq, meta changeMeta, parentVariantID string) (items []changeItem, err error) {
	items = make([]changeItem, 0, len(lines))
	for i := range lines {
		line := lines[i]
		variantID := strings.TrimSpace(line.VariantID)
		if variantID == "" {
			return nil, errors.New(inventoryenums.ErrStockVariantRequired)
		}
		quantity := line.Quantity
		switch direction {
		case inventoryenums.DirectionIn, inventoryenums.DirectionOut:
			if quantity <= 0 {
				return nil, errors.New(inventoryenums.ErrStockQuantityInvalid)
			}
		case inventoryenums.DirectionAdjust:
			if quantity < 0 {
				return nil, errors.New(inventoryenums.ErrStockQuantityInvalid)
			}
		}
		// 仓库逐级兜底：本行指定 → 请求级指定 → 该工程默认仓（缺默认仓显式报错）。
		want := strings.TrimSpace(line.WarehouseID)
		if want == "" {
			want = defaultWarehouseID
		}
		wh, werr := s.resolveWarehouse(ctx, projectID, want)
		if werr != nil {
			return nil, werr
		}
		items = append(items, changeItem{
			key:             stockKey{variantID: variantID, warehouseID: wh.ID},
			direction:       direction,
			quantity:        quantity,
			projectID:       wh.ProjectID,
			productID:       strings.TrimSpace(line.ProductID),
			skuCode:         strings.TrimSpace(line.SKUCode),
			reason:          meta.reason,
			parentVariantID: parentVariantID,
			warehouse:       wh,
			sourceType:      meta.sourceType,
			sourceRef:       meta.sourceRef,
			remark:          meta.remark,
			operatorID:      meta.operatorID,
		})
	}
	return items, nil
}

// applyStockChanges 在**一个事务**内完成全部行的加锁与增减（不留半截状态）。
//
// 事务内的三段式是本票并发安全的全部依据：
//
//	① 幂等建行   —— ON CONFLICT DO NOTHING，按标识升序；并发批次不会插出重复行；
//	② 按序加锁   —— 全部行按同一升序逐一 FOR UPDATE，锁序全局一致（无死锁）；
//	③ 按序应用   —— 在锁内算增减与前后值，写回数量并逐条写流水。
//
// ①②必须分成两趟而不能边建边锁：边建边锁会让两个批次在「各自已持有的行」上互等，
// 那才会真正成环（见 inventory_change_test.go 的对向扣减并发用例）。
func (s *Service) applyStockChanges(ctx context.Context, projectID string, items []changeItem) (out *changeOutcome, err error) {
	if len(items) == 0 {
		return nil, errors.New(inventoryenums.ErrStockLinesRequired)
	}
	now := time.Now().UTC()
	out = &changeOutcome{
		batchID:     uuid.NewString(),
		variants:    map[string]string{},
		warehouses:  map[string]*inventorymodel.WarehouseEntity{},
		reasonNames: map[string]string{},
	}
	for _, it := range items {
		if it.warehouse != nil {
			out.warehouses[it.key.warehouseID] = it.warehouse
		}
		if it.reason != nil {
			out.reasonNames[it.reason.Code] = it.reason.Name
		}
	}
	keys := uniqueSortedKeys(items)
	variantIDs := distinctVariantIDs(keys)

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// 变动前的元数据解析（**不加锁**）：目标行已存在时沿用它的商品 / SKU 快照。
		existing, lerr := s.m.ListStocksByVariantsTx(ctx, tx, variantIDs)
		if lerr != nil {
			return lerr
		}
		byKey := make(map[stockKey]*inventorymodel.StockEntity, len(existing))
		meta := make(map[string]*inventorymodel.StockEntity, len(existing))
		for _, e := range existing {
			byKey[stockKey{variantID: e.VariantID, warehouseID: e.WarehouseID}] = e
			if _, ok := meta[e.VariantID]; !ok {
				meta[e.VariantID] = e
			}
		}

		// ① 幂等建行（按标识升序）。
		pending := make([]*inventorymodel.StockEntity, 0, len(keys))
		for _, k := range keys {
			if _, ok := byKey[k]; ok {
				continue
			}
			item := firstItemOf(items, k)
			productID, skuCode := item.productID, item.skuCode
			if src, ok := meta[k.variantID]; ok {
				if productID == "" {
					productID = src.ProductID
				}
				if skuCode == "" {
					skuCode = src.SKUCode
				}
			}
			if productID == "" {
				return errors.New(inventoryenums.ErrStockProductRequired)
			}
			pending = append(pending, &inventorymodel.StockEntity{
				ID: uuid.NewString(), ProjectID: orString(item.projectID, projectID),
				WarehouseID: k.warehouseID, ProductID: productID, VariantID: k.variantID,
				SKUCode: skuCode, Quantity: 0,
				Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := s.m.EnsureStocksTx(ctx, tx, pending); err != nil {
			return err
		}

		// ② 按同一升序逐一加行锁。
		locked := make(map[stockKey]*inventorymodel.StockEntity, len(keys))
		for _, k := range keys {
			e, lerr := s.m.LockStockRowTx(ctx, tx, k.variantID, k.warehouseID)
			if lerr != nil {
				return lerr
			}
			locked[k] = e
		}

		// ③ 在锁内按序应用（同一 key 的多条项依次累加，各自写一条流水）。
		changed := make(map[stockKey]bool, len(keys))
		for _, it := range sortItems(items) {
			e := locked[it.key]
			before := e.Quantity
			var delta int
			switch it.direction {
			case inventoryenums.DirectionIn:
				delta = it.quantity
			case inventoryenums.DirectionOut:
				delta = -it.quantity
			case inventoryenums.DirectionAdjust:
				delta = it.quantity - before
			}
			after := before + delta
			if after < 0 {
				// 可用量不足：整体拒绝（事务回滚，不留半截扣减）。
				return errors.New(inventoryenums.ErrStockInsufficient)
			}
			if delta == 0 {
				// 调整到当前值：没有发生变动，不写流水。
				continue
			}
			e.Quantity = after
			changed[it.key] = true
			out.variants[it.key.variantID] = e.SKUCode
			out.movements = append(out.movements, movementOf(it, e, before, after, delta, out.batchID, now))
		}
		for _, k := range keys {
			if !changed[k] {
				continue
			}
			if err := s.m.UpdateStockQuantityTx(ctx, tx, locked[k].ID, locked[k].Quantity, now); err != nil {
				return err
			}
		}
		return s.m.CreateMovementsTx(ctx, tx, out.movements)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// finishChange 组装响应，并在事务提交之后执行缓存同步。
//
// 同步失败**不返回错误**：真源已经落库，主流程成功；失败项落台账并由对账兜底。
func (s *Service) finishChange(ctx context.Context, projectID, direction string, out *changeOutcome) *inventorydto.StockChangeResp {
	resp := &inventorydto.StockChangeResp{
		BatchID:       out.batchID,
		Direction:     direction,
		Movements:     make([]*inventorydto.MovementResp, 0, len(out.movements)),
		CacheTotals:   map[string]int{},
		CacheSynced:   true,
		CacheFailures: []string{},
	}
	for _, m := range out.movements {
		item := toMovementRespFromEntity(m)
		item.ReasonName = out.reasonNames[m.ReasonCode]
		if wh := out.warehouses[m.WarehouseID]; wh != nil {
			item.WarehouseCode, item.WarehouseName = wh.Code, wh.Name
		}
		resp.Movements = append(resp.Movements, item)
	}
	resp.CacheSynced = len(resp.CacheFailures) == 0
	return resp
}

// movementOf 构造一条流水（数量写回与流水同事务，故这里只拼装不落库）。
func movementOf(it changeItem, e *inventorymodel.StockEntity, before, after, delta int, batchID string, now time.Time) *inventorymodel.MovementEntity {
	m := &inventorymodel.MovementEntity{
		ID: uuid.NewString(), ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
		ProductID: e.ProductID, VariantID: e.VariantID, SKUCode: e.SKUCode,
		Direction: it.direction, Quantity: abs(delta), Delta: delta,
		QuantityBefore: before, QuantityAfter: after,
		SourceType: it.sourceType, SourceRef: it.sourceRef,
		Remark: it.remark, OperatorID: it.operatorID,
		BatchID: batchID, CreatedAt: now,
	}
	if it.reason != nil {
		code, id := it.reason.Code, it.reason.ID
		m.ReasonCode, m.ReasonID = code, &id
	}
	if it.parentVariantID != "" {
		parent := it.parentVariantID
		m.ParentVariantID = &parent
	}
	return m
}

// —— 工具 ——

// uniqueSortedKeys 去重并按 (变体, 仓库) 标识升序返回锁目标 —— 全局锁序的唯一来源。
func uniqueSortedKeys(items []changeItem) []stockKey {
	seen := make(map[stockKey]bool, len(items))
	keys := make([]stockKey, 0, len(items))
	for _, it := range items {
		if seen[it.key] {
			continue
		}
		seen[it.key] = true
		keys = append(keys, it.key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].variantID != keys[j].variantID {
			return keys[i].variantID < keys[j].variantID
		}
		return keys[i].warehouseID < keys[j].warehouseID
	})
	return keys
}

// sortItems 按同一锁序排列变动项（同一 key 内保持入参相对顺序）。
func sortItems(items []changeItem) []changeItem {
	out := make([]changeItem, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].key.variantID != out[j].key.variantID {
			return out[i].key.variantID < out[j].key.variantID
		}
		return out[i].key.warehouseID < out[j].key.warehouseID
	})
	return out
}

// distinctVariantIDs 去重变体 id（元数据批量解析用）。
func distinctVariantIDs(keys []stockKey) []string {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if seen[k.variantID] {
			continue
		}
		seen[k.variantID] = true
		out = append(out, k.variantID)
	}
	return out
}

// firstItemOf 取某行标识的首条变动项（建行时的元数据来源）。
func firstItemOf(items []changeItem, k stockKey) changeItem {
	for _, it := range items {
		if it.key == k {
			return it
		}
	}
	return changeItem{}
}

// normalizeDirection 归一变动方向（空 / 未知一律拒绝，不接受自由文本）。
func normalizeDirection(direction string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case inventoryenums.DirectionIn:
		return inventoryenums.DirectionIn, nil
	case inventoryenums.DirectionOut:
		return inventoryenums.DirectionOut, nil
	case inventoryenums.DirectionAdjust:
		return inventoryenums.DirectionAdjust, nil
	default:
		return "", errors.New(inventoryenums.ErrStockDirectionInvalid)
	}
}

// normalizeDirectionOrEmpty 归一可选方向（空串表示不过滤）。
func normalizeDirectionOrEmpty(direction string) (out string, err error) {
	if strings.TrimSpace(direction) == "" {
		return "", nil
	}
	return normalizeDirection(direction)
}

// movementPageArgs 归一流水分页参数。
func movementPageArgs(req *inventorydto.ListMovementReq) (page, size int) {
	page, size = 1, movementDefaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > movementMaxPageSize {
			size = movementMaxPageSize
		}
	}
	return page, size
}

// abs 整数绝对值。
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// orString 取非空字符串（工程 id 兜底）。
func orString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

// toMovementResp 投影行 → 流水响应。
func toMovementResp(r *inventorymodel.MovementRow) *inventorydto.MovementResp {
	resp := &inventorydto.MovementResp{
		ID: r.ID, ProjectID: r.ProjectID, WarehouseID: r.WarehouseID,
		WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
		ProductID: r.ProductID, VariantID: r.VariantID, SKUCode: r.SKUCode,
		Direction: r.Direction, Quantity: r.Quantity, Delta: r.Delta,
		QuantityBefore: r.QuantityBefore, QuantityAfter: r.QuantityAfter,
		ReasonCode: r.ReasonCode, ReasonName: r.ReasonName,
		SourceType: r.SourceType, SourceRef: r.SourceRef,
		Remark: r.Remark, OperatorID: r.OperatorID, BatchID: r.BatchID,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
	if r.ReasonID != nil {
		resp.ReasonID = strconv.FormatInt(*r.ReasonID, 10)
	}
	if r.ParentVariantID != nil {
		resp.ParentVariantID = *r.ParentVariantID
	}
	return resp
}

// toMovementRespFromEntity 实体 → 流水响应（变动响应里的即时回执）。
func toMovementRespFromEntity(m *inventorymodel.MovementEntity) *inventorydto.MovementResp {
	resp := &inventorydto.MovementResp{
		ID: m.ID, ProjectID: m.ProjectID, WarehouseID: m.WarehouseID,
		ProductID: m.ProductID, VariantID: m.VariantID, SKUCode: m.SKUCode,
		Direction: m.Direction, Quantity: m.Quantity, Delta: m.Delta,
		QuantityBefore: m.QuantityBefore, QuantityAfter: m.QuantityAfter,
		ReasonCode: m.ReasonCode, SourceType: m.SourceType, SourceRef: m.SourceRef,
		Remark: m.Remark, OperatorID: m.OperatorID, BatchID: m.BatchID,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
	if m.ReasonID != nil {
		resp.ReasonID = strconv.FormatInt(*m.ReasonID, 10)
	}
	if m.ParentVariantID != nil {
		resp.ParentVariantID = *m.ParentVariantID
	}
	return resp
}
