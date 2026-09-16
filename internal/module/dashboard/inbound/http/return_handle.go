// return_handle.go — 后台退货入库（RMA）管理页（BIZ-1 销售侧）。
//
// 订单模块的退货接口已就绪（/api/order/return/* 五个接口挂在三层链上：
// Session + CSRF + Casbin），但后台没有管理界面 —— 运营看不到也审不了退货申请。
// 本文件补齐这个入口：工程切换 + 状态计数 + 组合筛选 + 申请列表 + 详情
// （单头 / 逐行明细 / 订单摘要）+ 三个写操作，**无 JS 也能用** ——
// 所有链接都是普通 GET（详情靠 ?returnId= 在同一页展开），所有写操作都是原生表单 POST + csrf_token 隐藏域。
//
// 四条与退货流程的约定：
//
//  1. **先入库、后退款**：这是退货流程唯一的一条路径。反过来就是「钱退了、货没回来」，
//     而这正是退货最容易被薅的地方 —— 所以本页没有、也不应该有「只退款」的按钮。
//     状态机（真值在服务端，本页只按状态决定渲染哪些操作，不判合法性）：
//     requested 待审核 → approved 已同意待收货 → received 已入库待退款 → completed 已完成；
//     rejected / cancelled 是终态。
//
//  2. **一步到底是常态**：现实里货往往早就到了（客户先联系客服、客服再走系统），
//     所以「同意」表单里有 autoReceive 勾选项 —— 勾上即在同一次调用里完成入库 + 退款。
//
//  3. 金额与状态文案只在服务端算一次：RefundLabel / UnitPriceLabel / StatusLabel 都是
//     ReturnResp 里算好的展示字段，handler 与模板都不做任何金额换算 ——
//     两处换算迟早会分叉，而退货单分叉出来的差额是要对账的。
//
//  4. 操作人（OperatorType / OperatorID / OperatorName）由 handler 从会话覆盖写入，
//     表单里不存在这三个字段：能被客户端伪造的操作人，等于审计上没有操作人。
package dashboardhttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
)

const (
	// returnPageTitle 页面标题（dashboard enums 里没有这个键，直接走 withI18n 的 fallback 链路）。
	returnPageTitle = "退货入库"
	// returnOperatorTypeAdmin 后台操作人类型（表单不传，由 handler 覆盖写入）。
	returnOperatorTypeAdmin = "admin"
	// returnIDInvalidText 表单里的退货单 id 不合法（本页自造文案，已进本页白名单）。
	returnIDInvalidText = "退货单编号不合法，请回到列表页重新操作。"
)

// 退货状态取值（展示用字面量，刻意不 import 订单模块的 model 包）。
const (
	returnStatusRequested = "requested"
	returnStatusApproved  = "approved"
	returnStatusReceived  = "received"
	returnStatusCompleted = "completed"
	returnStatusRejected  = "rejected"
	returnStatusCancelled = "cancelled"
)

// returnStatusViews 退货状态 → 计数条 / 下拉用的中文标签 + 徽章样式。
//
// 标签只用于「没有单据上下文」的位置（计数条、状态筛选下拉）；
// 单据上的状态文案一律用服务端给的 StatusLabel —— 同一个状态在后台页、访客片段、
// 邮件里必须是同一句话，本页不另造一份文案表。
//
// Highlight 标记「有人等着处理」的状态：待审核（要审）与待退款（要补退款），
// 这两个数字在计数条上加粗，运营一眼能看到积压。
var returnStatusViews = []struct {
	Value     string
	Label     string
	Badge     string
	Highlight bool
}{
	{returnStatusRequested, "待审核", "badge-warning", true},
	{returnStatusApproved, "待收货", "badge-warning", false},
	{returnStatusReceived, "待退款", "badge-warning", true},
	{returnStatusCompleted, "已完成", "badge-success", false},
	{returnStatusRejected, "已拒绝", "badge-mute", false},
	{returnStatusCancelled, "已撤销", "badge-mute", false},
}

