package orderhttp

// order_write.go — 订单模块 JSON 写端点的**严格形状**工具（单档 / 批档并集）。
//
// 为什么需要这一层：本批把 order 域的页面写动作逐条迁到**真实 JSON 端点**（优惠码 → 订单 →
// 退货）。一个动作一条路径，而**批量不另开路径** —— sys_permission 每个 code 只有一列
// api_path / api_method，再开一条 /api/order/coupon/bulk-delete 就必须新增权限点，而
// 「新增权限点 = 已授权角色静默缩权」是本仓有案可查的事故（block_router.go / 迁移 521）。
//
// 判别键因此是**形状**而不是 action 标签：
//   - 单档只认单条键（id，或券那种「一整份配置」的字段集合）；
//   - 批档只认 ids。
//
// 混形（两个都给、一个都不给、白名单外的键、**显式 null**、类型不符）一律 400：猜分支会写错
// 数据，而删除没有撤销；显式 null 必须单独判 —— encoding/json 把 null 解进 string / []string /
// uint64 都不报错（落到零值），不判原文就会静默变成「一次空批量」或「把字段清空」。
//
// 与 mail（mail_contact_write.go）和 navigation（navigation_delete_handle.go）同形而**不是各写一套**：
// 前端基座只有一个（internal/templates/static/js/ui/write-base.js），它读 id / ids /
// items[].deleted|updated / items[].message 这一套字段名，三个模块给三套写法就等于让基座
// 维护三种协议。

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// orderWriteScene 结构化日志场景名（与 order 模块其它 logger.Scene("order") 一致）。
const orderWriteScene = "order"

// orderWriteMaxBodyBytes 写请求体上限（1 MiB，与 navigation 的写入口径一致）。
//
// 超过上限直接 400 而不是截断：截断后的 JSON 一定解析失败，调用方拿到的会是一句
// 「参数不合法」，看不出「其实是太大了」。
const orderWriteMaxBodyBytes = 1 << 20

// orderWriteBody 读一次请求体、解析顶层键值，并**把 body 复位**。
//
// 复位是刻意的：单档仍旧走 pkg/binding 的 Form（gin 按 Content-Type 选绑定器，form 与 json
// 两种绑定语义、binding:"required" 的校验都逐字不变），而 ShouldBind 要重新读同一个 body。
// 读一次判形状、再把 body 交还给 gin，比「自己实现一份绑定」少一整套漂移面。
//
// 返回的键值表有两种取值，调用方必须区分：
//   - `raw != nil` —— 请求体是 JSON 对象，形状判据可用（批档与单档由此分流）；
//   - `raw == nil && ok` —— 请求体**不是** JSON（表单编码 / 空体 / 解析不出对象）：
//     形状未知，调用方一律按**单档**处理，交回 ShouldBind 自己判。
//     这条分支是必须的：本批只把「批量」这一个能力搬进 JSON，单档仍要接受表单编码的
//     老调用方（后台页面在迁移期间、脚本与集成方在迁移之后都可能还在发表单）。
//
// 顶层是数组 / 字符串 / null、超过上限、读失败一律 400 —— 那是**任何**绑定器都读不出的体，
// 继续往下走只会得到一句更含糊的错误。
func orderWriteBody(c *gin.Context) (map[string]json.RawMessage, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return orderWriteReject(c, "写请求缺少请求体")
	}
	body, ok := orderWriteReadBody(c)
	if !ok {
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	// 表单编码：写基座统一走 formdata（批次 2），而形状分派（单档 / 批档）靠**顶层键**判断，
	// 所以这里要把表单字段也提成等价的 raw，否则批档请求会被当成"形状未知 → 单档"。
	if orderWriteIsFormBody(c) {
		return orderWriteFormRaw(c, body)
	}
	// 其它非 JSON 的体（空体等）：形状未知 → 单档，交给 gin 自己的绑定器。
	if c.ContentType() != ginbinding.MIMEJSON {
		return nil, true
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return orderWriteReject(c, "写请求体不是 JSON 对象")
	}
	return raw, true
}

