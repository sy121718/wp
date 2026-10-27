package inventoryservice

// inventory_transfer.go — 调拨用例（迁移 630 + 651）：申请 / 审批 / 出货 / 签收 / 取消 / 列表 / 详情。
//
// 状态机（单向；requested / rejected 由迁移 651 补入，对齐 ERP 的五态）：
//
//	requested ──Approve──→ pending ──Ship(需出货凭证)──→ in_transit ──Receive(需签收凭证)──→ completed
//	    │                     │
//	    ├──Reject──→ rejected └──Cancel──→ cancelled
//	    └──Cancel────────────→ cancelled
//
// 建单**一律**进 requested：审批的意义就是「出货前有人同意」，留一条绕过审批的路径
// 等于把「这批货该不该动」的判定分散到两处，而出货那一刻才是不可逆的时点。
// 单仓自营的小团队嫌麻烦？那由权限点解决（给他 approve 权限即可），不在状态机上开口子。
//
// 五条硬规则，全部来自用户明确要求或本仓既有铁律：
//
//  1. **出货与签收都必须带凭证附件**（缺少即拒绝，且整个事务回滚 —— 不留一张「已出货
//     但没凭证」的单：那种单据事后无法证明货是谁交出去的）；
//  2. 出货与签收都在**同一个事务**里完成「状态推进 + 库存变动 + 凭证落库」：
//     拆开的话会出现「状态已 in_transit、货没出库」这类事后只能人工对账的半截状态；
//  3. 调拨用**可售**（quantity − reserved）而不是在库：预占是留给订单的，
//     调拨吃掉它会让一张已付款的订单发不出货；
//  4. 明细指向**源仓库存行**（stock_id）：无归属的货（variant_id IS NULL）同样可以调拨，
//     签收时按同样的 variant_id / sku_code 在目标仓找行或建行 —— 无归属进、无归属出；
//  5. **审批只发生在 requested 态**，且审批时再看一次源仓可售（申请到同意之间货可能被
//     订单预占掉）：申请时判一次是为了「别让人申请本来就调不走的货」，审批时判一次是因为
//     那一刻才是要动的时刻 —— 只做前者会让审批通过的货在出货时才失败。
//
// 凭证校验走 contract 的 AttachmentResolver（不 import media）：逐个查存在性，
// 缺一张就整体拒绝 —— 「有 5 张凭证但其中 1 张被删了」不该被静默接受。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	json "go_wp/pkg/jsonx"
)

// 调拨单状态（迁移 630；requested / rejected 由迁移 651 补入）。
const (
	TransferStatusRequested = "requested"
	TransferStatusPending   = "pending"
	TransferStatusInTransit = "in_transit"
	TransferStatusCompleted = "completed"
	TransferStatusRejected  = "rejected"
	TransferStatusCancelled = "cancelled"
)

// maxTransferRejectReasonLen 驳回原因的长度上限（纯文本；对齐同类自由文本字段的口径）。
const maxTransferRejectReasonLen = 500

// 建单候选下拉的行数口径：默认 200 足够（一个仓的活跃 SKU 很少过百），
// 上限 500 —— 再多就不该靠下拉找，该先按关键词搜。
const (
	defaultTransferStockOptionLimit = 200
	maxTransferStockOptionLimit     = 500
)

// 凭证用途（迁移 630 的 usage_type）。
const (
	TransferAttachmentRequest = "request"
	TransferAttachmentShip    = "ship"
	TransferAttachmentReceive = "receive"
)

