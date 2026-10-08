package orderhttp

// return_page.go — 退货入库页（列表 / 详情 / 审核 / 收货 / 补退款 / 批量）的控制器。
//
// 分工与订单页、优惠码页一致（样板见 coupon_page.go）：控制器只做「绑定 → 调 service →
// 渲染」，文案 / 格式化 / 显示判断在模板里，dto 直接交给模板。
//
// 流程只有一条（页面上的说明也这么写）：**客户申请 → 审核 → 先入库、后退款**。
// 顺序不能反 —— 反过来就是「钱退了、货没回来」。所以这个页面没有「只退款」的按钮，
// 入库与退款由服务端在同一条用例里按顺序完成（幂等可重入：status=received 时再点
// 只会补做退款，不会重复加库存）。
//
// 显示投影（模板按它们决定渲染哪些表单，服务端仍是唯一裁决者）：
//
//   - 待审核：可「同意」（可勾 autoReceive 一步到底）或「拒绝」（理由必填）；
//   - 已同意待收货：可「确认收货并退货」；
//   - 已入库待退款：可「补退款」（同一个入口，服务端识别出入库已完成）；
//   - 其余（已完成 / 已拒绝 / 已撤销）：终态，没有可执行的操作。

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/order/enums"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
)

// returnPageHandle 退货入库页处理器。
type returnPageHandle struct {
	orders     ordercontract.OrderService
	projects   projectcontract.ProjectService
	warehouses ordercontract.ReturnWarehouseSource
	dict       sysconfigcontract.DictReader
}

// NewReturnPageHandle 构造。
func NewReturnPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	warehouses ordercontract.ReturnWarehouseSource, dict sysconfigcontract.DictReader) *returnPageHandle {
	return &returnPageHandle{orders: orders, projects: projects, warehouses: warehouses, dict: dict}
}

// returnFilter 页面筛选条件（GET 参数，全部可选）。
type returnFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（退货单号 / 订单号 / 客户邮箱，服务端决定匹配哪些列）。
	Keyword string
	// OrderID 非 0 时只列该订单的退货申请（从订单详情跳过来时带）。
	OrderID uint64
	// ReturnID 非 0 时展开该退货单的详情（同一个页面靠查询参数切换，不新开路由）。
	ReturnID uint64
}

// returnStatusValues 状态维度的取值白名单（含空串 = 「全部」）。
//
// 顺序 = 徽章行里的顺序；文案由模板经 StatusLabel 取词（真源是
// orderenums.ReturnStatusLabel 的 `admin.returns.status.*` 词条）。
var returnStatusValues = []string{
	"", "requested", "approved", "received", "completed", "rejected", "cancelled",
}

// returnListQueryKeys 列表上下文的白名单键（渲染时拼 action / POST 回来时读回，两处同一份）。
//
// **含 returnId**：审核完停在原来那张单的详情上是用户要的（状态刚变，正好看结果）；
// 而状态徽章与翻页链接用不含 returnId 的基础串（「换一批单看」时把展开态带过去只会让人以为页面坏了）。
var returnListQueryKeys = []string{"project", "keyword", "orderId", "page", "limit", "returnId"}