// returnFacingExtras 本页允许原样显示的**模块外**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），
// 后台审核侧的文案不在其中：同意 / 拒绝 / 入库成功的提示，以及三条只在后台出现的
// 状态错误 —— 不补这一层，运营点下去只会看到「系统内部错误」，
// 而真正的原因（「该申请不在待审核状态」）就丢了。
//
// 最后一条是本页自造文案：?err= 是用户可编辑的查询参数，回显时同样要过白名单，
// 自造文案不登记在这里就等着被自己吞掉。
var returnFacingExtras = []string{
	orderenums.MsgReturnApproved, orderenums.MsgReturnRejected, orderenums.MsgReturnReceived,
	orderenums.ErrReturnRejectReasonRequired, orderenums.ErrReturnNotReviewable, orderenums.ErrReturnNotReceivable,
	returnIDInvalidText,
}

// returnPageHandle 退货入库管理页处理器。
type returnPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
	// inventories 只用来渲染「入库仓库」下拉：退货入库落在哪个仓是**运营的决定**，
	// 让运营填一个仓库 ID 是把内部标识当输入项 —— 填错不报错，货就进错仓了。
	inventories inventorycontract.InventoryService
}

// NewReturnPageHandle 构造。
func NewReturnPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	inventories inventorycontract.InventoryService) *returnPageHandle {
	return &returnPageHandle{orders: orders, projects: projects, inventories: inventories}
}

// returnWarehouseOption 入库仓库下拉项（value 是仓库 id，Label 带短码与默认标记）。
type returnWarehouseOption struct {
	ID    string
	Label string
}

// warehouseOptions 某工程的仓库下拉项（默认仓在最前，由 inventory 侧排序保证）。
//
// 取不到时返回空表：页面据此只渲染「默认仓」一个选项，而不是让整页报错 ——
// 退货审核本身不依赖仓库列表（服务端在仓库为空时会兜底到默认仓）。
func (h *returnPageHandle) warehouseOptions(ctx context.Context, projectID string) []returnWarehouseOption {
	if h.inventories == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	list, err := h.inventories.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil
	}
	out := make([]returnWarehouseOption, 0, len(list))
	for _, w := range list {
		if opt, ok := warehouseOptionOf(w); ok {
			out = append(out, opt)
		}
	}
	return out
}

// warehouseOptionOf 仓库 → 下拉项（纯函数：IO 与文案拼装分开，口径可单测）。
//
// 三条取舍：
//
//	· **停用的仓不进下拉**（历史流水仍按 id 可读，但新入库不该再选它）；
//	· 标签带短码 —— 短码是仓库在 SKU 编码里的前缀，运营认得出「SZ」比认全名快；
//	· 默认仓显式标注 —— 留空时的兜底目标要让运营看得见，否则「我什么都没选」与
//	  「我以为会进某个仓」之间就靠猜。
func warehouseOptionOf(w *inventorydto.WarehouseResp) (opt returnWarehouseOption, ok bool) {
	if w == nil || strings.TrimSpace(w.ID) == "" {
		return opt, false
	}
	if w.Status != "" && w.Status != "enabled" {
		return opt, false
	}
	label := strings.TrimSpace(w.Name)
	if code := strings.TrimSpace(w.Code); code != "" {
		if label == "" {
			label = code
		} else {
			label = label + "（" + code + "）"
		}
	}
	if w.IsDefault {
		label += " · 默认仓"
	}
	return returnWarehouseOption{ID: w.ID, Label: label}, true
}

// returnFilter 页面筛选条件（GET 参数，全部可选）。
type returnFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（退货单号 / 订单号 / 客户邮箱，服务端决定匹配哪些列）。
	Keyword string
	// OrderID 非 0 时只列该订单的退货申请（从订单页跳进来时会带上）。
	OrderID uint64
	// ReturnID 非 0 时在列表上方渲染详情区（同一页面靠查询参数切换，不新开路由）。
	ReturnID uint64
}