// CreateTransfer 建调拨申请（两仓必须存在、active、不同；明细按 stock_id 校验属于源仓）。
//
// 建单落 **requested**（迁移 651）：真正的库存变动在 Ship，而审批是「出货前有人同意」
// 的那一步。申请时判一次可售，是为了让人当场就知道「这货调不走」，而不是等审批完才发现。
//
// 幂等：request_id 非空且命中既有单据时直接返回那张（重复提交不该产生两张单）。
func (s *Service) CreateTransfer(ctx context.Context, req *inventorydto.CreateTransferReq) (res *inventorydto.TransferResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	requestID := strings.TrimSpace(req.RequestID)
	if existing, ferr := s.m.FindTransferByRequestID(ctx, projectID, requestID); ferr != nil {
		return nil, ferr
	} else if existing != nil {
		return s.transferRespTx(ctx, nil, projectID, existing.ID)
	}
	fromWh, err := s.resolveWarehouse(ctx, projectID, req.FromWarehouseID)
	if err != nil {
		return nil, err
	}
	toWh, err := s.resolveWarehouse(ctx, projectID, req.ToWarehouseID)
	if err != nil {
		return nil, err
	}
	if fromWh.ID == toWh.ID {
		// DDL 的 CHECK 也拦得住，但那是一句没有上下文的约束错误 —— 先给可读的。
		return nil, inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)
	}
	if len(req.Lines) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrTransferLinesRequired)
	}
	// 申请凭证（可选）：给了就要真的存在。
	requestAtts, err := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	transferID := uuid.NewString()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		items, ierr := s.transferItemsTx(ctx, tx, projectID, fromWh, transferID, req.Lines)
		if ierr != nil {
			return ierr
		}
		no, nerr := s.nextTransferNoTx(ctx, tx, projectID, now)
		if nerr != nil {
			return nerr
		}
		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(requestAtts))
		for i, att := range requestAtts {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				AttachmentID: att, UsageType: TransferAttachmentRequest, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferTx(ctx, tx, &inventorymodel.TransferEntity{
			ID: transferID, ProjectID: projectID, TransferNo: no,
			FromWarehouseID: fromWh.ID, ToWarehouseID: toWh.ID,
			Status: TransferStatusRequested, Remark: strings.TrimSpace(req.Remark),
			CreateBy: req.OperatorID, RequestID: requestID,
			// 建单即申请：申请人就是建单人，申请时刻就是建单时刻。
			RequestedBy: req.OperatorID, RequestedAt: &now,
			CreateTime: now, UpdateTime: now,
		}, items, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, transferID)
}

// transferItemsTx 校验并构造调拨明细（每条 stock_id 必须属于源仓且不超可售）。
//
// 校验在锁**之后**做（调用方已持 tx）：可售判定必须在行锁内 —— 否则并发调拨
// 会双双读到「可售 10」然后各搬 8 件。
func (s *Service) transferItemsTx(ctx context.Context, tx *gorm.DB, projectID string,
	fromWh *inventorymodel.WarehouseEntity, transferID string,
	lines []inventorydto.TransferLineReq) (items []*inventorymodel.TransferItemEntity, err error) {
	if len(lines) > maxBatchLines {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	// 先取源仓全部库存行（一次查询），再按 stock_id 逐条校验归属与可售。
	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, projectID, fromWh.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	items = make([]*inventorymodel.TransferItemEntity, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	// 同一 stock_id 出现多次时累加：加锁与守卫都按行做，分两次判会让「先过、后撞」
	// 留下一条中途失败的痕迹（虽然事务会回滚，但对账日志里不好看）。
	wanted := make(map[string]int, len(lines))
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		sid := strings.TrimSpace(line.StockID)
		if sid == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
		}
		if line.Quantity <= 0 {
			return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
		}
		if _, ok := wanted[sid]; !ok {
			order = append(order, sid)
		}
		wanted[sid] += line.Quantity
		seen[sid] = true
	}
	for _, sid := range order {
		row, ok := byID[sid]
		if !ok {
			// 这条库存行不在源仓（或不属于本工程）—— 不能说「不存在」（那是信息泄漏），
			// 只说「不属于源仓」，运营据此检查自己选的行。
			return nil, inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)
		}
		want := wanted[sid]
		if availableOf(row.Quantity, row.ReservedQuantity) < want {
			return nil, inventoryenums.Err(inventoryenums.ErrStockNotEnough)
		}
		items = append(items, &inventorymodel.TransferItemEntity{
			TransferID: transferID, StockID: row.ID,
			VariantID: row.VariantID, SKUCode: row.SKUCode, Quantity: want,
		})
	}
	_ = seen
	return items, nil
}

