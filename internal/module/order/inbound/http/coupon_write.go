package orderhttp

// coupon_write.go — 优惠码 JSON 写端点的**严格形状并集**（单档 / 批档）。
//
// 三条路径、两个形状（判别键是形状，不是 action 标签）：
//
//	POST /api/order/coupon/create  单档：券的整份配置（新建，无 id）
//	POST /api/order/coupon/update  单档：券的整份配置（含 id，改动）；批档：{ids, status, projectId?} 停用 / 启用
//	POST /api/order/coupon/delete  单档：{id}；批档：{ids}
//
// **为什么不给批量单开一条路径**：sys_permission 每个 code 只有一列 api_path / api_method，
// 再开一条 /api/order/coupon/bulk-delete 就必须新增权限点，而「新增权限点 = 已授权角色静默
// 缩权」是本仓有案可查的事故（block_router.go / 迁移 521）。于是同一个删除权限只能有一个
// 授权锚点，单条与批量共用它 —— 与 navigation_delete_write.go / mail_contact_write.go 同源。
//
// 批档为什么逐条调用 service：前端基座（static/js/ui/json_write.js 的 bulk 档）要**逐条结果**
//（items[].deleted|updated + items[].message）才能决定移除哪一行、并说清哪几个没成功。
// 券的删除与更新在 service 里都是**单条**方法（DeleteCoupon / UpdateCoupon），逐条调用与
// 「整批一次」在语义上逐字相同（每条各自一个事务），却因此能给出逐条原因；条数上限走
// pkg/bulkids（200，超限整批 400，与页面路径同源）。
//
// 两个动作的 items 形状**故意不同**，因为基座把「这一条成功了」与「把这一行从表格里删掉」
// 当成两件事（由 data-write-outcome 分开）：删除是破坏性动作 → deleted=true，前端据此移除该行；
// 停用 / 启用不是 → updated=true（**成功条目也回**），前端读 data.updated、不移除任何行。
// 若把启停成功回成 deleted=true，前端会把还在的券从列表里删掉（页面看着像删了，刷新才发现还在）。
//
// 停用 / 启用批档保留页面路径的语义（一条都不能改错）：
//   - UpdateCoupon 是**整体更新**，漏送的字段会被写成零值（把门槛悄悄清零、把券码抹成空）
//     → 必须先把券读回来，把它现有的每一个可改字段原样带上（couponToggleReq）；
//   - projectId 是**越界守卫**：券 id 与工程是两个独立字段，切了工程之后 DOM 里可能还留着
//     别的工程的券，越界的那张不处理（绝不替用户跨工程改一张他看不见的券），记为跳过并给原因。

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

// 三条端点各自的形状白名单（严格并集：多一个键就 400，不做「忽略未知字段」的宽松解析）。
var (
	// 单档删除接受**两个键名**：`id`（新口径，与 json_write.js 的 single 模式固定发的键名一致）
	// 与 `couponId`（历史键名：迁 JSON 之前这个 handler 读的就是 c.PostForm("couponId")）。
	// 旧键保留是为了不静默破坏既有调用方；两者**同时出现**算形状错（不替调用方选一个）。
	orderCouponDeleteSingleKeys = map[string]bool{"id": true, "couponId": true}
	orderCouponDeleteBulkKeys   = map[string]bool{"ids": true}
	// 批档停用 / 启用允许出现的键：ids（主体）+ status（目标状态）+ projectId（越界守卫）。
	orderCouponToggleBulkKeys = map[string]bool{"ids": true, "status": true, "projectId": true}
)

// orderCouponDeleteIDKey 单档删除的取值键：恰好给一个才返回键名。
//
// 两个键都给（或都不给）时第二个返回值为 false —— 调用方要么写错、要么在混用两套口径，
// 这种请求不猜、直接 400。
func orderCouponDeleteIDKey(raw map[string]json.RawMessage) (string, bool) {
	hasID := orderWriteHasKey(raw, "id")
	hasCouponID := orderWriteHasKey(raw, "couponId")
	switch {
	case hasID && hasCouponID:
		return "", false
	case hasCouponID:
		return "couponId", true
	case hasID:
		return "id", true
	default:
		return "", false
	}
}