// orderWriteReject 统一的 400 出口（只记日志线索，body 原文不入日志）。
func orderWriteReject(c *gin.Context, reason string) (map[string]json.RawMessage, bool) {
	logger.Scene(orderWriteScene).With("reason", reason).Warn("订单写请求体被拒")
	response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
	return nil, false
}

// orderWriteReadBody 读出整个请求体（带上限）。
func orderWriteReadBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, orderWriteMaxBodyBytes+1))
	if err != nil {
		logger.Scene(orderWriteScene).Error(err, "读取订单写请求体失败")
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	if len(body) > orderWriteMaxBodyBytes {
		// 只记字节数：body 里可能有运营填的整份券配置，原文不入日志。
		logger.Scene(orderWriteScene).With("bytes", len(body)).Warn("订单写请求体超过上限，已拒绝")
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	return body, true
}

// orderWriteIsFormBody 这次请求是不是表单体（urlencoded 或 multipart）。
func orderWriteIsFormBody(c *gin.Context) bool {
	ct := c.ContentType()
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(ct, "multipart/form-data")
}

// orderWriteFormRaw 把表单字段提成与 JSON 请求等价的 raw。
//
// 两种编码的解析入口不同（urlencoded → ParseForm；multipart → ParseMultipartForm）。
// 解析后必须把 body 复位：单档分支还要交给 binding.Form 再读一遍。
func orderWriteFormRaw(c *gin.Context, body []byte) (map[string]json.RawMessage, bool) {
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(orderWriteMaxBodyBytes); err != nil {
			return orderWriteReject(c, "multipart 请求体解析失败")
		}
	} else if err := c.Request.ParseForm(); err != nil {
		return orderWriteReject(c, "表单请求体解析失败")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	form := c.Request.PostForm
	if len(form) == 0 {
		return nil, true
	}
	raw := make(map[string]json.RawMessage, len(form))
	for key, vals := range form {
		if len(vals) == 0 {
			continue
		}
		raw[key] = formValueRaw(key, vals)
	}
	if len(raw) == 0 {
		return nil, true
	}
	return raw, true
}

// formValueRaw 一个表单字段 → JSON 标量字节。
//
// 三条规则，都由下游读取方的口径决定（见各自注释）：
//  1. ids 恒按字符串数组（orderWriteIDsOf 解成 []json.RawMessage，裸 `1` 解不开）；
//  2. 同名多值 → 字符串数组；
//  3. 单值按值推断：数字/bool/null 裸发，其余包引号（两个读取方口径相反，见 rawScalarOf）。
func formValueRaw(key string, vals []string) json.RawMessage {
	if key == "ids" || key == "ids[]" {
		return quotedJSONArray(vals)
	}
	if len(vals) > 1 {
		return quotedJSONArray(vals)
	}
	return rawScalarOf(vals[0])
}

// quotedJSONArray 把若干字符串拼成 JSON 字符串数组。
func quotedJSONArray(vals []string) json.RawMessage {
	quoted := make([]string, 0, len(vals))
	for _, v := range vals {
		quoted = append(quoted, strconv.Quote(v))
	}
	return json.RawMessage("[" + strings.Join(quoted, ",") + "]")
}

// rawScalarOf 把一个 formdata 字符串值转成 JSON 标量的原始字节。
//
// 规则与 JSON 请求体保持一致：`0` / `1` / `-3.5` / `true` / `false` / `null` 原样当标量，
// 其余（含空串、带字母的 uuid）当字符串包引号。空串必须包引号成 `""` ——
// 裸空字节不是合法 JSON，下游 json.Unmarshal 会失败（而「空串」在业务上常是
// 「显式置空」，与「键不存在」不同）。
func rawScalarOf(v string) json.RawMessage {
	switch v {
	case "true", "false", "null":
		return json.RawMessage(v)
	case "":
		return json.RawMessage(`""`)
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return json.RawMessage(v)
	}
	return json.RawMessage(strconv.Quote(v))
}

// orderWriteHasKey 判断顶层是否出现某个键（出现即参与形状判别）。
//
// 用「键在不在」而不是「值是不是零」：{"ids": []} 是**合法**的「没勾选任何项」，
// 而 {"id": 0} 只是单档里的非法值 —— 两者必须由不同的判据分别回答。
func orderWriteHasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

// orderWriteShapeOK 顶层键集合必须落在这条分支的白名单里。
//
// 严格是刻意的：`{"id":1,"ids":["2"]}` 这种混形如果按某一边「猜」着执行，被改掉的可能
// 恰恰是调用方以为不会动的那一批；未知键则意味着调用方按另一份契约在说话，这时执行单边
// 语义等于拿一次写操作去赌对方的意思。
func orderWriteShapeOK(raw map[string]json.RawMessage, allow map[string]bool) bool {
	for key := range raw {
		if !allow[key] {
			return false
		}
	}
	return true
}

// orderWriteIsExplicitNull 判断字段是不是**显式** null（json.RawMessage 保留原文）。
func orderWriteIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

// orderWriteUintOf 取一个无符号整数字段。
//
// 只收**字面就是非负整数**的 JSON 数字：字符串 "5" 一律拒。判原文而不是判解出来的值 ——
// json.Number 的底层是 string，把 JSON 字符串 "5" 解进 uint64 **不会报错**，于是
// {"id":"5"} 会被当成 id=5 静默接受；两种形态表达同一个值只会让客户端各自猜一种。
// 字符串 / 布尔 / 对象 / 数组 / 小数 / 科学计数法 / 负数一律落在「不是非负整数」这一档。
//
// 缺键返回 (0, true)：省略是合法的调用方意图，是否「必填」由调用点按形状决定
// （白名单里只有这一个键时，缺键在调用点已经被 orderWriteHasKey 判掉了）。
func orderWriteUintOf(raw map[string]json.RawMessage, key string) (uint64, bool) {
	body, ok := raw[key]
	if !ok {
		return 0, true
	}
	text := strings.TrimSpace(string(body))
	if text == "" || text[0] < '0' || text[0] > '9' {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	var out uint64
	for i := 0; i < len(text); i++ {
		digit := uint64(text[i] - '0')
		if out > (^uint64(0)-digit)/10 {
			return 0, false // 溢出：这个 id 不可能存在，别静默截断成一个真实存在的 id
		}
		out = out*10 + digit
	}
	return out, true
}

// orderWriteIDsOf 取批档的 ids：必须是**字符串数组**且逐项都是字符串。
//
// 逐项判类型是为了 ["1", null] 这种半合法请求：静默把 null 当成空串再靠归一丢掉，
// 等于替调用方猜意图（去重与上限由 orderWriteNormalizeIDs 紧接着走 pkg/bulkids）。
// ids 本身是显式 null 时同样按形状错处理 —— `{"ids":null}` 否则会静默变成一次空批量，
// 调用方以为发了一批、回执却是「成功 0 项」。
//
// **空数组也是形状错**（本批的口径，方法是先校验后写库）：`{"ids":[]}` 表达的是「什么都没勾」，
// 那件事在浏览器侧就已经拦掉了（data-msg-none-selected，一次请求都不发），走到服务端只能是
// 调用方拼错了请求；放行会得到「200 + 成功 0 项」这种看着像成功的空动作。页面侧批量入口
// （未迁的那批）历史上允许空数组，本批不沿用 —— 那是另一套契约，没有兼容负担。
func orderWriteIDsOf(raw map[string]json.RawMessage) ([]string, bool) {
	body, ok := raw["ids"]
	if !ok || orderWriteIsExplicitNull(body) {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(string(item))
		if text == "" || text == "null" {
			return nil, false
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

// orderWriteNormalizeIDs 形状通过后的去重与上限（与页面路径同源：pkg/bulkids）。
//
// 超限整批 400、不部分执行：静默截断比无界更危险 —— 用户勾了 500 条、只处理了 200 条，
// 回执还说「已处理 200 个」，剩下 300 条无声消失。
func orderWriteNormalizeIDs(c *gin.Context, raw map[string]json.RawMessage) ([]string, bool) {
	ids, ok := orderWriteIDsOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	normalized, nerr := bulkids.Normalize(ids)
	if nerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderBulkTooManyText(nerr))
		return nil, false
	}
	return normalized, true
}

// orderBulkTooManyText 超限文案的**带参协议形态**（key|max|count）。
//
// 与 block / mail / navigation 同源：词条与两个数字都归 pkg/bulkids（词条里的两个 %s 依次是
// 上限与本次条数），response 的 translate 按 | 后的参数依次填参；页面侧的受控文案用的是同一条
// 词条，两个入口的提示因此不会各说各话。上限这件事说的是 pkg/bulkids 的规则，与动作是什么无关。
func orderBulkTooManyText(err error) string {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return bulkids.MsgTooMany
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, tooMany.Max, tooMany.Count)
}

// orderWriteParamText 参数级失败的可展示文案（当前语言）。
//
// 逐条原因里没有 error 对象（判定就在 handle 里，例如 ids 里混了一个非数字 id），走不了
// orderReasonText；词条缺席时回落**与 sys_i18n 的 zh-CN 词条逐字一致**的原文
// （迁移 180：参数不合法），否则界面上会出现一串裸 key。
func orderWriteParamText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrInvalidParam, "参数不合法")
}

