package orderhttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/web/shell"
)

const (
	// returnPageTitle 页面标题（订单模块 enums 里没有这个标题键，直接走 shell.Prepare 的 fallback 链路）。
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
	// warehouses 只用来渲染「入库仓库」下拉：退货入库落在哪个仓是**运营的决定**，
	// 让运营填一个仓库 ID 是把内部标识当输入项 —— 填错不报错，货就进错仓了。
	warehouses ordercontract.ReturnWarehouseSource
}

// NewReturnPageHandle 构造。
func NewReturnPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	warehouses ordercontract.ReturnWarehouseSource) *returnPageHandle {
	return &returnPageHandle{orders: orders, projects: projects, warehouses: warehouses}
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
		c.String(http.StatusInternalServerError, orderenums.ErrInternal)
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
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), orderFacingText)
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

	data := shell.Prepare(c, gin.H{
		"title":           returnPageTitle,
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		"Statuses":        counters,
		// 状态维度**只有**计数徽章一种入口（徽章行已经能点，再摆一个同维度的下拉，
		// 用户会怀疑两者是否等价，见 admin-ui-logic §3）。
		"FilterStatus":  filter.Status,
		"FilterLabel":   returnStatusLabel(filter.Status),
		"FilterKeyword": filter.Keyword,
		"FilterOrderID": returnOrderIDText(filter.OrderID),
		"ClearOrderURL": shell.FilterBaseURL("/admin/returns", returnFilterValues(selected, returnFilter{Status: filter.Status, Keyword: filter.Keyword})),
		"PendingHint":   returnPendingHint(counters),
		"Rows":          rows,
		"Total":         total,
		"Warehouses":    h.warehouseOptions(ctx, selected),
		"Detail":        detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这个键决定要不要渲染详情块。
		"HasDetail": len(detail) > 0,
		"Page":      page,
		"Limit":     limit,
		"Err":       pageErr,
		"Ok":        pageOk,
		// 批量动作的结论：数量是动态的，过不了 ?ok= / ?err= 的文案白名单，单独走 ?done=。
		"Done": strings.TrimSpace(c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/returns", returnFilterValues(selected, filter))
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
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
		OperatorID:   shell.CurrentUserID(c),
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
		OperatorID:   shell.CurrentUserID(c),
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
		OperatorID:    shell.CurrentUserID(c),
		OperatorName:  builtin.GetUsername(c),
	}); err != nil {
		returnRedirect(c, "", returnFacingError(c, err))
		return
	}
	returnRedirect(c, orderenums.MsgReturnReceived, "")
}

// ReturnBulkApprove 批量同意退货申请（POST /admin/returns/bulk-approve）。
//
// 逐条走**同一条单条审核路径**：不在「待审核」的单（已被别人审过、已撤销）由服务端拒绝，
// 只跳过它并计入跳过数，其余照常同意 —— 一条不合规的申请不该让整批停下。
//
// **不批量自动入库**：autoReceive 会把「同意 → 入库 → 退款」一步做完，批量点一下等于
// 对一批单同时加库存与放款，那不是批量操作该承担的确认强度。批量只走「同意」这一步，
// 入库与退款仍按单确认。
func (h *returnPageHandle) ReturnBulkApprove(c *gin.Context) {
	remark := strings.TrimSpace(c.PostForm("remark"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		returnRedirect(c, "", berr.Error())
		return
	}
	approved, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		_, err := h.orders.ApproveReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
			ReturnID:    id,
			Remark:      remark,
			AutoReceive: false,
			// 操作人由会话覆盖写入，绝不受表单影响。
			OperatorType: returnOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if err != nil {
			skipped++
			continue
		}
		approved++
	}
	returnBulkRedirect(c, bulkSummary("已同意", "退货申请", approved, skipped))
}

// ReturnBulkReject 批量拒绝退货申请（POST /admin/returns/bulk-reject）。
//
// 理由**必填**（与单条一致）：客户要知道为什么，否则他会再申请一次。缺理由时整批不处理
// 并原样回带提示 —— 静默跳过会让人以为「选中的单都审不了」。
func (h *returnPageHandle) ReturnBulkReject(c *gin.Context) {
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		returnBulkRedirect(c, "批量拒绝需要先填理由（表单里的备注框），本次没有处理任何退货申请。")
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		returnRedirect(c, "", berr.Error())
		return
	}
	rejected, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		_, err := h.orders.RejectReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
			ReturnID:     id,
			Remark:       remark,
			OperatorType: returnOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if err != nil {
			skipped++
			continue
		}
		rejected++
	}
	returnBulkRedirect(c, bulkSummary("已拒绝", "退货申请", rejected, skipped))
}

// returnBulkRedirect 批量动作回列表页：结论走 ?done=，当前筛选与窗口原样带回。
//
// 不带 returnId：批量是列表级动作，把某一张单的展开态带回来只会让人以为批量审的是那一单。
func returnBulkRedirect(c *gin.Context, doneText string) {
	q := url.Values{}
	for _, key := range []string{"project", "status", "keyword", "orderId", "page", "limit"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	q.Del("ok")
	q.Del("err")
	if doneText != "" {
		q.Set("done", doneText)
	}
	c.Redirect(http.StatusFound, "/admin/returns?"+q.Encode())
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

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
