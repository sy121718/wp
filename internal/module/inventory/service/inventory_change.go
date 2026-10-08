package inventoryservice

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

// 物料清单是「父 SKU → 子项 SKU × 用量」的派生物：扣减父 SKU 时把它展开成
// 若干子项 SKU，各子项各自的真源行上加锁扣减（整体一个事务、整体成功或整体拒绝）。
//
// 三条规则：
//
//	· 全量替换而非追加：同一父 SKU 的清单一次给全，先删后写（聚合内原子组合）；
//	· 空清单 = 清空：之后扣减该 SKU 就按它自己扣；
//	· 成环在维护入口就拒绝（A→B→A 会让展开无限递归），展开时另设层数上限兜底。

// 「变动原因覆盖出 / 入 / 调整各枚举，含自定义原因（引用可维护字典，不用自由文本）」：
//
//	· 内置原因：迁移 103 seed，project_id IS NULL（全工程可见，**只读**：不可改名、
//	  不可删除，只能停用 / 启用 —— 名称由系统按 code 派生 i18n key，见迁移 241）；
//	· 自定义原因：工程内 code 唯一，可改名 / 停用（停用后不再能被新变动引用，
//	  历史流水不受影响 —— 流水里存的是 code 快照）。
//
// 变动入口只接受字典里存在的 code，且原因方向必须与本次变动方向一致。
//
// 文案不算本模块的数据（2026-09 收口）：inventory_change_reasons.name 存的是 **i18n key**，
// 真文案在 sys_i18n（内容 → 文案词条，全站唯一真源）。本模块只负责「有哪些原因、方向、启停」，
// 自定义原因保存时把运营填的文案写成 sys_i18n 的一条词条 —— 运营改文案、加语言都在同一个地方，
// 而不是回到库存模块里再维护一份平行的名称表。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/inventory/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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
	// cost 非 nil 表示本次变动要顺带写入 (仓库, SKU) 的**当前成本价**（覆盖式）。
	//
	// 出库 / 盘点 / 报损一律不传（它们不动成本）；采购收货与生产入库把单价传进来，
	// 于是成本与数量落在同一个事务里。nil = 本次不碰成本，不是「把成本清空」。
	cost *float64
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

// stockPolicy 变动路径的**行级策略**：不跟踪（无限）的行怎么处理（BIZ-03）。
//
// 为什么需要它：入库 / 调整路径原本无条件把不跟踪的行切成跟踪（「显式给了数量就说明
// 要开始跟踪」）。这条规则对人类意图成立，但**系统自动归还**也走同一条函数并显式传数量
// —— 取消订单归还 5 件会把一个「无限」商品变成「库存 5」，第 6 件直接判库存不足。
// 而出库对不跟踪的行是**跳过**的（数量不构成约束，连流水都不写）：归还与出库不对称
// 才是缺陷本体 —— 没扣过的数量不该被加回来。
type stockPolicy int

const (
	// stockPolicyExplicit 显式变动（管理员入库 / 盘点调整 / 采购收货 / 生产入库）：
	// 不跟踪的行在入库 / 调整时切成跟踪（迁移 261 拍板的口径，保持不变）。
	stockPolicyExplicit stockPolicy = iota
	// stockPolicyRestore 系统自动归还（取消订单归还 / 退货入库）：不跟踪的行**跳过**，
	// 与出库对称 —— 不切开关、不写数量、不写流水。
	stockPolicyRestore
)

// changeMeta 一次变动的公共元数据（来源引用 / 原因 / 备注 / 操作人）。
type changeMeta struct {
	reason     *inventorymodel.ReasonEntity
	sourceType string
	sourceRef  string
	remark     string
	operatorID string
}

// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：单行与多行走同一条加锁路径。
//
// 成本：(仓库, SKU) 的当前成本价只在行**显式传了 CostPrice** 时才写（迁移 244，
// 覆盖式）。出库 / 盘点 / 报损的调用方一律不传 —— 调成本不是这些动作的事；
// 采购收货与生产入库把单价传进来，让成本与数量落在同一个事务里。
//
// 本方法**自己开事务**（自足调用方）。要把库存变动并进调用方自己那个事务的用 ChangeStockTx。
func (s *Service) ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error) {
	return s.changeStockTracking(ctx, req, nil)
}

// ChangeStockTx 与 ChangeStock 逐字同一条路径，但在**调用方的事务**里执行（tx 非 nil）。
//
// 事务透传版：**不自己开事务、错误原样返回** —— 专供同库跨模块的调用方（订单建单扣减 /
// 取消归还库存 / 退货入库）把库存变动纳入自己那个事务：任一步失败整体回滚，不需要
// 「先提交再补偿」（补偿只留给跨库 / 外部系统，见 AGENTS.md「写操作的事务与回滚」）。
func (s *Service) ChangeStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error) {
	_, _, _, err = s.changeStockTrackingTx(ctx, tx, req, nil)
	return err
}