// couponTimeInputValue 把 **datetime-local 控件形态** 的时间窗文本归口成服务端认的写法。
//
// 为什么必须有这一步：`<input type="datetime-local">` 提交的是带 T 的形态
// （`2006-01-02T15:04`，有秒时 `2006-01-02T15:04:05`），而券时间窗**只接受不带时区**的三种
// 写法（见 service 的 couponTimeLayouts：`2006-01-02 15:04:05` / `2006-01-02 15:04` /
// `2006-01-02`）。迁 JSON 之前，页面 POST 那条路由 couponSaveReqFromForm → couponFormTimeValue
// 做这次归口；现在浏览器把表单直接发给 API，归口只能在 handler 里做 —— 少了它，运营只要动过
// 时间窗就会收到「生效时间不合法」（而字段明明是按控件要求填的）。
//
// 只认 T 形态：其余写法**原样回传**，交给服务端按自己的口径判非法 —— handler 不替调用方猜格式，
// 也不把「已经合法的写法」再解析一遍（那等于两处都在定义格式）。
func couponTimeInputValue(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return s
}

// orderCouponBulkItem 批量逐条结果。
//
// 字段名与 block / mail / navigation 的批量结果同形（id + deleted / updated + message）：
// 前端的 json_write.js 按 data.deleted / data.updated 计数、按 items[].message 列出逐条原因，
// 各模块给不同的字段名就等于让基座维护多套读法。
type orderCouponBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted,omitempty"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

// orderCouponBulkResult 批量回执（两个计数键都给：前端按 outcome 读其中一个）。
type orderCouponBulkResult struct {
	Deleted int                   `json:"deleted"`
	Updated int                   `json:"updated"`
	Skipped int                   `json:"skipped"`
	Items   []orderCouponBulkItem `json:"items"`
}

// orderCouponInUseFallback 「已有核销记录，不能删除」的缺词兜底。
//
// 取值与 sys_i18n 的 zh-CN 词条**逐字一致**（迁移 180）。
var couponReasonFallbacks = map[string]string{
	orderenums.ErrCouponCodeTaken:     "这个优惠码已经存在",
	orderenums.ErrCouponTypeInvalid:   "优惠类型只支持按比例折扣或固定金额",
	orderenums.ErrCouponValueInvalid:  "优惠值不合法",
	orderenums.ErrCouponWindowInvalid: "生效时间不合法",
	orderenums.ErrCouponInUse:         "该优惠码已有核销记录，不能删除",
}

// orderCouponReasonText 券写动作的**逐条原因**（JSON 回执的 items[].message）。
//
// 比 orderReasonText 多认一层**管理侧白名单**（couponFacingText 的 extras：券码重复、
// 折扣类型 / 折扣值 / 时间窗不合法、有核销记录不能删）。那批文案不在
// orderenums.UserFacingMessages 里（那张表是结算链路 / 访客面用的），只认它会让运营最需要的
// 原因（「该优惠码已有核销记录，不能删除」）被抹成「操作失败，请稍后重试」。
//
// 两种出口的取词入口**刻意不同**（与 navigation 同口径）：页面出口 couponFacingError 走
// shell.TranslateFor 产出要塞进 HTML 的一句话；这里是 JSON 回执里的成品句，走
// pkg/response 的取词层（与顶层 message 同一个语言协商），词条缺席时用上面那张兜底表，
// 未命中任何白名单时记结构化日志并给归口文案（数据库原文只进日志）。
func orderCouponReasonText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	msg := couponFacingText(err.Error())
	if msg == "" {
		return orderReasonText(c, err)
	}
	if text := response.TranslateMessage(c, msg); text != msg {
		return text
	}
	if fallback := couponReasonFallbacks[msg]; fallback != "" {
		return fallback
	}
	// couponFacingText 也认两条**本来就是中文**的页面文案（券 id 不合法 / 券不属于当前工程），
	// 它们不是 key，原样交出即可。
	return msg
}

// orderWriteMissingCouponText 「这张券不在可操作范围内」的成品句。
//
// 覆盖两种事实：券读不到（已被别人删掉）与券不属于当前工程。对操作者而言是同一件事
// —— 这张券**不在当前列表（工程作用域）的可操作范围内**，处理它的方式也一样（刷新列表）。
func orderWriteMissingCouponText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrCouponNotFound, "优惠码不存在")
}

// orderCouponToggleTargetOf 读批档的目标状态：只认字面 0 / 1。
//
// 缺键、显式 null、字符串 "0"、"2"、布尔、小数一律形状错（400）：停用 / 启用是两个明确的
// 目标状态，替调用方猜「大概是想启用」等于让一次写操作去赌对方的意思。
func orderCouponToggleTargetOf(raw map[string]json.RawMessage) (int, bool) {
	body, ok := raw["status"]
	if !ok || orderWriteIsExplicitNull(body) {
		return 0, false
	}
	switch strings.TrimSpace(string(body)) {
	case "0":
		return 0, true
	case "1":
		return 1, true
	default:
		return 0, false
	}
}

