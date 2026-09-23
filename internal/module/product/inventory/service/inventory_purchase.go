// inventory_purchase.go — 采购单读写与状态推导（issue #18 验收 1/2）。
//
// 三条语义在本文件维护：
//
//  1. **单据身份**：采购单号工程内唯一（归一成大写）；来源（source_id）是 #17 的货源 ——
//     外部供应商与集团内关联公司都从同一张货源表选，停用的货源不能用来下单；
//     收货仓为空时兜底该工程默认仓（与「未指定仓库 → 默认仓」同一条兜底铁律）。
//  2. **状态是推导值**：pending / partial / received 全部由「已入库数量 与 采购数量」推出
//     （derivePurchaseStatus），没有任何人工置位入口；写入时与行数据在同一事务内自洽。
//  3. **行是原子的**：行全量替换只允许发生在「一行都还没入库」时 —— 已有入库数量的行
//     被改小会让 received_quantity <= quantity 不成立，状态推导也就失去意义。
package inventoryservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

const (
	// maxPurchaseLines 单张采购单的行数上限（与库存变动单批上限同量级）。
	maxPurchaseLines = 100
	// maxPurchaseCodeLen 采购单号长度上限。
	maxPurchaseCodeLen = 32
	// 采购单列表分页。
	defaultPurchasePageSize = 20
	maxPurchasePageSize     = 200
	// reasonCodePurchaseIn / reasonCodeProductionIn 入库用的**内置**变动原因 code
	//（迁移 103 seed，方向 in）。入库不写自由文本原因，一律引用字典条目 ——
	// 自定义原因若用了同一个 code，字典按「工程自定义优先」覆盖显示名，历史流水仍按 code 可读。
	reasonCodePurchaseIn   = "purchase_in"
	reasonCodeProductionIn = "production_in"
)

// CreatePurchaseOrder 新建采购单（验收 1：单头 + 结构化行）。
func (s *Service) CreatePurchaseOrder(ctx context.Context, req *inventorydto.CreatePurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	code, err := normalizePurchaseCode(req.Code)
	if err != nil {
		return nil, err
	}
	source, err := s.resolvePurchaseSource(ctx, projectID, req.SourceID)
	if err != nil {
		return nil, err
	}
	// 收货仓：显式指定需同工程且启用；为空兜底默认仓（缺默认仓时 resolveWarehouse 显式报错）。
	wh, err := s.resolveWarehouse(ctx, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.PurchaseCodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrPurchaseCodeTaken)
	}
	// 单头 id 先定，行在同一事务里带着它一起写（外键 NOT NULL）。
	orderID := uuid.NewString()
	// 收货仓的短码一起传下去：采购行的 sku_code 是**仓库侧快照**，
	// 建行时就要按目标仓口径归一旦校验（口径与理由见 inventory_stock_sku.go）。
	lines, err := buildPurchaseLines(projectID, orderID, wh.Code, req.Lines)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	order := &inventorymodel.PurchaseOrderEntity{
		ID: orderID, ProjectID: projectID, Code: code,
		SourceID: source.ID, WarehouseID: wh.ID,
		Status:    derivePurchaseStatus(lines),
		OrderedAt: now, ExpectedAt: normalizeTime(req.ExpectedAt.TimePtr()),
		Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
		Metadata: orJSON(req.Metadata, "{}"), CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.CreatePurchaseOrderTx(ctx, tx, order, lines)
	}); err != nil {
		return nil, err
	}
	// 响应带货源与仓库名（模板与接口都不用再查一次）。
	return s.purchaseOrderResp(ctx, order, lines, source, wh), nil
}

