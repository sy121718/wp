package orderhttp

// order_bulk_write.go — 订单列表页两个**批量写**动作（状态流转 / 批量取消）的 JSON 形状。
//
// 本批把订单列表页的六个页面写动作迁到真实 JSON 端点（task-98 S2）。其中四个单条动作
// （流转 / 取消 / 退款 / 备注）直调既有 `/api/order/*`，单档语义逐字不变；另外两个批量动作
// 不再另开路径 —— 判别键是**形状**：请求体里有 `ids` 就按批档处理，否则按单档。
//
// 为什么不新开 /api/order/bulk-status：`sys_permission` 每个权限点**只有一列**
// api_path / api_method（internal/permission/codes.go:43），再开一条路径就必须新增权限点，
// 而「新增权限点 = 已授权角色静默缩权」是本仓有案可查的事故（见 order_write.go 头注释）。
//
// 页面侧那套「受控结论文案 + 数字归一比对」不跟着迁移：结论文案不再由 Go 拼整句（那是提示页
// 的形态），改由模板词条 + 计数承载；服务端交出去的是**事实**：`updated` / `skipped` /
// `items[].message`。逐条原因走 orderReasonText（白名单词条 → 当前语言；缺词 → 中文原文；
// 未命中 → 记结构化日志 + 归口文案），与单档失败共用同一个取词层，因此接口与页面上
// 对同一条错误的说法不会分叉。

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// 批档顶层白名单（形状判据：只认这几把键，多一个都 400）。
//
// 单条流转的 `orderId` 刻意不在批档白名单里：`{"orderId":1,"ids":["2"]}` 这种混形如果按
// 某一边"猜"着执行，被改掉的可能恰恰是调用方以为不会动的那一批。
var (
	orderStatusBulkKeys = map[string]bool{"ids": true, "toStatus": true, "remark": true}
	orderCancelBulkKeys = map[string]bool{"ids": true, "remark": true}
)

// orderWriteBulkItem 批量写的一条结果：id 原样回带 + 是否成功 + 失败原因。
//
// 字段名与 mail / navigation / 优惠码同族（`id` / `updated` / `message`）：前端基座只有一个
// （internal/templates/static/js/ui/write-base.js），四个模块各给一套写法就等于让基座
// 维护四份协议。
type orderWriteBulkItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

// orderWriteBulkResult 批量写的整体回执（**只有成功态**）。
//
// 没有 skipped：批量写要么全成、要么全败（AGENTS.md §写操作）。失败走
// ErrorWithData + data.items，逐条给 id / 名称 / 原因；成功时所有条目都改成功了，
// 一个计数就够，逐条回执没有信息量。
type orderWriteBulkResult struct {
	Updated int `json:"updated"`
}