// ReturnsPage 退货入库管理页（GET /admin/returns）。
func (h *returnPageHandle) ReturnsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	page, limit := orderListWindow(c)
	filter := returnFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		OrderID:  orderQueryID(c.Query("orderId")),
		ReturnID: orderQueryID(c.Query("returnId")),
	}

	// 回显文案：?err= / ?ok= 都过白名单，查不到的一律收口
	// （查询参数是用户可编辑的，不能拿它当「业务提示」直接显示）。
	pageErr := orderQueryText(c, c.Query("err"), orderInternalText(c))
	pageOk := returnFacingQueryText(c, c.Query("ok"))

	rows := []gin.H{}
	counters := returnStatusCounters(nil, filter, selected)
	detail := gin.H{}
	var total int64

	if selected != "" {
		list, lerr := h.orders.ListReturns(ctx, &orderdto.ReturnListReq{
			ProjectID: selected,
			Status:    filter.Status,
			Keyword:   filter.Keyword,
			OrderID:   filter.OrderID,
			Offset:    (page - 1) * limit,
			Limit:     limit,
		})
		if lerr != nil {
			pageErr = firstNonEmpty(pageErr, returnFacingError(c, lerr))
		} else {
			total = list.Total
			// 计数不受筛选影响（它回答「各状态各有多少单」这个全局问题），直接用服务端给的整份计数。
			counters = returnStatusCounters(list.Counts, filter, selected)
			for _, r := range list.List {
				rows = append(rows, returnListRow(r, filter, selected, page, limit))
			}
		}
		if filter.ReturnID > 0 {
			det, derr := h.orders.GetReturn(ctx, filter.ReturnID)
			if derr != nil {
				pageErr = firstNonEmpty(pageErr, returnFacingError(c, derr))
			} else {
				detail = returnDetailView(det, filter, selected, page, limit)
			}
		}
	}

	data := withCSRF(c, gin.H{
		"title":           returnPageTitle,
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		"Statuses":        counters,
		"StatusOptions":   returnStatusOptions(),
		"FilterStatus":    filter.Status,
		"FilterLabel":     returnStatusLabel(filter.Status),
		"FilterKeyword":   filter.Keyword,
		"FilterOrderID":   returnOrderIDText(filter.OrderID),
		"ClearOrderURL":   filterBaseURL("/admin/returns", returnFilterValues(selected, returnFilter{Status: filter.Status, Keyword: filter.Keyword})),
		"PendingHint":     returnPendingHint(counters),
		"Rows":            rows,
		"Total":           total,
		"Warehouses":      h.warehouseOptions(ctx, selected),
		"Detail":          detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这个键决定要不要渲染详情块。
		"HasDetail": len(detail) > 0,
		"Page":      page,
		"Limit":     limit,
		"Err":       pageErr,
		"Ok":        pageOk,
	})
	base := filterBaseURL("/admin/returns", returnFilterValues(selected, filter))
	for k, v := range buildPagination(total, page, limit, base, translateFor(c)).templateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/returns.html", data)
}

// ReturnApprove 同意退货申请（POST /admin/returns/approve）。
//
// autoReceive 勾上时服务端会在同一次调用里完成「入库 + 退款」（一步到底）；
// 不勾则停在已同意待收货，等仓库点货后再走 ReturnReceive。
func (h *returnPageHandle) ReturnApprove(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		returnRedirect(c, "", returnIDInvalidText)
		return
	}
	res, err := h.orders.ApproveReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
		ReturnID:      returnID,
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		AutoReceive:   returnFormBool(c.PostForm("autoReceive")),
		WarehouseID:   strings.TrimSpace(c.PostForm("warehouseId")),
		TransactionID: strings.TrimSpace(c.PostForm("transactionId")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorType: returnOperatorTypeAdmin,
		OperatorID:   orderOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	})
	if err != nil {
		returnRedirect(c, "", returnFacingError(c, err))
		return
	}
	// 勾了 autoReceive 的单会直接落到「已入库待退款」甚至「已完成」，
	// 提示语跟着实际落地状态走，否则运营会以为只批了同意、又去点一次收货。
	ok := orderenums.MsgReturnApproved
	if res != nil && res.Status != returnStatusApproved {
		ok = orderenums.MsgReturnReceived
	}
	returnRedirect(c, ok, "")
}