// ReturnsPage 退货入库页（GET /admin/returns）。
func (h *returnPageHandle) ReturnsPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
	// 页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	selected := ""
	if loadErr == nil {
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

	var returns []*orderdto.ReturnResp
	// 计数表**一开始就补齐零值**：装载失败 / 未查询时它是 nil，而模板会拿计数做数值比较
	// （`{{if pendingCount > 0}}`），nil 索引出来的值参与比较会让 Jet 报错、整页 500 ——
	// 「这一页没读出来」的降级路径反而打不开页面，是最不该发生的一种 500。
	counts := countsFilled(nil, returnStatusValues)
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
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, lerr))
		} else {
			returns, counts, total = list.List, countsFilled(list.Counts, returnStatusValues), list.Total
		}
	}

	var detail *orderdto.ReturnDetailResp
	if selected != "" && filter.ReturnID > 0 {
		det, derr := h.orders.GetReturn(ctx, filter.ReturnID)
		if derr != nil {
			loadErrText = firstNonEmpty(loadErrText, returnFacingError(c, derr))
		} else {
			detail = det
		}
	}

	data := shell.Prepare(c, gin.H{
		"title":           "admin.returns.heading",
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		// 取数结果：dto 原样交给模板。
		"Returns": returns,
		"Counts":  counts,
		"Total":   total,
		// 「全部」徽章的计数（各状态之和）：模板里跨类型累加（int + int64）不值得押注，
		// 求和放这里一行，模板只取结果。
		"CountsAll": countsAll(counts),
		"Detail":    detail,
		// 状态徽章行的取值白名单（含空串 = 全部）+ 当前生效值。
		"StatusValues":  returnStatusValues,
		"FilterStatus":  filter.Status,
		"FilterKeyword": filter.Keyword,
		"FilterOrderID": filter.OrderID,
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有退货申请」分开。
		"LoadErr": loadErrText,
		// 列表上下文（拼进表单 action / 徽章链接；不含 status，见 returnListQueryKeys）。
		"ListQuery": returnListQuery(selected, filter, page, limit),
		// 入库仓下拉：只给启用的仓（停用的仓历史流水仍按 id 可读，但新入库不该再选它）。
		"Warehouses": h.enabledWarehouses(ctx, selected),
		// 状态 → 当前语言标签。enums 的 (key, fallback) 是两返回值，模板接不住，
		// 所以这里包一层单值函数进数据：**取词仍在模板调用点发生**，Go 不预先拼行视图。
		"StatusLabel": returnStatusLabelFunc(c),
		// 订单摘要里的订单状态标签（与订单页同一份真源 orderenums.OrderStatusLabel）。
		"OrderStatusLabel": orderStatusLabelFunc(c),
		"Page":             page,
		"Limit":            limit,
	})
	base := shell.WithParams("/admin/returns", map[string]string{
		"project": selected, "status": filter.Status, "keyword": filter.Keyword,
	})
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/order/returns.html", data)
}

// —— 写动作 ——

// ReturnApprove 同意退货申请（POST /admin/returns/approve）。
//
// autoReceive 勾上时服务端会在同一次调用里完成「入库 + 退款」（一步到底）；
// 不勾则停在已同意待收货，等仓库点货后再走 ReturnReceive。
func (h *returnPageHandle) ReturnApprove(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		h.returnFail(c, returnFacing(c, returnIDInvalidLabel))
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
		h.returnFail(c, returnFacingError(c, err))
		return
	}
	// 勾了 autoReceive 的单会直接落到「已入库待退款」甚至「已完成」，
	// 提示语跟着实际落地状态走，否则运营会以为只批了同意、又去点一次收货。
	ok := orderenums.MsgReturnApproved
	if res != nil && res.Status != "approved" {
		ok = orderenums.MsgReturnReceived
	}
	h.returnDone(c, ok)
}

// ReturnReject 拒绝退货申请（POST /admin/returns/reject）：**必须给理由** ——
// 客户要知道为什么，否则他会再申请一次。
func (h *returnPageHandle) ReturnReject(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		h.returnFail(c, returnFacing(c, returnIDInvalidLabel))
		return
	}
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		h.returnFail(c, returnFacingKey(c, orderenums.ErrReturnRejectReasonRequired))
		return
	}
	if _, err := h.orders.RejectReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
		ReturnID:     returnID,
		Remark:       remark,
		OperatorType: returnOperatorTypeAdmin,
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}); err != nil {
		h.returnFail(c, returnFacingError(c, err))
		return
	}
	h.returnDone(c, orderenums.MsgReturnRejected)
}

// ReturnReceive 确认收货（POST /admin/returns/receive）：先入库、再退款。
//
// 这个入口同时承担两件事，因为服务端把它们做成了幂等且可重入的同一条用例：
//
//   - status=approved：货到仓库了，点这一下 —— 入库（加回库存）成功后立刻退款；
//   - status=received：上一次入库成功但退款没做完（通道抖动 / 网络断了），
//     再点一次只会补做退款 —— 入库那一步服务端会识别出已完成，不会重复加库存。
func (h *returnPageHandle) ReturnReceive(c *gin.Context) {
	returnID := orderQueryID(c.PostForm("returnId"))
	if returnID == 0 {
		h.returnFail(c, returnFacing(c, returnIDInvalidLabel))
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
		h.returnFail(c, returnFacingError(c, err))
		return
	}
	h.returnDone(c, orderenums.MsgReturnReceived)
}