// RestockStock 系统自动归还（补偿性入库）：取消订单归还 / 退货入库走这条。
//
// 与 ChangeStock 的唯一差别是**行级策略**（见 stockPolicy）：不跟踪的行跳过，
// 不把它切成跟踪、也不写数量与流水。其余一切（加锁顺序、流水构造、成本写入、
// 幂等建行、事务语义）逐字相同 —— 复用同一条写入路径，不是另起一份实现。
//
// 为什么不复用 ChangeStock 加一个布尔参数：本包被 4 个方向调用（管理员 / 采购收货 /
// 生产入库 / 订单归还），加参数会让每个调用点都要表态，而其中 3 个的答案永远一样。
// 显式入口表达的是**语义**（归还 ≠ 入库），调用方选错时在代码审查里看得见。
func (s *Service) RestockStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, direction, err := s.changeStockTrackingPolicyTx(ctx, nil, req, nil, stockPolicyRestore)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, direction, out), nil
}

// RestockStockTx 与 RestockStock 逐字同一条路径，但在**调用方的事务**里执行（tx 非 nil）。
//
// 订单侧的取消 / 退货入库走的就是它（经 inventory/outbound/orderstock 适配）：
// 归还必须与订单状态推进同事务，失败整体回滚。
func (s *Service) RestockStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error) {
	_, _, _, err = s.changeStockTrackingPolicyTx(ctx, tx, req, nil, stockPolicyRestore)
	return err
}

// changeStockTracking 与 ChangeStock 是**同一条**路径，额外允许把若干行的
// **跟踪开关**在同一事务里一并写回（trackOverride：key = (变体, 仓库)，值 = 本次变动后
// 该行的目标开关）。
//
// 为什么需要它：库存页行内编辑「切成无限」= 数量清零 + 关开关，是两处持久化写入。
// 分成两次调用就会留下「数量已清零、开关还开着」或「开关关了、数量没清」的中间态
// （后者还会直接撞 CHECK (track_quantity OR quantity = 0) 报 23514）。所以关开关这件事
// 必须与数量写回、流水写入在同一个事务里完成 —— 也正因如此这里不再另开一条写路径：
// 它复用同一套加锁顺序与同一份流水构造，只是多带一个目标开关。
//
// trackOverride 为 nil 时行为与 ChangeStock 逐字一致。
func (s *Service) changeStockTracking(ctx context.Context, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, direction, err := s.changeStockTrackingTx(ctx, nil, req, trackOverride)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, direction, out), nil
}

// changeStockTrackingTx 变动契约的**事务实现**（显式策略）：tx 为 nil 时自己开一个事务，
// 非 nil 时全部写入落在调用方的事务里（提交 / 回滚由调用方负责）。
//
// 校验与入参归一（resolveProjectID / resolveReason / resolveWarehouse）是只读动作，
// 放在事务外与事务内没有区别 —— 写入只有 applyStockChangesTx 一处。
func (s *Service) changeStockTrackingTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool) (out *changeOutcome, projectID, direction string, err error) {
	return s.changeStockTrackingPolicyTx(ctx, tx, req, trackOverride, stockPolicyExplicit)
}

// changeStockTrackingPolicyTx 与 changeStockTrackingTx 同一条路径，多一个**行级策略**
// （stockPolicy）：决定不跟踪的行在本次入库 / 调整里是「切成跟踪」还是「跳过」。
//
// 只有策略不同，其余（校验 / 归一 / 加锁序 / 流水 / 成本）逐字共用 —— 归还与入库
// 不该有两条会各自漂移的写路径。
func (s *Service) changeStockTrackingPolicyTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, projectID, direction string, err error) {
	if req == nil {
		return nil, "", "", errors.New(inventoryenums.ErrInvalidParam)
	}
	if direction, err = normalizeDirection(req.Direction); err != nil {
		return nil, "", "", err
	}
	if len(req.Lines) == 0 {
		return nil, "", "", errors.New(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, "", "", errors.New(inventoryenums.ErrStockLinesTooMany)
	}
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, "", "", err
	}
	// 变动原因必须是字典里的条目，且方向与本次变动一致（不接受自由文本）。
	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, direction)
	if err != nil {
		return nil, "", "", err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}
	items, err := s.buildChangeItems(ctx, projectID, direction, req.WarehouseID, req.Lines, meta, "")
	if err != nil {
		return nil, "", "", err
	}
	if out, err = s.applyStockChangesOn(ctx, tx, projectID, items, trackOverride, policy); err != nil {
		return nil, "", "", err
	}
	return out, projectID, direction, nil
}

// DeductStock 按 SKU 扣减库存（验收 1/5）：不足即整体拒绝，可按物料清单展开多个子项 SKU。
//
// 本方法**自己开事务**。要把扣减并进调用方自己那个事务的用 DeductStockTx
// （订单建单就是这条路：订单 + 明细 + 流水 + 券核销 + 扣库存必须同事务）。
func (s *Service) DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, err := s.deductStockTx(ctx, nil, req)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, inventoryenums.DirectionOut, out), nil
}

