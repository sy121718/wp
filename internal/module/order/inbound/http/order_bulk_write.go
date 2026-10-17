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
// （internal/templates/static/js/ui/json_write.js），四个模块各给一套写法就等于让基座
// 维护四份协议。
type orderWriteBulkItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

// orderWriteBulkResult 批量写的整体回执（计数给模板显示"已改 N 条 / 跳过 M 条"）。
type orderWriteBulkResult struct {
	Updated int                  `json:"updated"`
	Skipped int                  `json:"skipped"`
	Items   []orderWriteBulkItem `json:"items"`
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
	res := orderWriteBulkResult{Items: make([]orderWriteBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderWriteBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			// 形状先行：这一条压根不是 id，没有库也能判出来。
			item.Message = orderWriteParamText(c)
		default:
			err := h.svc.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
				OrderID:  id,
				ToStatus: toStatus,
				Remark:   remark,
				// 操作人由会话覆盖写入，绝不受请求体影响。
				OperatorType: orderOperatorTypeAdmin,
				OperatorID:   operatorID,
				OperatorName: operatorName,
			})
			if err != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(err, "批量流转订单失败")
				item.Message = orderReasonText(c, err)
			} else {
				item.Updated = true
			}
		}
		if item.Updated {
			res.Updated++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
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
	res := orderWriteBulkResult{Items: make([]orderWriteBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderWriteBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			item.Message = orderWriteParamText(c)
		default:
			// 取消只有「成功 / 失败」两种结果（归还库存与释放券核销同事务，失败即回滚，
			// 不存在「状态已取消、库存没回来」这种带警告的成功）。
			if _, err := h.svc.CancelOrder(ctx, &orderdto.CancelOrderReq{
				OrderID:      id,
				Reason:       reason,
				OperatorType: orderOperatorTypeAdmin,
				OperatorID:   operatorID,
				OperatorName: operatorName,
			}); err != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(err, "批量取消订单失败")
				item.Message = orderReasonText(c, err)
			} else {
				item.Updated = true
			}
		}
		if item.Updated {
			res.Updated++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}