// ReturnBulkApprove 批量同意退货申请（POST /admin/returns/bulk-approve）。
//
// 逐条走**同一条单条审核路径**：不在「待审核」的单（已被别人审过、已撤销）由服务端拒绝，
// 只跳过它并计入跳过数，其余照常同意 —— 一条不合规的申请不该让整批停下。
//
// **不批量自动入库**：autoReceive 会把「同意 → 入库 → 退款」一步做完，批量点一下等于
// 对一批单同时加库存与放款，那不是批量操作该承担的确认强度。
func (h *returnPageHandle) ReturnBulkApprove(c *gin.Context) {
	remark := strings.TrimSpace(c.PostForm("remark"))
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.returnFail(c, shell.BulkIDsFacingText(c, berr))
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
	h.returnDone(c, bulkSummary(c, orderBulkVerbApproved, orderBulkNounReturn, approved, skipped))
}

// ReturnBulkReject 批量拒绝退货申请（POST /admin/returns/bulk-reject）：理由必填。
func (h *returnPageHandle) ReturnBulkReject(c *gin.Context) {
	remark := strings.TrimSpace(c.PostForm("remark"))
	if remark == "" {
		h.returnFail(c, orderBulkTextOf(c, returnBulkRejectReasonRequired))
		return
	}
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.returnFail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	rejected, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		if _, err := h.orders.RejectReturn(c.Request.Context(), &orderdto.ReturnReviewReq{
			ReturnID:     id,
			Remark:       remark,
			OperatorType: returnOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		}); err != nil {
			skipped++
			continue
		}
		rejected++
	}
	h.returnDone(c, bulkSummary(c, orderBulkVerbRejected, orderBulkNounReturn, rejected, skipped))
}

// —— 出口与请求工具 ——

// returnDone 写成功：提示页（htmx 档由 RenderJump 换成 HX-Redirect）。
func (h *returnPageHandle) returnDone(c *gin.Context, msg string) {
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      msg,
		Back:     shell.BackPath(c, "/admin/returns", returnListQueryKeys...),
		BackText: shell.TranslateFor(c)("admin.returns.heading", "退货入库"),
		Seconds:  1,
	})
}

// returnFail 写失败：提示页，不自动跳转（用户要看清原因）。
func (h *returnPageHandle) returnFail(c *gin.Context, msg string) {
	shell.RenderJump(c, shell.Jump{
		OK:       false,
		Msg:      msg,
		Back:     shell.BackPath(c, "/admin/returns", returnListQueryKeys...),
		BackText: shell.TranslateFor(c)("admin.returns.heading", "退货入库"),
	})
}