// orderWriteStringOf 取一个 JSON **字符串**字段（缺键 = 零值 + true）。
//
// 显式 null 与非字符串（数字 / 布尔 / 对象 / 数组）一律 false：`{"toStatus":null}` 落进
// string 不报错、静默变成空串，于是整批被判「非法目标状态」，调用方看到的是 N 条业务失败
// 而不是一次形状错 —— 猜类型会让错误落到离真相最远的地方。
func orderWriteStringOf(raw map[string]json.RawMessage, key string) (string, bool) {
	body, ok := raw[key]
	if !ok {
		return "", true
	}
	if orderWriteIsExplicitNull(body) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// orderWriteBulkShapeOK 批档形状校验（白名单 + 严格 id 列表；形状错直接 400）。
//
// 空数组同样是形状错（orderWriteIDsOf 的既有口径）：浏览器侧 `data-msg-none-selected`
// 已经拦掉"什么都没勾"，走到服务端只能是请求拼错了；放行会得到「200 + 成功 0 项」
// 这种看着像成功的空动作。
func orderWriteBulkShapeOK(c *gin.Context, raw map[string]json.RawMessage, allow map[string]bool) ([]string, bool) {
	if !orderWriteShapeOK(raw, allow) {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	return orderWriteNormalizeIDs(c, raw)
}

// orderStatusBulk 批档状态流转（POST /api/order/status 的批档形状）。
//
// 逐条走与单条**同一个** service 方法：状态机不允许那条边的订单（或已不存在的单）只跳过它、
// 继续处理其余 —— 批量操作不能因为一条非法边就整批停下，否则用户会以为「一条都没做」然后
// 反复重试（重试又会在别的单上撞同一堵墙）。失败只计入 skipped 并回带原因。
//
// `toStatus` 是**字符串**（paid / shipped / completed），服务端严格认它；它在批档里不是
// 数值字段，因此模板不给它 data-write-number。
//
// 形状档与业务档必须分开（这是本批的一处口径修正）：
//
//	· **形状档**：缺 `toStatus` / 空串 / 全空白 / 显式 null / 类型不是字符串 —— 都是「客户端
//	  漏字段或发错类型」，一律 400 且**零 service 调用**（用例用 nil service 钉住：真调了就会 panic）；
//	· **业务档**：目标状态合法但订单自己不可流转（状态机不允许这条边、已发货、单不存在）——
//	  200 + 逐条 skipped + 原因，批量不因一条非法边整批停下。
//
// 为什么不能把空串丢给 service 逐条裁决（页面路径的老语义）：那会把**客户端漏字段伪装成成功** ——
// 回执「0 个已流转、N 个被跳过」，用户看到「一条都没做」，与真正的业务不可流转混成同一句话，
// 排查方向从一开始就错了（实测：模板漏声明 data-write-extra 时就是这个现象）。
// 契约是「按形状认档、形状错 400」，与 S1 券批档同口径。
func (h *Handle) orderStatusBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, orderStatusBulkKeys)
	if !ok {
		return
	}
	toStatus, ok := orderWriteStringOf(raw, "toStatus")
	if !ok || strings.TrimSpace(toStatus) == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	remark, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	// **整批预检**（B06）：先把「不是合法 id」的条目一次找全，整批拒绝。
	// 形状错不需要碰库，所以放在最前面 —— 它也是最常见的一类（表单里混进空串）。
	blocked := orderBulkShapeBlocked(c, ids)
	if len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}

	// 逐条执行，但**任一条失败即整批放弃并回滚已完成的部分**。
	//
	// 为什么不能继续用"updated / skipped 计数"：用户拿到「成功 49 / 跳过 1」时
	// 既不知道是哪一条、也没法只重试那一条（AGENTS.md §写操作的批量判据）。
	// **整批同一事务**（ChangeStatusBatch）：而不是这里循环调 ChangeStatus。
	// 每条各开一个事务时，第 N 条失败前 N-1 条已经提交 —— "整批被拒"就成了一句假话：
	// 用户看到拒绝，库里却已经改了前几条。
	reqs := make([]*orderdto.ChangeStatusReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ChangeStatusReq{
			OrderID:  id,
			ToStatus: toStatus,
			Remark:   remark,
			// 操作人由会话覆盖写入，绝不受请求体影响。
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.ChangeStatusBatch(ctx, reqs); err != nil {
		logger.Scene(orderWriteScene).Error(err, "批量流转订单失败")
		// 事务已整体回滚，所以这里只需指出**挡住整批的那一条**——
		// 不存在"已执行需要复核"的条目（这正是改用单事务的收益）。
		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}

// orderCancelBulk 批档取消（POST /api/order/cancel 的批档形状）：服务端会归还尚未发货那部分库存。
//
// 取消原因**必填**，缺原因整批 400（不逐条跳过）：逐条取消却没有原因，事后在状态流转链上
// 会看到一串没有理由的「已取消」，那是审计上查不清的状态。这条判据原先在页面 handler 里，
// 随迁移搬到 JSON 出口；字段名仍是 `remark`（批量表单里那个备注框的 name，取消时它就是
// 取消原因；单档走 `reason`，与详情页表单的 name 一致）。
func (h *Handle) orderCancelBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, orderCancelBulkKeys)
	if !ok {
		return
	}
	reason, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if reason == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.BulkCancelReasonRequired)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	blocked := orderBulkShapeBlocked(c, ids)
	if len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}

	// 同流转批档：**整批同一事务**（CancelOrderBatch）。
	reqs := make([]*orderdto.CancelOrderReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.CancelOrderReq{
			OrderID:      id,
			Reason:       reason,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.CancelOrderBatch(ctx, reqs); err != nil {
		logger.Scene(orderWriteScene).Error(err, "批量取消订单失败")
		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}

// orderBulkShapeBlocked 找出「不是合法 id」的条目（形状错，不需要碰库）。
//
// 判据先行：空串与非法数字是批量表单最常见的一类输入错（前端把空行也提交了）。
// 它们不需要问数据库就能判定，所以整批预检的第一步就是它。
func orderBulkShapeBlocked(c *gin.Context, ids []string) []facing.BlockedItem {
	out := make([]facing.BlockedItem, 0)
	for _, rawID := range ids {
		id, perr := strconv.ParseUint(rawID, 10, 64)
		if perr != nil || id == 0 {
			out = append(out, facing.BlockedItem{
				ID: rawID,
				// 原因走模块自己的取词层（与单档失败同一份），接口与页面的说法不会分叉。
				ReasonKey: orderenums.ErrInvalidParam,
				Fallback:  orderenums.FallbackFor(orderenums.ErrInvalidParam),
			})
		}
	}
	return out
}

// orderBulkReasonBlocked 一条可定位的业务拒绝（id + 已经取好词的原因）。
//
// 与 orderBulkShapeBlocked 的差别：这里的文案已经过 orderReasonText（词条 → 当前语言），
// 所以走 Fallback 而不是 ReasonKey —— 再翻一次会把整句话当成 key。
func orderBulkReasonBlocked(rawID, message string) []facing.BlockedItem {
	return []facing.BlockedItem{{
		ID:       rawID,
		Fallback: message,
		// ReasonKey 留空：出口见空 key 时直接用 Fallback（不再查词条）。
		ReasonKey: message,
	}}
}

// orderBulkReject 整批被拒的统一出口：409 + data.items（id / 名称 / 原因）。
//
// 用 409 而不是 400：其中可能包含"业务上此刻不能改"（状态不允许、并发已变更），
// 那不是参数错，用户改的是时机而不是输入。
func orderBulkReject(c *gin.Context, items []facing.BlockedItem) {
	// 形状与 product 的批量拒绝一致（data.items[].id / name / reasonKey / message）：
	// 前端基座只有一个，四个模块各给一套写法就等于让它维护四份协议。
	//
	// 这里手工摊平成小写键，而不是直接塞 facing.BlockedItem —— 那个类型没有 json tag
	//（它首先是 pkg/facing 的接口形状，不是传输契约）。摊平之后 message 已按当前语言取词。
	rows := make([]gin.H, 0, len(items))
	for _, it := range items {
		rows = append(rows, gin.H{
			"id":      it.ID,
			"name":    it.Name,
			"message": orderBulkItemText(c, it),
		})
	}
	response.ErrorWithData(c, http.StatusConflict, orderenums.ErrBulkRejected, gin.H{"items": rows})
}

// orderBulkItemText 一条拒绝明细的对外文案。
//
// ReasonKey 可能是模块词条 key（要按当前语言取词），也可能是调用方**已经取好词**的整句
// （orderBulkReasonBlocked 传的就是 orderReasonText 的结果）。用 TranslateMessage 区分：
// 命中词条就用译文，没命中说明它本来就是一句可读的话，原样交出。
func orderBulkItemText(c *gin.Context, it facing.BlockedItem) string {
	key := strings.TrimSpace(it.ReasonKey)
	if key != "" {
		if text := response.TranslateMessage(c, key); text != key {
			return text
		}
		return key
	}
	return it.Fallback
}