// ReturnReject 拒绝退货申请（POST /admin/returns/reject）：**必须给理由** ——
// 客户要知道为什么，否则他会再申请一次。
func (h *returnPageHandle) ReturnReject(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		returnRedirect(c, "", returnIDInvalidText)
		return
	}
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		returnRedirect(c, "", orderenums.ErrReturnRejectReasonRequired)
		return
	}
	if _, err := h.orders.RejectReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
		ReturnID:     returnID,
		Remark:       remark,
		OperatorType: returnOperatorTypeAdmin,
		OperatorID:   orderOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	}); err != nil {
		returnRedirect(c, "", returnFacingError(c, err))
		return
	}
	returnRedirect(c, orderenums.MsgReturnRejected, "")
}

// ReturnReceive 确认收货（POST /admin/returns/receive）：先入库、再退款。
//
// 这个入口同时承担两件事，因为服务端把它们做成了幂等且可重入的同一条用例：
//
//	· status=approved：货到仓库了，点这一下 —— 入库（加回库存）成功后立刻退款；
//	· status=received：上一次入库成功但退款没做完（通道抖动 / 网络断了），
//	  再点一次只会补做退款 —— 入库那一步服务端会识别出已完成，不会重复加库存。
//
// 所以 received 状态的按钮文案是「补退款」，但它走的是同一个接口。
func (h *returnPageHandle) ReturnReceive(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		returnRedirect(c, "", returnIDInvalidText)
		return
	}
	if _, err := h.orders.ReceiveReturn(c.Request.Context(), &orderdto.ReturnReceiveReq{
		ReturnID:      returnID,
		WarehouseID:   strings.TrimSpace(c.PostForm("warehouseId")),
		TransactionID: strings.TrimSpace(c.PostForm("transactionId")),
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		OperatorType:  returnOperatorTypeAdmin,
		OperatorID:    orderOperatorID(c),
		OperatorName:  builtin.GetUsername(c),
	}); err != nil {
		returnRedirect(c, "", returnFacingError(c, err))
		return
	}
	returnRedirect(c, orderenums.MsgReturnReceived, "")
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// returnListRow 退货单 → 列表行视图（状态文案、金额、时间都在这里定型）。
func returnListRow(r *orderdto.ReturnResp, filter returnFilter, projectID string, page, limit int) gin.H {
	if r == nil {
		return gin.H{}
	}
	vals := returnFilterValues(projectID, filter)
	// 详情链接在同一页面上展开（靠 returnId 查询参数），因此把当前窗口一起带上：
	// 关掉详情或再翻页时，用户还站在原来那一屏。
	vals["returnId"] = strconv.FormatUint(r.ID, 10)
	vals["page"] = strconv.Itoa(page)
	vals["limit"] = strconv.Itoa(limit)
	return gin.H{
		"ID":            r.ID,
		"ReturnNo":      r.ReturnNo,
		"OrderNo":       r.OrderNo,
		"Status":        r.Status,
		"StatusLabel":   r.StatusLabel,
		"Badge":         returnStatusBadge(r.Status),
		"CustomerName":  orderTextOrEmpty(r.CustomerName),
		"CustomerEmail": orderTextOrEmpty(r.CustomerEmail),
		"RefundLabel":   r.RefundLabel,
		"CreatedAt":     orderTimeLabel(r.CreateTime.Time()),
		"DetailURL":     filterBaseURL("/admin/returns", vals),
		"Expanded":      filter.ReturnID == r.ID,
	}
}