// orderCouponProjectOf 读批档的工程守卫（可选字段，但给了就必须是字符串）。
func orderCouponProjectOf(raw map[string]json.RawMessage) (string, bool) {
	body, ok := raw["projectId"]
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

// couponTimeValueUTC 把 dto 里的**时刻**写成服务端认的写法（不带时区、按 UTC 呈现）。
//
// 为什么按 UTC 而不是本地墙钟：券时间窗只接受不带时区的三种写法，且服务端按
// couponWindowLocation（= **UTC**）解释它们（service 的 parseCouponTime）。于是「按本地墙钟
// 写回去」会把时刻整体平移一个时区偏移 —— 表现是「批量启停把券的生效时间悄悄挪了几小时」。
// 按 UTC 呈现不引入任何偏移：读出来的时刻原样写回，仍是同一个时刻。
//
// 与 couponTimeInputValue 的分工：那个负责**控件输入**（带 T 的形态 → 服务端认的形态，算子
// 眼里看到的墙钟就是他要表达的时刻）；这里负责**系统自己回送的值**（口径必须与读出来的
// 时刻一致，不能经过展示层）。两者都表达「服务端认的写法」，但来源不同，不能互相替代。
func couponTimeValueUTC(v *utils.JSONTime) string {
	if v == nil {
		return ""
	}
	return v.Time().UTC().Format("2006-01-02 15:04:05")
}

// couponToggleReq 由券的**当前值**拼出整体更新请求：只改 status，其余字段原样回送。
//
// 时间窗走 couponTimeValueUTC（时刻逐位不变），**不用**展示层的 dateTimeLocal：那个是控件
// 形态（带 T）且在服务端非法 —— 用它拼请求会让整批启停回「生效时间不合法」（本批的负样本
// 方向实测过），即便改成墙钟写法也会平移时区。
func couponToggleReq(cp *orderdto.CouponResp, target int, operatorID uint64, operatorName string) *orderdto.CouponSaveReq {
	return &orderdto.CouponSaveReq{
		ID:            cp.ID,
		ProjectID:     cp.ProjectID,
		Code:          cp.Code,
		Name:          cp.Name,
		DiscountType:  cp.DiscountType,
		DiscountValue: cp.DiscountValue,
		MinSubtotal:   cp.MinSubtotal,
		MaxUses:       cp.MaxUses,
		PerUserLimit:  cp.PerUserLimit,
		StartsAt:      couponTimeValueUTC(cp.StartsAt),
		EndsAt:        couponTimeValueUTC(cp.EndsAt),
		Status:        target,
		Remark:        cp.Remark,
		// 操作人由会话覆盖写入，绝不受请求体影响。
		OperatorID:   operatorID,
		OperatorName: operatorName,
	}
}

// couponDeleteBulk 批档删除：逐条走与单条删除**同一个** service 方法，某一条失败只计入
// skipped 并回带原因，整批不中断。
//
// 整批回滚会让用户以为「一个都没删」，然后反复重试；而「有核销记录的券被拒」本来就是
// 预期内的业务结果（该券请改用停用），不该牵连同批的其它券。
func (h *Handle) couponDeleteBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteNormalizeIDs(c, raw)
	if !ok {
		return
	}
	res := orderCouponBulkResult{Items: make([]orderCouponBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderCouponBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			// 形状先行：这一条压根不是 id，没有库也能判出来。
			item.Message = orderWriteParamText(c)
		default:
			if derr := h.svc.DeleteCoupon(ctx, id); derr != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(derr, "批量删除优惠码失败")
				item.Message = orderCouponReasonText(c, derr)
			} else {
				item.Deleted = true
			}
		}
		if item.Deleted {
			res.Deleted++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}

// couponToggleBulk 批档停用 / 启用（POST /api/order/coupon/update 的批档形状）。
func (h *Handle) couponToggleBulk(c *gin.Context, raw map[string]json.RawMessage) {
	target, ok := orderCouponToggleTargetOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	projectID, ok := orderCouponProjectOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderWriteNormalizeIDs(c, raw)
	if !ok {
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	res := orderCouponBulkResult{Items: make([]orderCouponBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderCouponBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			item.Message = orderWriteParamText(c)
		default:
			cp, gerr := h.svc.GetCoupon(ctx, id)
			switch {
			case gerr != nil || cp == nil:
				item.Message = orderWriteMissingCouponText(c)
			case projectID != "" && cp.ProjectID != projectID:
				// 越界守卫：不跨工程改一张操作者看不见的券，但要说清为什么没动它。
				item.Message = orderWriteMissingCouponText(c)
			default:
				if _, uerr := h.svc.UpdateCoupon(ctx, couponToggleReq(cp, target, operatorID, operatorName)); uerr != nil {
					logger.Scene(orderWriteScene).With("id", rawID).Error(uerr, "批量启停优惠码失败")
					item.Message = orderCouponReasonText(c, uerr)
				} else {
					item.Updated = true
				}
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