// DeductStockTx 与 DeductStock 逐字同一条路径，但在**调用方的事务**里执行（tx 非 nil）。
//
// 事务透传版：**不自己开事务、错误原样返回**（库存不足也原样返回，由调用方决定怎么呈现）。
func (s *Service) DeductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (err error) {
	_, _, err = s.deductStockTx(ctx, tx, req)
	return err
}

// deductStockTx 扣减的事务实现（tx 为 nil 时自己开事务）。
func (s *Service) deductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (
	out *changeOutcome, projectID string, err error) {
	if req == nil {
		return nil, "", errors.New(inventoryenums.ErrInvalidParam)
	}
	if len(req.Lines) == 0 {
		return nil, "", errors.New(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, "", errors.New(inventoryenums.ErrStockLinesTooMany)
	}
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, "", err
	}
	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, inventoryenums.DirectionOut)
	if err != nil {
		return nil, "", err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}
	items, err := s.buildChangeItems(ctx, projectID, inventoryenums.DirectionOut, req.WarehouseID, req.Lines, meta, "")
	if err != nil {
		return nil, "", err
	}
	// 按物料清单展开：父 SKU → 子项 SKU × 用量 × 请求量（多级清单逐层展开）。
	// 展开要读 inventory_bom_items（迁移 215 名单），作用域用本请求已解析出的 projectID。
	if req.ExpandBOM {
		if items, err = s.expandBOM(ctx, projectID, items); err != nil {
			return nil, "", err
		}
	}
	if out, err = s.applyStockChangesOn(ctx, tx, projectID, items, nil, stockPolicyExplicit); err != nil {
		return nil, "", err
	}
	return out, projectID, nil
}