// UpdatePurchaseOrder 修改采购单（单头逐字段可选；行全量替换仅在未入库时允许）。
func (s *Service) UpdatePurchaseOrder(ctx context.Context, req *inventorydto.UpdatePurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	order, err := s.m.GetPurchaseOrder(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapPurchaseOrderNotFound(err)
	}
	// 行上的工程即解析结果（策略保证同工程），后续写入一律以它作为作用域。
	projectID = order.ProjectID
	if req.SourceID != nil {
		source, serr := s.resolvePurchaseSource(ctx, projectID, *req.SourceID)
		if serr != nil {
			return nil, serr
		}
		order.SourceID = source.ID
	}
	if req.WarehouseID != nil {
		wh, werr := s.resolveWarehouse(ctx, projectID, *req.WarehouseID)
		if werr != nil {
			return nil, werr
		}
		order.WarehouseID = wh.ID
	}
	// 行的 sku_code 快照按**改单后的**收货仓归一（改单可能同时换了仓）：仓库侧编码是
	// 「这条货在这个仓叫什么」，换仓后旧前缀不再成立 —— 与建单走同一条归一入口。
	// 只在真要换行时解析：改备注这类不动行的调用不该因为「当年的收货仓后来被停用」而失败。
	var orderWarehouseCode string
	if req.ReplaceLines {
		orderWarehouse, werr := s.resolveWarehouse(ctx, projectID, order.WarehouseID)
		if werr != nil {
			return nil, werr
		}
		orderWarehouseCode = orderWarehouse.Code
	}
	if req.ExpectedAt != nil {
		order.ExpectedAt = normalizeTime(req.ExpectedAt.TimePtr())
	}
	if req.ClearExpectedAt {
		order.ExpectedAt = nil
	}
	if req.Remark != nil {
		order.Remark = strings.TrimSpace(*req.Remark)
	}
	if req.OperatorID != nil {
		order.OperatorID = strings.TrimSpace(*req.OperatorID)
	}
	if req.Metadata != nil {
		order.Metadata = orJSON(req.Metadata, "{}")
	}

	var lines []*inventorymodel.PurchaseLineEntity
	now := time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// 锁单头：与登记入库共用同一把锁，改单与收货不会交错出「半新半旧」的行集合。
		if _, lerr := s.m.LockPurchaseOrderTx(ctx, tx, order.ID, projectID); lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		current, lerr := s.m.ListPurchaseLinesTx(ctx, tx, order.ID, projectID)
		if lerr != nil {
			return lerr
		}
		lines = current
		if req.ReplaceLines {
			for _, line := range current {
				if line.ReceivedQuantity > 0 {
					// 已入库的行不可再改（改了「已入库 ≤ 采购数量」就不成立）。
					return errors.New(inventoryenums.ErrPurchaseLinesLocked)
				}
			}
			replaced, berr := buildPurchaseLines(projectID, order.ID, orderWarehouseCode, req.Lines)
			if berr != nil {
				return berr
			}
			if rerr := s.m.ReplacePurchaseLinesTx(ctx, tx, order.ID, projectID, replaced); rerr != nil {
				return rerr
			}
			lines = replaced
		}
		order.Status = derivePurchaseStatus(lines)
		order.UpdatedAt = now
		return s.m.UpdatePurchaseOrderTx(ctx, tx, order)
	}); err != nil {
		return nil, err
	}
	return s.purchaseOrderResp(ctx, order, lines, nil, nil), nil
}