// returnDetailView 退货单详情（单头 + 逐行明细 + 订单摘要 + 按状态决定的操作）→ 模板视图。
func returnDetailView(d *orderdto.ReturnDetailResp, filter returnFilter, projectID string, page, limit int) gin.H {
	if d == nil || d.Return == nil {
		return gin.H{}
	}
	ret := d.Return

	items := make([]gin.H, 0, len(ret.Items))
	for _, it := range ret.Items {
		if it == nil {
			continue
		}
		items = append(items, gin.H{
			"OrderItemID":      it.OrderItemID,
			"ProductName":      orderTextOrEmpty(it.ProductName),
			"VariantLabel":     orderTextOrEmpty(it.VariantLabel),
			"SKU":              orderTextOrEmpty(it.SKU),
			"UnitPriceLabel":   it.UnitPriceLabel,
			"Quantity":         it.Quantity,
			"ReceivedQuantity": it.ReceivedQuantity,
			"RefundLabel":      it.RefundLabel,
			// Returnable 是「该订单项还能退多少」—— 运营据此判断这单还能不能再收一件退货。
			"Returnable": it.Returnable,
		})
	}

	// 订单摘要：审核时不可能不看订单（退货针对的是这一单），所以详情里一并给出。
	order := gin.H{}
	if d.Order != nil {
		o := d.Order
		order = gin.H{
			"OrderNo":       o.OrderNo,
			"OrderURL":      filterBaseURL("/admin/orders", map[string]string{"project": projectID, "orderId": strconv.FormatUint(o.ID, 10)}),
			"Status":        o.Status,
			"StatusLabel":   orderStatusLabel(o.Status),
			"Badge":         orderStatusBadge(o.Status),
			"TotalLabel":    orderMoneyLabel(o.Total, o.Currency),
			"CreatedAt":     orderTimeLabel(o.CreateTime.Time()),
			"CustomerName":  orderTextOrEmpty(o.CustomerName),
			"CustomerEmail": orderTextOrEmpty(o.CustomerEmail),
			"ShipName":      orderTextOrEmpty(o.ShipName),
			"ShipPhone":     orderTextOrEmpty(o.ShipPhone),
			"ShipAddress": orderAddressLabel(o.ShipProvince, o.ShipCity, o.ShipDistrict,
				o.ShipAddress, o.ShipZip),
		}
	}

	// 表单回跳参数：操作完回到同一张退货单的详情，而不是被弹回未筛选的列表第一页。
	form := gin.H{
		"Project":  projectID,
		"OrderID":  returnOrderIDText(filter.OrderID),
		"ReturnID": strconv.FormatUint(ret.ID, 10),
		"Status":   filter.Status,
		"Keyword":  filter.Keyword,
		"Page":     strconv.Itoa(page),
		"Limit":    strconv.Itoa(limit),
	}

	// 操作开关按状态给：**不渲染当前状态不可能成功的按钮**（合法性仍由服务端裁决）。
	// requested 才有「同意 / 拒绝」；approved 是「确认收货并退货」；
	// received 只补退款 —— 同一入口，语义不同。
	requested := ret.Status == returnStatusRequested
	approved := ret.Status == returnStatusApproved
	received := ret.Status == returnStatusReceived

	return gin.H{
		"Head": gin.H{
			"ID":            ret.ID,
			"ReturnNo":      ret.ReturnNo,
			"OrderNo":       ret.OrderNo,
			"OrderURL":      filterBaseURL("/admin/orders", map[string]string{"project": projectID, "orderId": strconv.FormatUint(ret.OrderID, 10)}),
			"Status":        ret.Status,
			"StatusLabel":   ret.StatusLabel,
			"Badge":         returnStatusBadge(ret.Status),
			"Reason":        orderTextOrEmpty(ret.Reason),
			"RefundLabel":   ret.RefundLabel,
			"CustomerName":  orderTextOrEmpty(ret.CustomerName),
			"CustomerEmail": orderTextOrEmpty(ret.CustomerEmail),
			"AdminNote":     orderTextOrEmpty(ret.AdminNote),
			"ReviewerName":  orderTextOrEmpty(ret.ReviewerName),
			"ReviewedAt":    orderTimeLabelPtr(ret.ReviewedAt.TimePtr()),
			"ReceivedAt":    orderTimeLabelPtr(ret.ReceivedAt.TimePtr()),
			"RefundedAt":    orderTimeLabelPtr(ret.RefundedAt.TimePtr()),
			"TransactionID": orderTextOrEmpty(ret.TransactionID),
			"CreatedAt":     orderTimeLabel(ret.CreateTime.Time()),
			"UpdatedAt":     orderTimeLabel(ret.UpdateTime.Time()),
		},
		"Items":          items,
		"Order":          order,
		"HasOrder":       len(order) > 0,
		"Note":           returnStatusNote(ret.Status),
		"CanApprove":     requested,
		"CanReject":      requested,
		"CanReceive":     approved,
		"CanRetryRefund": received,
		"Form":           form,
	}
}

