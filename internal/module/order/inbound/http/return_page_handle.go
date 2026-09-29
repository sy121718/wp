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
	// returnOperatorTypeAdmin 后台操作人类型（表单不传，由 handler 覆盖写入）。
	returnOperatorTypeAdmin = "admin"
)

// 退货页的展示标签（key + 中文兜底，调用点 tr(key, fallback) 取词）。
var (
	// returnPageTitleLabel 页面标题（sys_i18n 已有 admin.returns.heading）。
	returnPageTitleLabel = orderLabel{"admin.returns.heading", "退货入库"}
	// returnIDInvalidLabel 表单里的退货单 id 不合法。
	//
	// 它走 ?err= 回显：写侧塞的是 **fallback（中文兜底）**，不是当前语言译文 ——
	// 读侧白名单认的是这条中文串，取词后的英文译文会被自己吞掉（通道本身的改造
	// 由共享辅助统一做）。key 已备好，通道改造后调用点换成取词即可。
	returnIDInvalidLabel = orderLabel{"admin.returns.form.invalid_id", "退货单编号不合法，请回到列表页重新操作。"}
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

// returnStatusViews 退货状态 → 计数条 / 下拉用的徽章样式。
//
// **只有「状态值 → 样式」**：文案由 returnStatusText 从真源（orderenums.ReturnStatusLabel）
// 取词 —— 展示表里再存一份中文，改一处另一处必然静默漂移（本页此前的 Label 就是这么来的）。
//
// Highlight 标记「有人等着处理」的状态：待审核（要审）与待退款（要补退款），
// 这两个数字在计数条上加粗，运营一眼能看到积压。
var returnStatusViews = []struct {
	Value     string
	Badge     string
	Highlight bool
}{
	{returnStatusRequested, "badge-warning", true},
	{returnStatusApproved, "badge-warning", false},
	{returnStatusReceived, "badge-warning", true},
	{returnStatusCompleted, "badge-success", false},
	{returnStatusRejected, "badge-mute", false},
	{returnStatusCancelled, "badge-mute", false},
}

// returnFacingExtras 本页允许原样显示的**模块外**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），
// 后台审核侧的文案不在其中：同意 / 拒绝 / 入库成功的提示，以及几条只在后台出现的
// 状态错误 —— 不补这一层，运营点下去只会看到「系统内部错误」，
// 而真正的原因（「该申请不在待审核状态」）就丢了。
//
// ErrReturnNotFound 是「?returnId= 指向的单不存在」这一档的唯一文案：
// 它不在 UserFacingMessages 里（那条白名单服务访客侧结算），于是本页此前把它收口成
// 「系统内部错误，请稍后重试」—— 有反馈，但说的不是真实原因（单不存在 ≠ 系统故障）。
// 出口按当前语言取词（returnFacingPageText），页面因此显示「退货申请不存在」。
//
// 最后一条是本页自造文案：?err= 是用户可编辑的查询参数，回显时同样要过白名单，
// 自造文案不登记在这里就等着被自己吞掉。
var returnFacingExtras = []string{
	orderenums.MsgReturnApproved, orderenums.MsgReturnRejected, orderenums.MsgReturnReceived,
	orderenums.ErrReturnRejectReasonRequired, orderenums.ErrReturnNotReviewable, orderenums.ErrReturnNotReceivable,
	orderenums.ErrReturnNotFound,
	returnIDInvalidLabel.fallback,
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

	// 回显文案：?err= / ?ok= 都过白名单，查不到的一律收口
	// （查询参数是用户可编辑的，不能拿它当「业务提示」直接显示）。
	// 命中白名单的那一支要取当前语言的译文（returnFacingPageText）：页面上的
	// {{.Err}} / {{.Ok}} 是直接渲染的文本，不经过 pkg/response 的 translate。
	// 先于装载计算：装载失败要**压过**它（见下）。
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), returnFacingPageText(c))
	pageOk := returnFacingQueryText(c, c.Query("ok"))
	// 展示标签的取词函数（视图组装只用它，不再在 Go 里写死中文标签）。
	tr := shell.TranslateFor(c)

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据见 order_page_handle.go 的 OrdersPage）：
	// 空列表 + 归口提示 + HTTP 200，页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
	// 装载失败**压过 ?err=**：它是这次请求真实发生的事。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = orderFacingError(c, loadErr)
	}

	// 装载失败时不再去读列表 / 详情：工程上下文都没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查退货单，查出来的是哪个工程的单都说不清。
	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}
	page, limit := orderListWindow(c)
	filter := returnFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		OrderID:  orderQueryID(c.Query("orderId")),
		ReturnID: orderQueryID(c.Query("returnId")),
	}

	rows := []gin.H{}
	counters := returnStatusCounters(tr, nil, filter, selected)
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
			counters = returnStatusCounters(tr, list.Counts, filter, selected)
			for _, r := range list.List {
				rows = append(rows, returnListRow(tr, r, filter, selected, page, limit))
			}
		}
		if filter.ReturnID > 0 {
			det, derr := h.orders.GetReturn(ctx, filter.ReturnID)
			if derr != nil {
				pageErr = firstNonEmpty(pageErr, returnFacingError(c, derr))
			} else {
				detail = returnDetailView(tr, det, filter, selected, page, limit)
			}
		}
	}

	data := shell.Prepare(c, gin.H{
		"title":           orderLabelOf(tr, returnPageTitleLabel),
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		"Statuses":        counters,
		// 状态维度**只有**计数徽章一种入口（徽章行已经能点，再摆一个同维度的下拉，
		// 用户会怀疑两者是否等价，见 admin-ui-logic §3）。
		"FilterStatus":  filter.Status,
		"FilterLabel":   returnStatusLabel(tr, filter.Status),
		"FilterKeyword": filter.Keyword,
		"FilterOrderID": returnOrderIDText(filter.OrderID),
		"ClearOrderURL": shell.FilterBaseURL("/admin/returns", returnFilterValues(selected, returnFilter{Status: filter.Status, Keyword: filter.Keyword})),
		"PendingHint":   returnPendingHint(tr, counters),
		"Rows":          rows,
		"Total":         total,
		"Warehouses":    h.warehouseOptions(ctx, selected, tr),
		"Detail":        detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这个键决定要不要渲染详情块。
		"HasDetail": len(detail) > 0,
		// 同上：装载失败时空态必须与「这个工程还没有退货申请」区分开，判据由 handler 算好。
		"LoadFailed": loadFailed,
		"Page":       page,
		"Limit":      limit,
		"Err":        pageErr,
		"Ok":         pageOk,
		// 批量动作的结论：数量是动态的，过不了 ?ok= / ?err= 的文案白名单，单独走 ?done=。
		"Done": orderPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/returns", returnFilterValues(selected, filter))
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/order/returns.html", data)
}

// ReturnApprove 同意退货申请（POST /admin/returns/approve）。
//
// autoReceive 勾上时服务端会在同一次调用里完成「入库 + 退款」（一步到底）；
// 不勾则停在已同意待收货，等仓库点货后再走 ReturnReceive。
func (h *returnPageHandle) ReturnApprove(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		returnRedirect(c, "", returnIDInvalidLabel.fallback)
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
		returnRedirect(c, "", returnIDInvalidLabel.fallback)
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
		returnRedirect(c, "", returnIDInvalidLabel.fallback)
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
		returnRedirect(c, "", orderBulkIDsText(c, berr))
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
	returnBulkRedirect(c, bulkSummary(c, orderBulkVerbApproved, orderBulkNounReturn, approved, skipped))
}

// ReturnBulkReject 批量拒绝退货申请（POST /admin/returns/bulk-reject）。
//
// 理由**必填**（与单条一致）：客户要知道为什么，否则他会再申请一次。缺理由时整批不处理
// 并原样回带提示 —— 静默跳过会让人以为「选中的单都审不了」。
func (h *returnPageHandle) ReturnBulkReject(c *gin.Context) {
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		returnBulkRedirect(c, orderBulkTextOf(c, returnBulkRejectReasonRequired))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		returnRedirect(c, "", orderBulkIDsText(c, berr))
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
	returnBulkRedirect(c, bulkSummary(c, orderBulkVerbRejected, orderBulkNounReturn, rejected, skipped))
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
