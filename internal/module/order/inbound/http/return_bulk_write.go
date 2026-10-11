package orderhttp

// return_bulk_write.go — 退货入库页两个**批量写**动作（批量同意 / 批量拒绝）的 JSON 形状。
//
// 本批把退货页的五个页面写动作迁到真实 JSON 端点（task-98 S3）。三个单条动作
// （同意 / 拒绝 / 确认收货）直调既有 `/api/order/return/{approve,reject,receive}`，单档语义
// 逐字不变；两个批量动作不另开路径 —— 判别键是**形状**：请求体里有 `ids` 就按批档处理。
//
// 为什么不新开 /api/order/return/bulk-*：`sys_permission` 每个权限点**只有一列**
// api_path / api_method（internal/permission/codes.go:43），再开一条路径就必须新增权限点，
// 而「新增权限点 = 已授权角色静默缩权」是本仓有案可查的事故（见 order_write.go 头注释）。
//
// 与 S2（订单列表页批量流转 / 批量取消，order_bulk_write.go）同一套形状与同一份回执结构：
// `{ids, remark}` 进，`{updated, skipped, items[].id/updated/message}` 出。页面侧那套
// 「受控结论文案 + 数字归一比对」不跟着迁移：结论文案不再由 Go 拼整句（那是提示页的形态），
// 改由模板词条 + 计数承载（见 public/migrations/605_admin_returns_write_ui_i18n.sql）。
//
// 两条与单条动作**故意不同**的语义（原先就在页面 handler 里，随迁移搬到 JSON 出口）：
//   · 批量同意**不自动入库**：autoReceive 会把「同意 → 入库 → 退款」一步做完，批量点一下
//     等于对一批单同时加库存与放款，那不是批量操作该承担的确认强度。批档白名单里因此
//     **没有** autoReceive —— 请求带它会被形状校验直接 400，而不是被当成单条字段忽略。
//   · 批量拒绝**理由必填**：逐条拒绝却没有理由，事后在状态流转链上会看到一串没有理由的
//     「已拒绝」，审计上查不清。缺理由整批 400（不逐条跳过），与订单侧的批量取消同口径。
//
// 时间与退货单编号：`ids` 里的元素是**字符串**（基座按勾选框的 value 原样收），逐条
// `strconv.ParseUint` 后才交给 service —— 非法元素只跳过它自己并计入 skipped，不让整批停下
// （批量操作因一条非法 id 整批失败时，用户会以为「一条都没做」然后反复重试）。

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// 批档顶层白名单（形状判据：只认这几把键，多一个都 400）。
//
// 单条的 `returnId` 刻意不在批档白名单里：`{"returnId":1,"ids":["2"]}` 这种混形如果按某一边
// 「猜」着执行，被改掉的可能恰恰是调用方以为不会动的那一批（口径同 order_bulk_write.go）。
var (
	returnBulkApproveKeys = map[string]bool{"ids": true, "remark": true}
	returnBulkRejectKeys  = map[string]bool{"ids": true, "remark": true}
)

// returnBulkApprove 批档同意退货（POST /api/order/return/approve 的批档形状）。
//
// 逐条走与单条**同一个** service 方法：不在「待审核」的申请（已被别人审过、已撤销）由服务端
// 拒绝，只跳过它并计入 skipped，其余照常同意 —— 一条不合规的申请不该让整批停下。
// 失败只计入 skipped 并回带原因（走 orderReasonText，与单档失败共用同一个取词层）。
func (h *Handle) returnBulkApprove(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, returnBulkApproveKeys)
	if !ok {
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
			_, err := h.svc.ApproveReturn(ctx, &orderdto.ReturnReviewReq{
				ReturnID: id,
				Remark:   remark,
				// 批量一律不自动入库（见文件头注释）：批档白名单里也没有这个键。
				AutoReceive: false,
				// 操作人由会话覆盖写入，绝不受请求体影响。
				OperatorType: orderOperatorTypeAdmin,
				OperatorID:   operatorID,
				OperatorName: operatorName,
			})
			if err != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(err, "批量同意退货申请失败")
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

// returnBulkReject 批档拒绝退货（POST /api/order/return/reject 的批档形状）：理由必填。
//
// 缺 / 空 / 显式 null 整批 400 + **零 service 调用**（用例用 nil service 钉住：真调了就会
// panic）。顺序与单条一致：先形状、再必填、最后逐条业务。
func (h *Handle) returnBulkReject(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, returnBulkRejectKeys)
	if !ok {
		return
	}
	remark, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if remark == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.BulkReturnRejectReasonRequired)
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
			if _, err := h.svc.RejectReturn(ctx, &orderdto.ReturnReviewReq{
				ReturnID:     id,
				Remark:       remark,
				OperatorType: orderOperatorTypeAdmin,
				OperatorID:   operatorID,
				OperatorName: operatorName,
			}); err != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(err, "批量拒绝退货申请失败")
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