// ShipTransfer 出货（**必须带出货凭证**）：状态 pending → in_transit + 逐行出库。
func (s *Service) ShipTransfer(ctx context.Context, req *inventorydto.TransferShipReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	// 凭证必填：**先判空再进事务**（不必为一次注定失败的写开事务）。
	if len(req.AttachmentIDs) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)
	}
	ids, aerr := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if aerr != nil {
		return nil, aerr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusPending {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}
		// 出库（reason=transfer_out / source_type=transfer / source_ref=单号）：
		// 与采购、订单走同一条变动契约（有变动必有流水，锁序一致）。
		lines := make([]inventorydto.StockChangeLineReq, 0, len(items))
		for _, it := range items {
			var variantID string
			if it.VariantID != nil {
				variantID = *it.VariantID
			}
			lines = append(lines, inventorydto.StockChangeLineReq{
				WarehouseID: e.FromWarehouseID,
				VariantID:   variantID,
				SKUCode:     it.SKUCode,
				Quantity:    it.Quantity,
			})
		}
		// 无归属行的 variantID 为空 —— 变动契约要求变体上下文（否则建不出有身份的流水），
		// 所以这里按行 id 直接走「按库存行出库」的窄路径（见 shipUnclaimedTx）。
		if lerr := s.transferOutTx(ctx, tx, e, items, lines); lerr != nil {
			return lerr
		}
		if e, uerr := s.m.GetTransferTx(ctx, tx, projectID, e.ID); uerr == nil {
			_ = e
		}
		e.Status = TransferStatusInTransit
		e.ShippedBy = req.OperatorID
		e.ShippedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		if uerr := s.m.UpdateTransferTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 凭证按入参顺序写（sort = 入参下标）：运营在抽屉里选的顺序就是页面上的顺序。
		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(ids))
		for i, att := range ids {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				TransferID: e.ID, AttachmentID: att,
				UsageType: TransferAttachmentShip, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferAttachmentsTx(ctx, tx, projectID, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

// transferOutTx 逐行出库（含**无归属行**：它们的 variant_id 为空，走按库存行扣减的路径）。
//
// 为什么无归属行不能走常规变动契约：buildChangeItems 要求每行有 variantID
// （流水的维度是变体 × 仓），而无归属行恰恰没有变体 —— 强行补一个空串会让流水
// 落一条「空变体」的记录，那比不写更糟（对账时无法归属到任何一行）。
//
// 这里按 model 的原子 SQL 直接扣：`quantity -= n` 且 `quantity >= n` 写在 WHERE 里，
// 与发货那条同一手法（守卫在 SQL 里，并发下只有一句能匹配上行）。
func (s *Service) transferOutTx(ctx context.Context, tx *gorm.DB, e *inventorymodel.TransferEntity,
	items []*inventorymodel.TransferItemEntity, lines []inventorydto.StockChangeLineReq) error {
	// 源仓的行（拿 product_id：变动契约建行时需要它，而明细只存了变体与编码快照）。
	srcRows, serr := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if serr != nil {
		return serr
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(srcRows))
	for _, r := range srcRows {
		byID[r.ID] = r
	}
	claimed := make([]inventorymodel.TransferItemEntity, 0, len(items))
	for _, it := range items {
		if it.VariantID != nil && strings.TrimSpace(*it.VariantID) != "" {
			claimed = append(claimed, *it)
			continue
		}
		// 无归属行：按 stock_id 直接扣（守卫在 SQL 里）。
		if err := s.deductUnclaimedStockTx(ctx, tx, e.ProjectID, it.StockID, it.Quantity); err != nil {
			return err
		}
	}
	if len(claimed) == 0 {
		return nil
	}
	claimLines := make([]inventorydto.StockChangeLineReq, 0, len(claimed))
	for _, it := range claimed {
		var variantID, productID string
		if it.VariantID != nil {
			variantID = *it.VariantID
		}
		if src := byID[it.StockID]; src != nil {
			productID = inventorymodel.PtrString(src.ProductID)
		}
		claimLines = append(claimLines, inventorydto.StockChangeLineReq{
			WarehouseID: e.FromWarehouseID, VariantID: variantID, ProductID: productID,
			SKUCode: it.SKUCode, Quantity: it.Quantity,
		})
	}
	return s.DeductStockTx(ctx, tx, &inventorydto.DeductStockReq{
		ProjectID: e.ProjectID, ReasonCode: "transfer_out",
		SourceType: "transfer", SourceRef: e.TransferNo,
		Remark: "调拨出库 → " + e.ToWarehouseID,
		Lines:  claimLines,
	})
}

// deductUnclaimedStockTx 无归属行出库（按库存行 id）：quantity -= n，守卫在 SQL 的 WHERE 里。
//
// 不写流水：流水的维度是「变体 × 仓」，无归属行没有变体 —— 写一条空变体的流水
// 没有任何归属价值。调拨的事实由单据 + 无归属行数量的变化共同表达。
func (s *Service) deductUnclaimedStockTx(ctx context.Context, tx *gorm.DB, projectID, stockID string, quantity int) error {
	affected, err := s.m.DeductUnclaimedStockTx(ctx, tx, projectID, stockID, quantity, time.Now().UTC())
	if err != nil {
		return err
	}
	if affected == 0 {
		return inventoryenums.Err(inventoryenums.ErrStockNotEnough)
	}
	return nil
}

// ReceiveTransfer 签收（**必须带签收凭证**）：状态 in_transit → completed + 逐行在目标仓入库。
func (s *Service) ReceiveTransfer(ctx context.Context, req *inventorydto.TransferReceiveReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	if len(req.AttachmentIDs) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)
	}
	ids, aerr := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if aerr != nil {
		return nil, aerr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusInTransit {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}
		if lerr := s.transferInTx(ctx, tx, e, items); lerr != nil {
			return lerr
		}
		e.Status = TransferStatusCompleted
		e.ReceivedBy = req.OperatorID
		e.ReceivedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		if uerr := s.m.UpdateTransferTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(ids))
		for i, att := range ids {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				TransferID: e.ID, AttachmentID: att,
				UsageType: TransferAttachmentReceive, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferAttachmentsTx(ctx, tx, projectID, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

// transferInTx 逐行在目标仓入库：同 (variant_id, sku_code) 有行则累加、否则建行。
//
// 判据是 **(variant_id, sku_code)** 而不是只按 sku_code：无归属行（variant 为空）
// 与已归属行可以是同一个编码（引用前后），它们的行必须分开 —— 一起累加会把
// 「已归属的货」和「无归属的货」混成一行，后来的引用就撞唯一约束了。
func (s *Service) transferInTx(ctx context.Context, tx *gorm.DB, e *inventorymodel.TransferEntity,
	items []*inventorymodel.TransferItemEntity) error {
	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.ToWarehouseID)
	if err != nil {
		return err
	}
	// 源仓的行（商品 id 的继承来源：无归属行两个都为空，已归属行带 product_id）。
	srcRows, serr := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if serr != nil {
		return serr
	}
	// key = 变体（空串表示无归属）+ '|' + 编码：uuid 与编码都不含 '|'，不会撞键。
	type key struct{ variant, sku string }
	byKey := make(map[key]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		v := ""
		if r.VariantID != nil {
			v = *r.VariantID
		}
		byKey[key{variant: v, sku: r.SKUCode}] = r
	}
	now := time.Now().UTC()
	toCreate := make([]*inventorymodel.StockEntity, 0, len(items))
	toIncrease := make(map[string]int, len(items))
	for _, it := range items {
		v := ""
		if it.VariantID != nil {
			v = *it.VariantID
		}
		existing, ok := byKey[key{variant: v, sku: it.SKUCode}]
		if ok {
			toIncrease[existing.ID] += it.Quantity
			continue
		}
		// 目标仓没有这一行：建一条（无归属进、无归属出 —— variant_id 原样继承）。
		toCreate = append(toCreate, &inventorymodel.StockEntity{
			ID: uuid.NewString(), ProjectID: e.ProjectID, WarehouseID: e.ToWarehouseID,
			// 商品 id 从源仓那一行带过来（无归属行两个都为空 —— 继承即可）。
			ProductID: srcProductID(srcRows, it.StockID), VariantID: it.VariantID,
			SKUCode: it.SKUCode, TrackQuantity: true, Quantity: it.Quantity,
			Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
		})
	}
	for _, row := range toCreate {
		if cerr := s.m.CreateStockRowTx(ctx, tx, row); cerr != nil {
			return cerr
		}
	}
	for stockID, qty := range toIncrease {
		if aerr := s.m.IncreaseStockQuantityTx(ctx, tx, stockID, qty, now); aerr != nil {
			return aerr
		}
	}
	return nil
}

// srcProductID 从源仓的行里取商品 id（无归属行为 nil）。
//
// 单独一个函数只为让 transferInTx 的建行语句读得出来：一行里同时有「目标仓的
// 库存行键」与「源仓的商品 id」两件事时，内联表达式会看不出哪个是哪个。
func srcProductID(rows []*inventorymodel.StockEntity, stockID string) *string {
	for _, r := range rows {
		if r.ID == stockID {
			return r.ProductID
		}
	}
	return nil
}

// ApproveTransfer 同意调拨（仅 requested）：requested → pending，并**重新校验源仓可售**。
//
// 为什么审批时要再校验一次可售：申请与同意之间可能隔了几小时，那批货可能已被订单预占、
// 或被另一张调拨单搬走一部分。只判申请时那一次，会让审批通过的货在出货时才失败 ——
// 而失败点越晚，经手的人越难判断该找谁。
//
// 允许在审批时改目标仓（req.ToWarehouseID 非空才改）：申请只说「这批货要从哪走」，
// 真正发往哪里常常在同意那一刻才定 —— 为此另开一条「改目标仓」的接口只是把同一个决定拆两处。
func (s *Service) ApproveTransfer(ctx context.Context, req *inventorydto.TransferApproveReq) (
	res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}
		if lerr := s.assertTransferAvailableTx(ctx, tx, e, items); lerr != nil {
			return lerr
		}
		if toID := strings.TrimSpace(req.ToWarehouseID); toID != "" && toID != e.ToWarehouseID {
			toWh, werr := s.resolveWarehouse(ctx, projectID, toID)
			if werr != nil {
				return werr
			}
			if toWh.ID == e.FromWarehouseID {
				return inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)
			}
			e.ToWarehouseID = toWh.ID
		}
		e.Status = TransferStatusPending
		e.ApprovedBy = req.OperatorID
		e.ApprovedName = strings.TrimSpace(req.OperatorName)
		e.ApprovedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

// RejectTransfer 驳回调拨（仅 requested）：requested → rejected，理由必填。
//
// 驳回不动库存、不动凭证 —— 它只是把「这批货不该走」这个决定记下来，让申请人知道为什么。
func (s *Service) RejectTransfer(ctx context.Context, req *inventorydto.TransferRejectReq) (
	res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrTransferRejectReasonRequired)
	}
	if len([]rune(reason)) > maxTransferRejectReasonLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrTransferRejectReasonTooLong,
			"最多 "+strconv.Itoa(maxTransferRejectReasonLen)+" 个字符")
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		e.Status = TransferStatusRejected
		e.RejectedBy = req.OperatorID
		e.RejectedName = strings.TrimSpace(req.OperatorName)
		e.RejectedAt = &now
		e.RejectedReason = reason
		e.UpdateTime = now
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