// returnStatusCounters 状态计数条（「全部」+ 六个状态，各自带一个筛选链接）。
//
// counts 为 nil（未查询 / 查询失败）时全部按 0 渲染：计数条是导航，不是结论，
// 取不到数就不显示假的数字，但页面结构保持不变。
func returnStatusCounters(counts map[string]int64, filter returnFilter, projectID string) []gin.H {
	var all int64
	for _, n := range counts {
		all += n
	}
	out := make([]gin.H, 0, len(returnStatusViews)+1)
	out = append(out, returnStatusCounter("", "全部", "badge-mute", false, all, filter, projectID))
	for _, view := range returnStatusViews {
		out = append(out, returnStatusCounter(view.Value, view.Label, view.Badge, view.Highlight,
			counts[view.Value], filter, projectID))
	}
	return out
}

// returnStatusCounter 单个计数项（带筛选链接；点「全部」即清掉 status 维度）。
func returnStatusCounter(value, label, badge string, highlight bool, count int64, filter returnFilter, projectID string) gin.H {
	vals := returnFilterValues(projectID, filter)
	if value == "" {
		delete(vals, "status")
	} else {
		vals["status"] = value
	}
	return gin.H{
		"Value": value, "Label": label, "Badge": badge, "Count": count, "Highlight": highlight,
		"URL":    filterBaseURL("/admin/returns", vals),
		"Active": filter.Status == value,
	}
}

// returnStatusOptions 状态下拉（筛选用：全部 + 六个状态）。
func returnStatusOptions() []gin.H {
	options := make([]gin.H, 0, len(returnStatusViews))
	for _, view := range returnStatusViews {
		options = append(options, gin.H{"Value": view.Value, "Label": view.Label})
	}
	return options
}

// returnFilterValues 列表页链接要保留的筛选条件（空值由 filterBaseURL 丢弃）。
//
// 这里**不含 returnId**：点状态、翻页这些动作的语义是「换一批单看」，
// 顺手把详情展开态带过去只会让人以为页面坏了。展开态由行内链接单独加。
func returnFilterValues(projectID string, filter returnFilter) map[string]string {
	vals := map[string]string{
		"project": projectID,
		"status":  filter.Status,
		"keyword": filter.Keyword,
	}
	if filter.OrderID > 0 {
		vals["orderId"] = strconv.FormatUint(filter.OrderID, 10)
	}
	return vals
}