// returnListQuery 列表上下文 → 查询串（拼进表单 action / 徽章链接；空值不拼）。
//
// 刻意**不含 status**：徽章链接要覆盖它、写表单要保留它（表单里那个隐藏域）。
func returnListQuery(projectID string, filter returnFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("keyword", filter.Keyword)
	if filter.OrderID > 0 {
		set("orderId", strconv.FormatUint(filter.OrderID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

// returnStatusText 状态 → 展示文案：按状态**值**取词（不拿 service 给的中文标签反查）。
//
// 两个消费方：页面（模板经 StatusLabel 函数调用）与 JSON 接口（order_handle.go 的
// localizeReturnLabels —— 接口响应里的 StatusLabel 也要按请求语言取词，否则英文站点恒中文）。
// 认不出的取值回落「—」：模板直接渲染这个值，空白单元格读不出「不知道」。
func returnStatusText(tr translate, status string) string {
	key, fallback := orderenums.ReturnStatusLabel(status)
	return orderLabelOf(tr, orderLabel{key, fallback})
}

// returnStatusLabelFunc 状态 → 当前语言标签（包一层单值函数给模板调用）。
//
// 真源是 orderenums.ReturnStatusLabel 的 (key, fallback)；模板接不住两返回值，
// 所以在这里收敛成单值。**取词发生在模板调用点**（`{{ .StatusLabel(s) }}`），
// Go 不预先给每一行拼标签。
func returnStatusLabelFunc(c *gin.Context) func(string) string {
	tr := shell.TranslateFor(c)
	return func(status string) string {
		return returnStatusText(tr, status)
	}
}

// countsAll 各状态计数之和（「全部」徽章的那个数）。
func countsAll(counts map[string]int64) int64 {
	var all int64
	for _, v := range counts {
		all += v
	}
	return all
}

// orderStatusLabelFunc 订单状态 → 当前语言标签（订单摘要里的那个徽章用）。
func orderStatusLabelFunc(c *gin.Context) func(string) string {
	tr := shell.TranslateFor(c)
	return func(status string) string {
		key, fallback := orderenums.OrderStatusLabel(status)
		return tr(key, fallback)
	}
}

// enabledWarehouses 入库仓下拉的候选：只保留启用的仓（默认仓在最前，由库存侧排序保证）。
func (h *returnPageHandle) enabledWarehouses(ctx context.Context, projectID string) []ordercontract.ReturnWarehouse {
	if h.warehouses == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	list, err := h.warehouses.ListReturnWarehouses(ctx, projectID)
	if err != nil {
		// 仓列表读不出来只降级（下拉里没有选项，留空即默认仓）：审核本身不受影响。
		return nil
	}
	out := make([]ordercontract.ReturnWarehouse, 0, len(list))
	for _, w := range list {
		if strings.TrimSpace(w.ID) == "" {
			continue
		}
		if w.Status != "" && w.Status != "enabled" {
			continue
		}
		out = append(out, w)
	}
	return out
}

// —— 退货页自有的常量与文案白名单 ——

// returnOperatorTypeAdmin 后台操作人类型（表单不传，由控制器覆盖写入）。
const returnOperatorTypeAdmin = "admin"

// 退货单编号不合法（表单里的 returnId 缺失 / 被改坏时回带的参数级提示）。
var returnIDInvalidLabel = orderLabel{"admin.returns.form.invalid_id", "退货单编号不合法，请回到列表页重新操作。"}

// returnFacingExtras 本页允许原样显示的**模块外**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），
// 后台审核侧的文案不在其中：同意 / 拒绝 / 入库成功的提示，以及几条只在后台出现的
// 状态错误 —— 不补这一层，运营点下去只会看到「系统内部错误」，
// 而真正的原因（「该申请不在待审核状态」）就丢了。
var returnFacingExtras = []string{
	orderenums.MsgReturnApproved, orderenums.MsgReturnRejected, orderenums.MsgReturnReceived,
	orderenums.ErrReturnRejectReasonRequired, orderenums.ErrReturnNotReviewable, orderenums.ErrReturnNotReceivable,
	orderenums.ErrReturnNotFound,
	returnIDInvalidLabel.fallback,
}

// returnBulkRejectReasonRequired 批量拒绝缺理由时的提示（整批不处理）。
var returnBulkRejectReasonRequired = orderBulkText{orderenums.BulkReturnRejectReasonRequired,
	"批量拒绝需要先填理由（表单里的备注框），本次没有处理任何退货申请。"}

// returnFacingError 把 service 的错误收敛成可展示文案：命中白名单取词展示，
// 未命中记结构化日志并给归口文案（原文只进日志）。
func returnFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := returnFacingText(err.Error()); msg != "" {
		return shell.TranslateFor(c)(msg, msg)
	}
	logger.Scene("order-return").With("path", c.Request.URL.Path).Error(err, "退货写操作失败")
	return shell.PageInternalText(c)
}

// returnFacingText 白名单判定：命中返回原文，未命中返回空串。
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

// returnFacingKey 本页自造文案（enums 里的 key）取词后展示。
//
// 兜底用 key 本身而不是中文：这些 key 已在 seed 里（check-i18n-keys-seeded.sh 守着），
// 真缺词条时露出的是一串 key —— 比「悄悄显示一句写死的中文」更容易被发现。
func returnFacingKey(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, key)
}

// returnFormBool 复选框的值：只认 "1" / "true" / "on"（浏览器三种写法）。
func returnFormBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// returnFacing 取词（key + 中文兜底 → 当前语言的成品文案）。
func returnFacing(c *gin.Context, l orderLabel) string {
	return orderLabelOf(shell.TranslateFor(c), l)
}