// assertTransferAvailableTx 源仓仍能提供这些货（审批/出货前的最后一道校验）。
//
// 与 transferItemsTx 的校验同源（都用 availableOf = quantity − reserved），
// 但**不合并成一份实现**：建单那次还要构造明细行、顺手归一重复 stock_id，
// 这里只需要一个布尔判断。硬凑一个「既能构造又能校验」的函数，会让两处的意图都看不出来。
func (s *Service) assertTransferAvailableTx(ctx context.Context, tx *gorm.DB,
	e *inventorymodel.TransferEntity, items []*inventorymodel.TransferItemEntity) error {
	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if err != nil {
		return err
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, it := range items {
		row, ok := byID[it.StockID]
		if !ok {
			// 那一行已经不在了（变体删除、仓库换过货）—— 单据里指向的货没了。
			return inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)
		}
		if availableOf(row.Quantity, row.ReservedQuantity) < it.Quantity {
			return inventoryenums.Err(inventoryenums.ErrStockNotEnough)
		}
	}
	return nil
}

// CancelTransfer 取消（requested / pending；in_transit 要先签收或走反向调拨）。
func (s *Service) CancelTransfer(ctx context.Context, req *inventorydto.TransferCancelReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		// requested（还没批）与 pending（批了没发）都可以取消；in_transit 不行 ——
		// 货已经在路上，取消会让「货在哪」变成一个没人回答的问题。
		if e.Status != TransferStatusPending && e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		e.Status = TransferStatusCancelled
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

// ListTransferStockOptions 建单抽屉的候选：某仓里可售的库存行（按编码 / 名称搜，按可售降序）。
//
// 只回可售 > 0 的行：可售为 0 的货摆在下拉里，选中之后必然报「库存不足」——
// 那不是「信息更全」，是把失败推迟到点击之后（与调拨用可售而不是在库同一条理由）。
func (s *Service) ListTransferStockOptions(ctx context.Context, req *inventorydto.TransferStockOptionReq) (
	list []*inventorydto.TransferStockOptionResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	if _, werr := s.resolveWarehouse(ctx, projectID, req.WarehouseID); werr != nil {
		return nil, werr
	}
	rows, err := s.m.ListStocksByWarehouseTx(ctx, nil, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	limit := transferStockOptionLimit(req.Limit)
	list = make([]*inventorydto.TransferStockOptionResp, 0, len(rows))
	for _, r := range rows {
		available := availableOf(r.Quantity, r.ReservedQuantity)
		if available <= 0 {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(r.SKUCode), keyword) &&
			!strings.Contains(strings.ToLower(r.Name), keyword) {
			continue
		}
		variantID := ""
		if r.VariantID != nil {
			variantID = *r.VariantID
		}
		list = append(list, &inventorydto.TransferStockOptionResp{
			StockID: r.ID, SKUCode: r.SKUCode, VariantID: variantID,
			Name:     r.Name,
			Quantity: r.Quantity, ReservedQuantity: r.ReservedQuantity,
			AvailableQuantity: available,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].AvailableQuantity != list[j].AvailableQuantity {
			return list[i].AvailableQuantity > list[j].AvailableQuantity
		}
		return list[i].SKUCode < list[j].SKUCode
	})
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

// transferStockOptionLimit 候选下拉的行数上限。
func transferStockOptionLimit(limit int) int {
	if limit <= 0 {
		return defaultTransferStockOptionLimit
	}
	if limit > maxTransferStockOptionLimit {
		return maxTransferStockOptionLimit
	}
	return limit
}

// ListTransfers 调拨单列表。
func (s *Service) ListTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (
	list []*inventorydto.TransferRowResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	page, size := transferPageArgs(req.Page, req.Size)
	rows, err := s.m.ListTransfers(ctx, inventorymodel.TransferListFilter{
		ProjectID: projectID, Status: strings.TrimSpace(req.Status),
		Keyword: strings.TrimSpace(req.Keyword),
	}, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.TransferRowResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toTransferRowResp(r))
	}
	return list, nil
}

// CountTransfers 调拨单总数（与 ListTransfers 同一份过滤条件）。
func (s *Service) CountTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (n int64, err error) {
	if req == nil {
		return 0, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return 0, perr
	}
	return s.m.CountTransfers(ctx, inventorymodel.TransferListFilter{
		ProjectID: projectID, Status: strings.TrimSpace(req.Status),
		Keyword: strings.TrimSpace(req.Keyword),
	})
}

// GetTransfer 调拨单详情（明细 + 三类凭证）。
func (s *Service) GetTransfer(ctx context.Context, req *inventorydto.GetTransferReq) (
	res *inventorydto.TransferDetailResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.m.GetTransfer(ctx, strings.TrimSpace(req.ID), projectID)
	if gerr != nil {
		return nil, mapTransferNotFound(gerr)
	}
	base, err := s.transferRespTx(ctx, nil, projectID, e.ID)
	if err != nil {
		return nil, err
	}
	detail := &inventorydto.TransferDetailResp{Transfer: base}
	for _, att := range base.Attachments {
		switch att.UsageType {
		case TransferAttachmentRequest:
			detail.RequestAttachments = append(detail.RequestAttachments, att)
		case TransferAttachmentShip:
			detail.ShipAttachments = append(detail.ShipAttachments, att)
		case TransferAttachmentReceive:
			detail.ReceiveAttachments = append(detail.ReceiveAttachments, att)
		}
	}
	return detail, nil
}

// transferRespTx 组装调拨单回执（tx 为 nil 时用非事务读取）。
//
// 明细的「商品名 / 变体标签」取不到时留空（无归属行就是空）：展示信息读不到
// 不该让整张单据读不出来 —— 那个变体可能已经被删了。
func (s *Service) transferRespTx(ctx context.Context, tx *gorm.DB, projectID, id string) (
	res *inventorydto.TransferResp, err error) {
	e, gerr := s.m.GetTransfer(ctx, id, projectID)
	if gerr != nil {
		return nil, mapTransferNotFound(gerr)
	}
	items, ierr := s.m.ListTransferItems(ctx, projectID, id)
	if ierr != nil {
		return nil, ierr
	}
	atts, aerr := s.m.ListTransferAttachments(ctx, projectID, id)
	if aerr != nil {
		return nil, aerr
	}
	row := &inventorydto.TransferRowResp{
		ID: e.ID, TransferNo: e.TransferNo, Status: e.Status,
		Remark:          e.Remark,
		FromWarehouseID: e.FromWarehouseID, ToWarehouseID: e.ToWarehouseID,
		CreateTime: e.CreateTime.Format(time.RFC3339),
	}
	// 两个仓的短码 / 名称：详情回执也要能直接显示「SZ → SH」。
	// 读不到（仓已被删）时留空，不让整张单据读不出来 —— 明细与状态才是单据本体。
	if wh, werr := s.resolveWarehouse(ctx, projectID, e.FromWarehouseID); werr == nil {
		row.FromCode, row.FromName = wh.Code, wh.Name
	}
	if wh, werr := s.resolveWarehouse(ctx, projectID, e.ToWarehouseID); werr == nil {
		row.ToCode, row.ToName = wh.Code, wh.Name
	}
	if e.ShippedAt != nil {
		row.ShippedAt = e.ShippedAt.Format(time.RFC3339)
	}
	if e.ReceivedAt != nil {
		row.ReceivedAt = e.ReceivedAt.Format(time.RFC3339)
	}
	res = &inventorydto.TransferResp{
		TransferRowResp: *row,
		CreateBy:        e.CreateBy, ShippedBy: e.ShippedBy, ReceivedBy: e.ReceivedBy,
		RequestedBy: e.RequestedBy, ApprovedBy: e.ApprovedBy,
		ApprovedName: e.ApprovedName,
		RejectedBy:   e.RejectedBy, RejectedName: e.RejectedName,
		RejectedReason: e.RejectedReason,
	}
	if e.RequestedAt != nil {
		row.RequestedAt = e.RequestedAt.Format(time.RFC3339)
		res.RequestedAt = row.RequestedAt
	}
	if e.ApprovedAt != nil {
		row.ApprovedAt = e.ApprovedAt.Format(time.RFC3339)
		res.ApprovedAt = row.ApprovedAt
	}
	if e.RejectedAt != nil {
		row.RejectedAt = e.RejectedAt.Format(time.RFC3339)
		res.RejectedAt = row.RejectedAt
	}
	total := 0
	for _, it := range items {
		variantID := ""
		if it.VariantID != nil {
			variantID = *it.VariantID
		}
		res.Items = append(res.Items, &inventorydto.TransferItemResp{
			ID: it.ID, StockID: it.StockID, VariantID: variantID,
			SKUCode: it.SKUCode, Quantity: it.Quantity,
		})
		total += it.Quantity
	}
	res.ItemCount = len(res.Items)
	res.TotalQuantity = total
	for _, att := range atts {
		brief := &inventorydto.TransferAttachmentResp{
			AttachmentID: att.AttachmentID, UsageType: att.UsageType,
		}
		// 附件展示信息（名称 / 大小 / 类型 / URL）：查不到时留空 ——
		// 附件被删之后单据仍要能打开（只是那一张显示成「已删除」）。
		if s.attachments != nil {
			if got, aerr := s.attachments.ResolveAttachment(ctx, projectID, att.AttachmentID); aerr == nil && got != nil {
				brief.FileName, brief.FileSize = got.FileName, got.FileSize
				brief.MimeType, brief.URL = got.MimeType, got.URL
			}
		}
		res.Attachments = append(res.Attachments, brief)
	}
	return res, nil
}

// resolveAttachmentIDs 校验凭证 id 存在（逐个查，缺一张即整体拒绝）。
//
// 为什么不用 ResolveAttachments 批量：批量返回的 nil 占位还要逐项检查，
// 而这里需要的是「哪一张不存在」的确定性判断 —— 直接逐个查读起来更直白，
// 且凭证张数是个位数（不构成往返压力）。
func (s *Service) resolveAttachmentIDs(ctx context.Context, projectID string, ids []int64) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if s.attachments == nil {
			// 未注入附件读口（纯库存单测路径）：不校验存在性，只保留 id。
			// 生产装配必注入 —— 缺它时「凭证是否存在」就无从判起。
			out = append(out, id)
			continue
		}
		if _, err := s.attachments.ResolveAttachment(ctx, projectID, id); err != nil {
			if strings.Contains(err.Error(), "ErrAttachmentNotFound") {
				return nil, inventoryenums.Err(inventoryenums.ErrAttachmentNotFound)
			}
			return nil, err
		}
		out = append(out, id)
	}
	// 空入参返回空切片（而不是错误）：**建单的申请凭证是可选的**，
	// 「缺凭证」的强制只发生在出货与签收那两处（调用方先自行判空）。
	return out, nil
}