// returnPendingHint 顶部待办提示：把「有人在等」的两类数字单独拎出来说一句。
// 两类都为 0 时返回空串，模板据此不渲染。
func returnPendingHint(counters []gin.H) string {
	var pending, refund int64
	for _, item := range counters {
		switch item["Value"] {
		case returnStatusRequested:
			pending, _ = item["Count"].(int64)
		case returnStatusReceived:
			refund, _ = item["Count"].(int64)
		}
	}
	if pending == 0 && refund == 0 {
		return ""
	}
	return fmt.Sprintf("当前有 %d 单待审核、%d 单已入库待退款 —— 这两个状态是有人在等着处理的。", pending, refund)
}

// —— 表单与文案工具 ——

// returnRedirect 回列表页并把结论经查询参数回显（错误 ?err=、成功 ?ok=）。
//
// 表单里的隐藏域带回当前筛选、展开中的退货单与分页窗口：一次审核之后运营看到的是
// 「同一张单的新状态」，而不是被弹回未筛选的列表第一页再去重新找一遍。
//
// ok / err 一律以本次操作的结论为准（先 Del 再 Set），不采信客户端塞进来的提示。
func returnRedirect(c *gin.Context, okText, errText string) {
	q := url.Values{}
	for _, key := range []string{"project", "status", "keyword", "orderId", "returnId", "page", "limit"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	q.Del("ok")
	q.Del("err")
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	c.Redirect(http.StatusFound, "/admin/returns?"+q.Encode())
}

// returnFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「退货数量超过可退数量」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此先放行模块自己声明的
// orderenums.UserFacingMessages（orderFacingText），再放行本页登记的后台审核侧文案
// （returnFacingExtras），其余一律落到统一提示。
func returnFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := returnFacingText(err.Error()); msg != "" {
		return msg
	}
	return orderInternalText(c)
}

// returnFacingText 白名单校验：命中返回原文，未命中返回空串。
func returnFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range returnFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// returnFacingQueryText 成功提示的回显（?ok=）：同样过白名单，未命中落空串 ——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func returnFacingQueryText(c *gin.Context, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return returnFacingText(raw)
}

// returnFormBool 表单里的布尔勾选（checkbox 勾上才提交值，没勾就整键缺失）。
func returnFormBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// returnStatusBadge 状态 → 徽章样式（认不出的状态给中性徽章，不猜颜色）。
func returnStatusBadge(status string) string {
	for _, view := range returnStatusViews {
		if view.Value == status {
			return view.Badge
		}
	}
	return "badge-mute"
}

// returnStatusLabel 状态 → 中文标签（筛选项回显用；未知值原样返回）。
func returnStatusLabel(status string) string {
	for _, view := range returnStatusViews {
		if view.Value == status {
			return view.Label
		}
	}
	if strings.TrimSpace(status) == "" {
		return "全部"
	}
	return status
}

// returnStatusNote 当前状态该做什么 / 为什么没有按钮 —— 一屏内有且只有一句说明。
func returnStatusNote(status string) string {
	switch status {
	case returnStatusRequested:
		return "客户已提交，等待审核：同意后可以勾「立即完成入库 + 退款」（货已经在手上时），也可以等货到仓库再点确认收货。"
	case returnStatusApproved:
		return "已同意，等待收货：货到仓库后点下面的「确认收货并退货」—— 它会先入库、入库成功后立刻退款。"
	case returnStatusReceived:
		return "货已入库，但退款没做完：点下面的「补退款」重试退款即可（入库那一步已完成，不会重复加库存）。"
	case returnStatusCompleted:
		return "这单已完成：货已入库、款已退。没有可执行的操作。"
	case returnStatusRejected:
		return "这单已被拒绝（终态）。客户如有异议，需要他重新提交退货申请。"
	case returnStatusCancelled:
		return "客户已撤销这单申请（终态）。没有可执行的操作。"
	default:
		return ""
	}
}

// returnOrderIDText orderId 的展示文本：0 → 空串（模板据此决定要不要显示筛选提示）。
func returnOrderIDText(orderID uint64) string {
	if orderID == 0 {
		return ""
	}
	return strconv.FormatUint(orderID, 10)
}