// GetPurchaseOrder 采购单详情（含全部采购行）。
func (s *Service) GetPurchaseOrder(ctx context.Context, req *inventorydto.GetPurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	order, err := s.m.GetPurchaseOrder(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapPurchaseOrderNotFound(err)
	}
	lines, err := s.m.ListPurchaseLines(ctx, order.ID, order.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.purchaseOrderResp(ctx, order, lines, nil, nil), nil
}

// ListPurchaseOrders 采购单列表（状态 / 货源 / 关键词都是可组合的筛选维度）。
func (s *Service) ListPurchaseOrders(ctx context.Context, req *inventorydto.ListPurchaseOrderReq) (list []*inventorydto.PurchaseOrderResp, err error) {
	filter, page, size, err := s.purchaseOrderFilter(ctx, req)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListPurchaseOrderRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	orderIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		orderIDs = append(orderIDs, r.ID)
	}
	// 行一次性批量取回（列表页不产生 N+1）。
	allLines, err := s.m.ListPurchaseLinesByOrders(ctx, orderIDs, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	byOrder := make(map[string][]*inventorymodel.PurchaseLineEntity, len(orderIDs))
	for _, line := range allLines {
		byOrder[line.OrderID] = append(byOrder[line.OrderID], line)
	}
	list = make([]*inventorydto.PurchaseOrderResp, 0, len(rows))
	for _, r := range rows {
		lines := byOrder[r.ID]
		resp := purchaseOrderRespFromRow(r)
		resp.Lines = toPurchaseLineResps(lines)
		list = append(list, resp)
	}
	return list, nil
}

// CountPurchaseOrders 采购单总张数（分页页面的「共 N 条」与总页数）。
//
// **与 ListPurchaseOrders 共用同一个 purchaseOrderFilter**：状态推导值 / 货源 / 关键词 /
// 工程作用域的归一只有一份实现。状态这一维尤其不能各抄一遍 —— 页面筛的是**推导状态**
// （未入库 / 部分入库 / 已入库），由 service 归一到 model 的过滤，抄错时计数与列表分叉，
// 分页条会给出一个永远翻不到底的页数。
func (s *Service) CountPurchaseOrders(ctx context.Context, req *inventorydto.ListPurchaseOrderReq) (n int64, err error) {
	filter, _, _, err := s.purchaseOrderFilter(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.CountPurchaseOrders(ctx, filter)
}

// purchaseOrderFilter 把采购单列表请求归一成 model 过滤条件 + 分页参数（List / Count 共用）。
func (s *Service) purchaseOrderFilter(ctx context.Context, req *inventorydto.ListPurchaseOrderReq) (filter inventorymodel.PurchaseOrderFilter, page, size int, err error) {
	if req == nil {
		return filter, page, size, errors.New(inventoryenums.ErrInvalidParam)
	}
	status, serr := normalizePurchaseStatusFilter(req.Status)
	if serr != nil {
		return filter, page, size, serr
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return filter, page, size, perr
	}
	filter = inventorymodel.PurchaseOrderFilter{
		ProjectID: projectID, Status: status,
		SourceID: strings.TrimSpace(req.SourceID), Keyword: strings.TrimSpace(req.Keyword),
	}
	page, size = purchasePageArgs(req.Page, req.Size)
	return filter, page, size, nil
}

// —— 内部工具 ——

// buildPurchaseLines 归一入参并构造采购行（校验集中在此：数量 / 单价 / 重复 SKU / 仓库侧 SKU 编码）。
//
// warehouseCode 是该单的收货仓短码：行的 sku_code 是**仓库侧快照**，按它归一并校验
// （幂等剥前缀 + 空串拒绝，见 inventory_stock_sku.go）。空的 sku_code 不再静默落库 ——
// 那正是「页面表单没提交该字段」时被放过的那条路：行建出来了、编码是空的，
// 一路带到登记入库，最后在库存真源上留下一条没有身份的货（或撞唯一约束）。
func buildPurchaseLines(projectID, orderID, warehouseCode string, rows []inventorydto.PurchaseLineReq) (lines []*inventorymodel.PurchaseLineEntity, err error) {
	if len(rows) == 0 {
		return nil, errors.New(inventoryenums.ErrPurchaseLinesRequired)
	}
	if len(rows) > maxPurchaseLines {
		return nil, errors.New(inventoryenums.ErrPurchaseLinesTooMany)
	}
	now := time.Now().UTC()
	seen := make(map[string]bool, len(rows))
	lines = make([]*inventorymodel.PurchaseLineEntity, 0, len(rows))
	for i, row := range rows {
		variantID := strings.TrimSpace(row.VariantID)
		if variantID == "" {
			return nil, errors.New(inventoryenums.ErrPurchaseLineRequired)
		}
		if seen[variantID] {
			// 同一张单里同一个 SKU 只允许一行：已入库数量的归属必须不含糊。
			return nil, errors.New(inventoryenums.ErrPurchaseLineDuplicate)
		}
		seen[variantID] = true
		if row.Quantity <= 0 {
			return nil, errors.New(inventoryenums.ErrPurchaseQuantityInvalid)
		}
		if row.UnitPrice <= 0 {
			return nil, errors.New(inventoryenums.ErrPurchasePriceInvalid)
		}
		sortValue := row.Sort
		if sortValue == 0 {
			sortValue = i
		}
		// 仓库侧编码的归一与校验：空串（含只给了仓码前缀的编码）一律拒绝 ——
		// 「没给编码」必须当场变成一句可行动的错误，而不是一条没有身份的采购行。
		skuCode, serr := normalizeStockSKU(row.SKUCode, warehouseCode)
		if serr != nil {
			return nil, serr
		}
		lines = append(lines, &inventorymodel.PurchaseLineEntity{
			ID: uuid.NewString(), OrderID: orderID, ProjectID: projectID,
			ProductID: strings.TrimSpace(row.ProductID), VariantID: variantID,
			SKUCode:  skuCode,
			Quantity: row.Quantity, ReceivedQuantity: 0, UnitPrice: row.UnitPrice,
			Sort: sortValue, Remark: strings.TrimSpace(row.Remark),
			Metadata: orJSON(nil, "{}"), CreatedAt: now, UpdatedAt: now,
		})
	}
	return lines, nil
}

// derivePurchaseStatus 由「已入库数量 与 采购数量」推导采购单状态（验收 2）。
//
// 三种结果互斥且穷尽：全部未入库 → pending；全部收满 → received；其余 → partial。
// 没有任何人工置位入口，改单与收货之后都调用它重算，因此状态永远与行数据自洽。
func derivePurchaseStatus(lines []*inventorymodel.PurchaseLineEntity) (status string) {
	if len(lines) == 0 {
		return inventoryenums.PurchaseStatusPending
	}
	receivedAll := true
	receivedAny := false
	for _, line := range lines {
		if line.ReceivedQuantity >= line.Quantity {
			receivedAny = true
			continue
		}
		receivedAll = false
		if line.ReceivedQuantity > 0 {
			receivedAny = true
		}
	}
	switch {
	case receivedAll:
		return inventoryenums.PurchaseStatusReceived
	case receivedAny:
		return inventoryenums.PurchaseStatusPartial
	default:
		return inventoryenums.PurchaseStatusPending
	}
}

// resolvePurchaseSource 解析采购来源：必须存在、同工程、启用中（停用 = 不再选用）。
func (s *Service) resolvePurchaseSource(ctx context.Context, projectID, sourceID string) (e *inventorymodel.SourceEntity, err error) {
	id := strings.TrimSpace(sourceID)
	if id == "" {
		return nil, errors.New(inventoryenums.ErrPurchaseSourceRequired)
	}
	e, err = s.m.GetSource(ctx, id, projectID)
	if err != nil {
		return nil, mapSourceNotFound(err)
	}
	if e.ProjectID != projectID {
		return nil, errors.New(inventoryenums.ErrSourceNotFound)
	}
	if e.Status != inventoryenums.SourceStatusActive {
		return nil, errors.New(inventoryenums.ErrPurchaseSourceDisabled)
	}
	return e, nil
}

// purchaseOrderResp 组装采购单响应：源 / 仓展示信息为空时现查（详情路径）。
func (s *Service) purchaseOrderResp(ctx context.Context, e *inventorymodel.PurchaseOrderEntity,
	lines []*inventorymodel.PurchaseLineEntity, source *inventorymodel.SourceEntity,
	wh *inventorymodel.WarehouseEntity) *inventorydto.PurchaseOrderResp {
	if source == nil {
		if got, err := s.m.GetSource(ctx, e.SourceID, e.ProjectID); err == nil {
			source = got
		}
	}
	if wh == nil {
		if got, err := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); err == nil {
			wh = got
		}
	}
	resp := &inventorydto.PurchaseOrderResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code,
		SourceID: e.SourceID, WarehouseID: e.WarehouseID,
		Status: e.Status, OrderedAt: utils.NewJSONTime(e.OrderedAt), ExpectedAt: utils.NewJSONTimePtr(e.ExpectedAt),
		Remark: e.Remark, OperatorID: e.OperatorID,
		Lines: toPurchaseLineResps(lines), CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
	if source != nil {
		resp.SourceName, resp.SourceType = source.Name, source.Type
	}
	if wh != nil {
		resp.WarehouseName = wh.Name
	}
	resp.TotalQuantity, resp.ReceivedQuantity = sumPurchaseLines(lines)
	return resp
}

// purchaseOrderRespFromRow 列表路径的响应组装（行汇总直接取投影里的聚合值，
// 货源 / 仓库名来自同一次 join，不再逐单回查）。
func purchaseOrderRespFromRow(row *inventorymodel.PurchaseOrderRow) *inventorydto.PurchaseOrderResp {
	resp := &inventorydto.PurchaseOrderResp{
		ID: row.ID, ProjectID: row.ProjectID, Code: row.Code,
		SourceID: row.SourceID, SourceName: row.SourceName, SourceType: row.SourceType,
		WarehouseID: row.WarehouseID, WarehouseName: row.WarehouseName,
		Status: row.Status, OrderedAt: utils.NewJSONTime(row.OrderedAt), ExpectedAt: utils.NewJSONTimePtr(row.ExpectedAt),
		Remark: row.Remark, OperatorID: row.OperatorID,
		TotalQuantity: row.TotalQuantity, ReceivedQuantity: row.ReceivedQuantity,
		CreatedAt: utils.NewJSONTime(row.CreatedAt), UpdatedAt: utils.NewJSONTime(row.UpdatedAt),
	}
	return resp
}

// toPurchaseLineResps 采购行实体 → 响应（带「未入库余量」这个推导量）。
func toPurchaseLineResps(lines []*inventorymodel.PurchaseLineEntity) []*inventorydto.PurchaseLineResp {
	out := make([]*inventorydto.PurchaseLineResp, 0, len(lines))
	for _, line := range lines {
		out = append(out, &inventorydto.PurchaseLineResp{
			ID: line.ID, ProductID: line.ProductID, VariantID: line.VariantID,
			SKUCode: line.SKUCode, Quantity: line.Quantity,
			ReceivedQuantity:    line.ReceivedQuantity,
			OutstandingQuantity: line.Quantity - line.ReceivedQuantity,
			UnitPrice:           line.UnitPrice, Sort: line.Sort, Remark: line.Remark,
		})
	}
	return out
}

// sumPurchaseLines 汇总采购数量与已入库数量。
func sumPurchaseLines(lines []*inventorymodel.PurchaseLineEntity) (total, received int) {
	for _, line := range lines {
		total += line.Quantity
		received += line.ReceivedQuantity
	}
	return total, received
}

// normalizePurchaseCode 归一采购单号：大写，允许字母 / 数字 / 下划线 / 连字符。
func normalizePurchaseCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrPurchaseCodeRequired)
	}
	if len(out) > maxPurchaseCodeLen {
		return "", errors.New(inventoryenums.ErrPurchaseCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", errors.New(inventoryenums.ErrPurchaseCodeInvalid)
	}
	return out, nil
}

// normalizePurchaseStatusFilter 归一状态筛选（空串表示不过滤）。
func normalizePurchaseStatusFilter(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return "", nil
	case inventoryenums.PurchaseStatusPending, inventoryenums.PurchaseStatusPartial,
		inventoryenums.PurchaseStatusReceived:
		return strings.ToLower(strings.TrimSpace(status)), nil
	default:
		return "", errors.New(inventoryenums.ErrPurchaseStatusInvalid)
	}
}

// purchasePageArgs 归一采购单分页参数。
func purchasePageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultPurchasePageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
		if outSize > maxPurchasePageSize {
			outSize = maxPurchasePageSize
		}
	}
	return outPage, outSize
}

// normalizeTime 归一可空时间（nil 保持 nil，非 nil 统一成 UTC）。
func normalizeTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// mapPurchaseOrderNotFound 把仓储的「记录不存在」映射成业务错误。
func mapPurchaseOrderNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrPurchaseOrderNotFound)
	}
	return err
}