// nextTransferNoTx 调拨单号：TRF + YYYYMMDD + 6 位零填充序号（冲突由唯一索引兜底）。
func (s *Service) nextTransferNoTx(ctx context.Context, tx *gorm.DB, projectID string, now time.Time) (string, error) {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	n, err := s.m.CountTransfersOnDayTx(ctx, tx, projectID, dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("TRF%s%06d", now.Format("20060102"), n+1), nil
}

// transferPageArgs 调拨单分页参数归一。
func transferPageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultPageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
	}
	if outSize > maxPageSize {
		outSize = maxPageSize
	}
	return outPage, outSize
}

// toTransferRowResp 列表投影 → 响应。
func toTransferRowResp(r *inventorymodel.TransferListRow) *inventorydto.TransferRowResp {
	if r == nil {
		return nil
	}
	out := &inventorydto.TransferRowResp{
		ID: r.ID, TransferNo: r.TransferNo, Status: r.Status,
		Remark:          r.Remark,
		FromWarehouseID: r.FromWarehouseID, FromCode: r.FromCode, FromName: r.FromName,
		ToWarehouseID: r.ToWarehouseID, ToCode: r.ToCode, ToName: r.ToName,
		ItemCount: r.ItemCount, TotalQuantity: r.TotalQuantity,
		CreateTime: r.CreateTime.Format(time.RFC3339),
	}
	if r.ShippedAt != nil {
		out.ShippedAt = r.ShippedAt.Format(time.RFC3339)
	}
	if r.ReceivedAt != nil {
		out.ReceivedAt = r.ReceivedAt.Format(time.RFC3339)
	}
	if r.RequestedAt != nil {
		out.RequestedAt = r.RequestedAt.Format(time.RFC3339)
	}
	if r.ApprovedAt != nil {
		out.ApprovedAt = r.ApprovedAt.Format(time.RFC3339)
	}
	if r.RejectedAt != nil {
		out.RejectedAt = r.RejectedAt.Format(time.RFC3339)
	}
	out.RejectedReason = r.RejectedReason
	out.ApprovedName = r.ApprovedName
	out.RejectedName = r.RejectedName
	return out
}

// orJoinRemark 在已有备注后追加一段说明（空备注时直接取新段）。
//
// 与采购域的 orJoinRemark 同一形状：两张单据的备注都是「人写的自由文本」，
// 系统追加的推进说明必须用统一分隔符接在后面，不能覆盖人工内容。
func orJoinRemark(existing, extra string) string {
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	return existing + "；" + extra
}

// mapTransferNotFound 单据不存在 → 业务错误，其余原样透出。
func mapTransferNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return inventoryenums.Err(inventoryenums.ErrTransferNotFound)
	}
	return err
}

// 编译期断言：本文件实现了契约里声明的调拨用例。
var _ inventorycontract.TransferService = (*Service)(nil)