// ListMovements 库存流水列表（验收 3：方向 / 数量 / 原因 / 来源引用都可查可过滤）。
func (s *Service) ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error) {
	filter, err := s.movementFilter(ctx, req)
	if err != nil {
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

// CountMovements 流水总条数（分页页面的「共 N 条」与总页数）。
//
// **与 ListMovements 共用同一个 movementFilter**：过滤条件的归一（方向白名单、
// 时间解析、工程作用域解析）与非法参数拒绝只有一份实现，两处口径不存在分叉的余地 ——
// 「总数 0 而列表有数据」这类矛盾只在筛选维度上用错时才暴露，查起来极费时间。
func (s *Service) CountMovements(ctx context.Context, req *inventorydto.ListMovementReq) (n int64, err error) {
	filter, err := s.movementFilter(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.CountMovements(ctx, filter)
}

// movementFilter 把流水列表请求归一成 model 过滤条件（ListMovements / CountMovements 共用）。
//
// 归一的三件事：方向必须落在白名单（非法值直接拒绝，不退化成「不过滤」）、时间区间
// 按本地时区解析且上界补齐当日末刻、工程作用域必填（缺作用域在非超级角色下是静默空集）。
func (s *Service) movementFilter(ctx context.Context, req *inventorydto.ListMovementReq) (filter inventorymodel.MovementFilter, err error) {
	if req == nil {
		return filter, errors.New(inventoryenums.ErrInvalidParam)
	}
	filter = inventorymodel.MovementFilter{
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
		return filter, err
	}
	// 时间区间：页面表单给的是**本地时间**（日期或日期时间），这里按本地时区解析；
	// 只给日期时上界按当日 23:59:59 收口 —— 「截止今天」不该把今天整天漏掉。
	if filter.TimeFrom, err = parseMovementTime(req.TimeFrom, false); err != nil {
		return filter, err
	}
	if filter.TimeTo, err = parseMovementTime(req.TimeTo, true); err != nil {
		return filter, err
	}
	if filter.TimeFrom != nil && filter.TimeTo != nil && filter.TimeTo.Before(*filter.TimeFrom) {
		return filter, errors.New(inventoryenums.ErrMovementTimeRangeInvalid)
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return filter, err
	}
	return filter, nil
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
		// 显式成本：空 = 本次不碰成本（出库 / 盘点 / 报损的默认行为），
		// 给了值就校验 —— 成本可以「尚未核算」（NULL），但不能是负数 / NaN / Inf。
		cost, cerr := normalizeExplicitCost(line.CostPrice)
		if cerr != nil {
			return nil, cerr
		}
		items = append(items, changeItem{
			key:             stockKey{variantID: variantID, warehouseID: wh.ID},
			direction:       direction,
			quantity:        quantity,
			cost:            cost,
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

// applyStockChangesOn 完成全部行的加锁与增减（不留半截状态）：tx 为 nil 时自己开一个
// 事务，非 nil 时**落在调用方的事务里**（不提交、不回滚 —— 那是调用方的事）。
//
// 两种形态共用同一份实现（applyStockChangesTx），因此「自足调用」与「事务透传调用」
// 的加锁顺序、流水构造、成本写入逐字一致 —— 不存在两条会各自漂移的写路径。
func (s *Service) applyStockChangesOn(ctx context.Context, tx *gorm.DB, projectID string, items []changeItem,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, err error) {
	if tx != nil {
		return s.applyStockChangesTx(ctx, tx, projectID, items, trackOverride, policy)
	}
	err = s.m.Transaction(ctx, func(t *gorm.DB) error {
		var e error
		out, e = s.applyStockChangesTx(ctx, t, projectID, items, trackOverride, policy)
		return e
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// applyStockChangesTx 变动写入的事务内实现（tx 由调用方负责提交 / 回滚）。
//
// 事务内的三段式是本票并发安全的全部依据：
//
//	① 幂等建行   —— ON CONFLICT DO NOTHING，按标识升序；并发批次不会插出重复行；
//	② 按序加锁   —— 全部行按同一升序逐一 FOR UPDATE，锁序全局一致（无死锁）；
//	③ 按序应用   —— 在锁内算增减与前后值，写回数量并逐条写流水。
//
// ①②必须分成两趟而不能边建边锁：边建边锁会让两个批次在「各自已持有的行」上互等，
// 那才会真正成环（见 inventory_change_test.go 的对向扣减并发用例）。
func (s *Service) applyStockChangesTx(ctx context.Context, tx *gorm.DB, projectID string, items []changeItem,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, err error) {
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
	// 显式成本按 (变体, 仓库) 汇总：同一 key 出现多条时**最后一条显式成本**生效
	//（按锁序处理，结果确定；调用方不该给同一个 key 两个成本）。
	costs := explicitCosts(items)

	err = func() error {
		// 变动前的元数据解析（**不加锁**）：目标行已存在时沿用它的商品 / SKU 快照。
		// projectID 必传：inventory_stocks 带 FORCE 策略，这条事务内的首读没有作用域时
		// 恒 0 行 —— 展开出来的子项于是解析不到商品快照（见该方法的注释）。
		existing, lerr := s.m.ListStocksByVariantsTx(ctx, tx, variantIDs, projectID)
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
				SKUCode: skuCode,
				// 变动路径上首次出现的行同样默认**不跟踪（无限）**：与商品侧建变体、
				// 与 EnsureStock 的新建口径一致（迁移 261：新建行默认无限，存量行才 true）。
				// 入库 / 调整会在 ③ 里把这一行切成跟踪并把数量写回。
				TrackQuantity: false, Quantity: 0,
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
		//
		// 两条与「无限库存」相关的语义（迁移 261，2026-09-19 拍板）：
		//
		//	· 不跟踪（track_quantity = false = 无限）的行**出库不校验可用量、不扣减**
		//	  ——「无限」的定义就是数量不构成约束，订单照卖，因此这里直接跳过，
		//	  连流水都不写（没有数量变动就没有变动，与同一行 adjust 到当前值同款）；
		//	· 入库 / 调整**显式给了数量**就把行切成跟踪：给具体数量这件事本身
		//	  就意味着要跟踪，否则 CHECK (track_quantity OR quantity = 0) 会在写非 0
		//	  数量时拒绝。切换与数量写回在同一事务、同一批已加锁的行上完成。
		changed := make(map[stockKey]bool, len(keys))
		for _, it := range sortItems(items) {
			e := locked[it.key]
			if !e.TrackQuantity && it.direction == inventoryenums.DirectionOut {
				continue
			}
			// 系统自动归还（取消订单归还 / 退货入库）对不跟踪的行**跳过**，与出库对称：
			// 出库时它没被扣过（连流水都没有），归还时自然也不该加回来 ——
			// 加了就会把这个「无限」商品悄悄变成「库存 N」（BIZ-03）。
			if !e.TrackQuantity && policy == stockPolicyRestore {
				continue
			}
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
				// 只监跟踪行：不跟踪的行在上面已经跳过（它们没有「不足」这一说）。
				return errors.New(inventoryenums.ErrStockInsufficient)
			}
			switched := false
			if !e.TrackQuantity {
				e.TrackQuantity = true
				switched = true
			}
			if delta == 0 {
				// 没有数量变动，不写流水；但「切成跟踪」仍要写回 —— 给 0 也是一个
				// 显式数量（「卖光了」），与「没填 = 无限」是两回事。
				if switched {
					changed[it.key] = true
				}
				continue
			}
			e.Quantity = after
			changed[it.key] = true
			out.variants[it.key.variantID] = e.SKUCode
			out.movements = append(out.movements, movementOf(it, e, before, after, delta, out.batchID, now))
		}
		for _, k := range keys {
			e := locked[k]
			// 调用方要求的**目标跟踪开关**（nil 映射 = 不改）：写在同一事务、同一次 UPDATE 里。
			// 「切成无限」就是这条路径（数量清零 + 关开关原子完成）。
			if target, ok := trackOverride[k]; ok && target != e.TrackQuantity {
				e.TrackQuantity = target
				changed[k] = true
			}
			if !changed[k] {
				continue
			}
			// CHECK (track_quantity OR quantity = 0) 的口径在这里提前兜住：不跟踪的行
			// 不允许带数字。直接撞约束只会拿到一个没有上下文的 23514。
			if !e.TrackQuantity && e.Quantity != 0 {
				return errors.New(inventoryenums.ErrStockUntrackedQuantity)
			}
			if err := s.m.UpdateStockQuantityAndTrackingTx(ctx, tx, e.ID,
				e.Quantity, e.TrackQuantity, now); err != nil {
				return err
			}
		}

		// ④ 成本写入（同一事务、同一批已加锁的行）：只有**显式传了成本**的行才写，
		//    覆盖式 —— 数量有没有变化都照写（盘点到同一数量但成本要改，是合法诉求）。
		//    放在数量写回之后不影响锁序：目标行在 ② 里已全部锁住，这里只做 UPDATE。
		for _, k := range keys {
			cost, ok := costs[k]
			if !ok {
				continue
			}
			if err := s.m.UpdateStockCostTx(ctx, tx, locked[k].ID, cost, now); err != nil {
				return err
			}
		}
		return s.m.CreateMovementsTx(ctx, tx, out.movements)
	}()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// finishChange 组装响应。
//
// 历史上这里还要在事务提交之后做一次商品侧缓存同步（issue #16）。迁移 121 删掉那层缓存
// 之后已无同步动作：CacheTotals / CacheSynced / CacheFailures 三个字段仍在响应结构里，
// 值恒为「空 map / true / 空切片」—— 别把它们读成「同步确实成功了」，它们现在只表示这条
// 老路径没有任何失败可报。
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
		ID: utils.NewTimeOrderedID(), ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
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
	// 成本留痕（迁移 256）：流水记下**这次变动时刻**的成本，之后改库存行成本不再
	// 改写历史 —— 「实际发出那批货的成本」由此固定下来。逐方向的取法：
	//
	//   本次带了显式成本（采购单价 / 生产入库单价；盘点改成本也走这条）→ 记它：
	//     「这批货进来按多少算 / 本次盘点把成本定成多少」就是这次变动的事实；
	//   没带显式成本（出库恒如此，入库 / 调整也可以）→ 复制该库存行**当时的**当前成本：
	//     出库由此固定「实际发出那批货的成本」；不影响成本的变动则留下这条货当时的口径。
	//
	// e 是**本次变动写回之前**的库存行（成本写回在 applyStockChanges 的 ④、晚于本函数）：
	// 传了显式成本时 e.CostPrice 还是旧值，所以那条路径必须用 it.cost。
	// it.cost == nil 与成本列 NULL 是两回事：前者「本次没带成本」，后者「尚未核算」——
	// 未核算一律留 NULL，绝不用 0 冒充（0 是合法的显式成本）。
	switch {
	case it.cost != nil:
		value := *it.cost
		m.UnitCost = &value
	case e.CostPrice != nil:
		value := *e.CostPrice
		m.UnitCost = &value
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

// normalizeExplicitCost 归一**显式传入**的成本价（nil = 本次变动不碰成本）。
//
// 空是合法状态（成本列 NULL = 尚未核算；本模块不用 0 冒充未知），但只要给了值，
// 就必须是 >= 0 的有限数：负数、NaN、Inf 都会让「当前成本」变成一个不可解释的量。
func normalizeExplicitCost(raw *float64) (out *float64, err error) {
	if raw == nil {
		return nil, nil
	}
	value := *raw
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, errors.New(inventoryenums.ErrStockCostInvalid)
	}
	return &value, nil
}

// explicitCosts 汇总本次变动里显式给出的成本（按 (变体, 仓库) 去重，后者覆盖前者）。
func explicitCosts(items []changeItem) map[stockKey]float64 {
	out := make(map[stockKey]float64)
	for _, it := range items {
		if it.cost != nil {
			out[it.key] = *it.cost
		}
	}
	return out
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

// parseMovementTime 解析流水时间筛选项。
//
// 接受纯日期（2006-01-02）与到秒 / 到分的时间（2006-01-02 15:04[:05]）。
// endOfDay 为真且只给日期时，上界补到当日 23:59:59 —— 否则「截止某天」的查询
// 会停在当天 00:00:00，把那一整天的流水漏掉（这类漏在半截时间上的查询最难发现：
// 结果看起来「有数据、只是少一些」）。
func parseMovementTime(raw string, endOfDay bool) (out *time.Time, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{utils.LayoutDay, utils.LayoutSecond, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		t, terr := time.ParseInLocation(layout, value, time.Local)
		if terr != nil {
			continue
		}
		if endOfDay && layout == utils.LayoutDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return &t, nil
	}
	return nil, errors.New(inventoryenums.ErrMovementTimeRangeInvalid)
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
		// 成本留痕同源（投影带上 mv.unit_cost）：列表与变动回执给出同一个值。
		UnitCost:  r.UnitCost,
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
		UnitCost:  m.UnitCost,
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

// SetBOM 全量替换某个父 SKU 的物料清单（验收 5）。
func (s *Service) SetBOM(ctx context.Context, req *inventorydto.SetBOMReq) (res *inventorydto.BOMResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	parentVariantID := strings.TrimSpace(req.ParentVariantID)
	if parentVariantID == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	seen := make(map[string]bool, len(req.Items))
	rows := make([]*inventorymodel.BOMItemEntity, 0, len(req.Items))
	for _, item := range req.Items {
		componentID := strings.TrimSpace(item.ComponentVariantID)
		if componentID == "" {
			return nil, errors.New(inventoryenums.ErrBOMComponentRequired)
		}
		if componentID == parentVariantID {
			return nil, errors.New(inventoryenums.ErrBOMSelfReference)
		}
		if item.Quantity <= 0 {
			return nil, errors.New(inventoryenums.ErrBOMQuantityInvalid)
		}
		if seen[componentID] {
			return nil, errors.New(inventoryenums.ErrBOMDuplicateComponent)
		}
		seen[componentID] = true
		rows = append(rows, &inventorymodel.BOMItemEntity{
			ID: uuid.NewString(), ProjectID: projectID,
			ParentVariantID: parentVariantID, ParentSKUCode: strings.TrimSpace(req.ParentSKUCode),
			ComponentVariantID: componentID, ComponentSKUCode: strings.TrimSpace(item.ComponentSKUCode),
			Quantity: item.Quantity, CreatedAt: now, UpdatedAt: now,
		})
	}
	// 成环检测与全量替换都在工程作用域内（inventory_bom_items 在迁移 215 名单里）：
	// 检测读不到父链会**放行成环**，替换缺作用域则静默 0 行。两处都用调用方已解析出的
	// projectID，不依赖「唯一工程兜底」——那在多工程下会退化成 ErrInvalidParam。
	if err = s.assertBOMNoCycle(ctx, parentVariantID, projectID, rows); err != nil {
		return nil, err
	}
	if err = s.m.ReplaceBOM(ctx, parentVariantID, projectID, rows); err != nil {
		return nil, err
	}
	return s.GetBOM(ctx, &inventorydto.GetBOMReq{ProjectID: projectID, ParentVariantID: parentVariantID})
}

// GetBOM 某个父 SKU 的物料清单（无清单返回空 items，不是错误）。
func (s *Service) GetBOM(ctx context.Context, req *inventorydto.GetBOMReq) (res *inventorydto.BOMResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	parentVariantID := strings.TrimSpace(req.ParentVariantID)
	if parentVariantID == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListBOMItems(ctx, parentVariantID, projectID)
	if err != nil {
		return nil, err
	}
	res = &inventorydto.BOMResp{
		ParentVariantID: parentVariantID,
		Items:           make([]*inventorydto.BOMItemResp, 0, len(rows)),
	}
	for _, r := range rows {
		res.ProjectID = r.ProjectID
		res.ParentSKUCode = r.ParentSKUCode
		res.UpdatedAt = r.UpdatedAt.Format(time.RFC3339)
		res.Items = append(res.Items, &inventorydto.BOMItemResp{
			ID: r.ID, ComponentVariantID: r.ComponentVariantID,
			ComponentSKUCode: r.ComponentSKUCode, Quantity: r.Quantity,
		})
	}
	return res, nil
}

// assertBOMNoCycle 成环检测（作用域由调用方给）：新增 P → C 会成环，当且仅当「C 已经能走到 P」
// （即 P 是 C 的祖先）。判定办法是从 P 沿「谁把 X 当子项」一路上行，
// 若途中撞到任何一个新子项 C，就说明存在 C → … → P 的既有路径，加上 P → C 即成环。
//
// 方向很容易写反：从子项向上找到的是「C 的祖先」，那只能说明 P → … → C 早已存在，
// 再加 P → C 只是多了一条通路，并不成环 —— 这样的清单会被放行，展开时直接死循环。
//
// projectID 必须传：inventory_bom_items 带 FORCE 策略，缺作用域时 ListBOMParents 读到空
// 父链 —— 判定于是「一路都没撞到新子项」，**成环被静默放行**。这不是查不到数据，
// 而是把该拒绝的写入放行（清单已经落库，靠下游 maxBOMDepth 才表现出来）。
func (s *Service) assertBOMNoCycle(ctx context.Context, parentVariantID, projectID string,
	rows []*inventorymodel.BOMItemEntity) (err error) {
	components := make(map[string]bool, len(rows))
	for _, r := range rows {
		components[r.ComponentVariantID] = true
	}
	visited := make(map[string]bool, len(rows)+1)
	stack := []string{parentVariantID}
	for len(stack) > 0 {
		current := stack
		stack = nil
		for _, id := range current {
			if visited[id] {
				continue
			}
			visited[id] = true
			parents, perr := s.m.ListBOMParents(ctx, id, projectID)
			if perr != nil {
				return perr
			}
			for _, p := range parents {
				if components[p.ParentVariantID] {
					return errors.New(inventoryenums.ErrBOMCycle)
				}
				stack = append(stack, p.ParentVariantID)
			}
		}
	}
	return nil
}

// expandBOM 按物料清单把「父 SKU 变动项」展开成「叶子 SKU 变动项」（验收 5）。
//
// 逐层展开（BFS）：有清单的 SKU 换成子项（用量 = 子项用量 × 请求量），
// 没有清单的 SKU 就是叶子，按自身扣减。子项沿用**父项解析出的仓库**，
// 这样一次扣减的仓库口径唯一；祖先链随节点携带，出现环即拒绝。
//
// projectID 是 inventory_bom_items 的作用域（迁移 215）：缺它时每层都读到空子项，
// 有清单的 SKU 全被当成叶子 —— 扣减静默少扣子项料，且不报错。
func (s *Service) expandBOM(ctx context.Context, projectID string, items []changeItem) (out []changeItem, err error) {
	type node struct {
		item  changeItem
		chain map[string]bool
	}
	frontier := make([]node, 0, len(items))
	for _, it := range items {
		frontier = append(frontier, node{item: it, chain: map[string]bool{it.key.variantID: true}})
	}
	out = make([]changeItem, 0, len(items))
	for depth := 0; depth < maxBOMDepth; depth++ {
		if len(frontier) == 0 {
			return out, nil
		}
		parents := make([]string, 0, len(frontier))
		for _, n := range frontier {
			parents = append(parents, n.item.key.variantID)
		}
		rows, lerr := s.m.ListBOMItemsByParents(ctx, parents, projectID)
		if lerr != nil {
			return nil, lerr
		}
		children := make(map[string][]*inventorymodel.BOMItemEntity, len(rows))
		for _, r := range rows {
			children[r.ParentVariantID] = append(children[r.ParentVariantID], r)
		}
		next := make([]node, 0, len(frontier))
		for _, n := range frontier {
			kids := children[n.item.key.variantID]
			if len(kids) == 0 {
				out = append(out, n.item)
				continue
			}
			for _, kid := range kids {
				if n.chain[kid.ComponentVariantID] {
					return nil, errors.New(inventoryenums.ErrBOMCycle)
				}
				chain := make(map[string]bool, len(n.chain)+1)
				for id := range n.chain {
					chain[id] = true
				}
				chain[kid.ComponentVariantID] = true
				child := n.item
				child.key = stockKey{variantID: kid.ComponentVariantID, warehouseID: n.item.key.warehouseID}
				child.quantity = n.item.quantity * kid.Quantity
				child.skuCode = kid.ComponentSKUCode
				// 子项的商品 id 由真源快照解析（建行时才会用到），不在这里猜。
				child.productID = ""
				child.parentVariantID = n.item.key.variantID
				next = append(next, node{item: child, chain: chain})
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		// 列表里不可能有环（维护入口已拒绝），这是数据异常的第二道防线。
		return nil, errors.New(inventoryenums.ErrBOMDepthExceeded)
	}
	return out, nil
}

// maxReasonCodeLen 原因 code 长度上限。
const maxReasonCodeLen = 32

// reasonI18nPrefix 原因词条的 key 前缀（内置：inventory.reason.<code>）。
const reasonI18nPrefix = "inventory.reason."

// builtinReasonKey 内置原因的 i18n key（形如 inventory.reason.purchase_in）。
//
// 内置原因全工程共用一条词条，因此 key 里不带工程 —— 文案改了所有工程一起变，
// 这正是内置的含义（它是产品预置的取值范围，不是某个工程的自定义项）。
func builtinReasonKey(code string) string { return reasonI18nPrefix + strings.ToLower(code) }

// customReasonKey 自定义原因的 i18n key。
//
// key 里带工程 id：sys_i18n 是全局表，两个工程各建一个同 code 的自定义原因时，
// 不带工程 id 的 key 会互相覆盖（后建的工程把前一个的文案悄悄改掉）。
func customReasonKey(projectID, code string) string {
	return reasonI18nPrefix + "custom." + strings.ToLower(strings.TrimSpace(projectID)) + "." + strings.ToLower(code)
}

// reasonTextCategory sys_i18n 里的分类（后台词条页按它筛选）。
const reasonTextCategory = "inventory"

// saveReasonText 把原因文案写进 sys_i18n（该 key 的 zh-CN 一行），并重载词条缓存。
//
// 失败**不阻断**原因本身的写入，只记一条日志：原因是业务数据（流水要引用它的 code），
// 文案是展示层；把「i18n 组件不可用」（纯库存单测 / 未初始化）升级成「原因建不了」
// 是拿一个更贵的故障换一个更便宜的。en-US 不写 —— 按项目约定，新词条不伪造译文，
// 英文界面取不到时按默认语言（zh-CN）回退。
func saveReasonText(ctx context.Context, key, text string) (err error) {
	return i18n.SaveEntry(ctx, i18n.Entry{
		Key:      key,
		Lang:     "zh-CN",
		Value:    text,
		Category: reasonTextCategory,
		Remark:   "internal/module/inventory/service/inventory_change.go",
	})
}

// writeReasonText 写词条并吞掉失败（记日志），返回是否写入成功。
func writeReasonText(ctx context.Context, key, text string) (ok bool) {
	if err := saveReasonText(ctx, key, text); err != nil {
		logger.Scene("inventory").With("key", key).With("err", err).Warn("变动原因文案写入 sys_i18n 失败")
		return false
	}
	return true
}

// ListReasons 变动原因列表（工程自定义 + 全部内置）。
func (s *Service) ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	filter := inventorymodel.ReasonFilter{
		Keyword:         strings.TrimSpace(req.Keyword),
		IncludeDisabled: req.IncludeDisabled,
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return nil, err
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListReasons(ctx, filter)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.ReasonResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toReasonResp(r))
	}
	return list, nil
}

// CreateReason 新建自定义变动原因（code 工程内唯一，方向一经确定不可改）。
func (s *Service) CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrReasonNameRequired)
	}
	code, err := normalizeReasonCode(req.Code)
	if err != nil {
		return nil, err
	}
	direction, err := normalizeDirection(req.Direction)
	if err != nil {
		return nil, errors.New(inventoryenums.ErrReasonDirectionInvalid)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 内置原因占用同一命名空间：自定义 code 不能与内置撞名（否则解析结果不确定）。
	if taken, cerr := s.m.ReasonCodeExists(ctx, projectID, code, 0); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrReasonCodeTaken)
	}
	// 原因行只存 i18n key，运营填的文案写成 sys_i18n 的一条词条。
	// 顺序「先词条、后原因」：反过来会出现「原因已能选、页面却显示裸 key」的窗口。
	key := customReasonKey(projectID, code)
	writeReasonText(ctx, key, name)
	now := time.Now().UTC()
	pid := projectID
	e := &inventorymodel.ReasonEntity{
		ProjectID: &pid, Code: code, Name: key, Direction: direction,
		IsBuiltin: false, Status: inventoryenums.StatusActive, Sort: req.Sort,
		CreateTime: now, UpdatedAt: now,
	}
	if err = s.m.CreateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// UpdateReason 修改自定义变动原因（内置原因一律拒绝）。
func (s *Service) UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if perr != nil {
		return nil, errors.New(inventoryenums.ErrReasonNotFound)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetReason(ctx, rid, projectID)
	if err != nil {
		return nil, mapReasonNotFound(err)
	}
	// 内置原因只读：它的名称就是系统按 code 派生的 key（inventory.reason.<code>），
	// 允许改文案等于让「内置」名不副实 —— 停用 / 排序则允许（那是使用范围，不是身份）。
	if e.IsBuiltin && req.Name != nil {
		return nil, errors.New(inventoryenums.ErrReasonBuiltin)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrReasonNameRequired)
		}
		// key 不变（它派生自 code 与工程），改的是词条的值。
		writeReasonText(ctx, e.Name, name)
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
			return nil, errors.New(inventoryenums.ErrReasonStatusInvalid)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// resolveReason 解析变动原因：必须是字典里的 active 条目，且方向与本次变动一致。
func (s *Service) resolveReason(ctx context.Context, projectID, code, direction string) (r *inventorymodel.ReasonEntity, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New(inventoryenums.ErrStockReasonRequired)
	}
	e, ferr := s.m.FindReasonByCode(ctx, projectID, code)
	if ferr != nil {
		return nil, mapReasonNotFound(ferr)
	}
	if e.Direction != direction {
		return nil, errors.New(inventoryenums.ErrReasonDirectionMismatch)
	}
	return e, nil
}

// normalizeReasonCode 归一并校验原因 code：小写字母 / 数字 / 下划线，1..maxReasonCodeLen。
func normalizeReasonCode(code string) (out string, err error) {
	out = strings.ToLower(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrReasonCodeRequired)
	}
	if len(out) > maxReasonCodeLen {
		return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
	}
	for _, r := range out {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
		}
	}
	return out, nil
}

// mapReasonNotFound 行不存在 → 业务错误，其余原样透出。
func mapReasonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrReasonNotFound)
	}
	return err
}

// toReasonResp 实体 → 响应。
func toReasonResp(e *inventorymodel.ReasonEntity) *inventorydto.ReasonResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.ReasonResp{
		ID: strconv.FormatInt(e.ID, 10), Code: e.Code, Name: e.Name, Direction: e.Direction,
		IsBuiltin: e.IsBuiltin, Status: e.Status, Sort: e.Sort,
		CreatedAt: e.CreateTime.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ProjectID != nil {
		resp.ProjectID = *e.ProjectID
	}
	return resp
}