// orderWriteNotFoundText 「这一条不存在」的可展示文案（当前语言，词条见迁移 180）。
func orderWriteNotFoundText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrCouponNotFound, "优惠码不存在")
}

// orderWriteTextOf 取一条受控文案的当前语言文本（词条缺席时回落中文原文）。
func orderWriteTextOf(c *gin.Context, key, fallback string) string {
	if text := response.TranslateMessage(c, key); text != key {
		return text
	}
	return fallback
}

// orderWriteInternalText 归口文案的当前语言文本（词条缺席时回落受控中文）。
func orderWriteInternalText(c *gin.Context) string {
	// orderenums.ErrInternal 本身就是中文原文（不是 key），两条分支都安全：
	// 是 key 就翻译，不是就原样返回。
	return response.TranslateMessage(c, orderenums.ErrInternal)
}

// orderReasonText 把一条错误事实转成**逐条原因**（items[].message）要显示的成品句。
//
// 与页面出口（order_shared.go 的 orderFacingError）刻意分开：那里的产物是要塞进 HTML 的
// 一句话、走 shell.TranslateFor；这里是 JSON 回执里的成品句、走 pkg/response 的取词层
// ——与顶层 message 同一个语言协商，同一个错误在接口与页面上说法一致这件事靠**共用同一份
// 白名单**（orderenums.UserFacingMessages）保证，而不是靠两边各翻一遍。
//
// 未命中白名单（数据库原文、驱动前缀）→ 记结构化日志 + 归口文案：逐条原因是列表里显示给
// 人看的「为什么这一条没成功」，裸 key 与库表原文同属「响应不是可信边界」那一类。
func orderReasonText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderFacingText(err.Error()); msg != "" {
		if text := response.TranslateMessage(c, msg); text != msg {
			return text
		}
		// 词条缺席：白名单里存的是**已经能给人看**的中文业务文案，原样交出即可。
		return msg
	}
	logger.Scene(orderWriteScene).
		With("user_id", builtin.GetUserID(c)).
		Error(err, "订单批量写逐条失败")
	return orderWriteInternalText(c)
}
