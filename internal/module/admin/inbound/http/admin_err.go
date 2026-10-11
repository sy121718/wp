package adminhttp

// admin_err.go — admin handler 的错误响应归口（审计 CQ-009 / CQ-010 的 admin 收口）。
//
// 背景：本模块的 inbound/http 曾有 74 处把 err.Error() 直接拼进响应消息 ——
// 44 处 `MsgBadRequest+": "+err.Error()`（请求绑定失败）与 30 处裸 `err.Error()`（service 错误）。
// service 一旦把 PostgreSQL 原文上抛（表名、唯一约束名 uk_sys_role_code、SQLSTATE 23505），
// 它就会原样渲染到后台页面上 —— 响应不是可信边界。
// 门禁：scripts/check-no-internal-error-leak.sh（判据是「任何 *.ErrorWithMessage( / c.String( 的
// 实参里出现 .Error()」）。
//
// **判据只有一条**：`adminui.PageErrorFact`（内部是 pkg/response.IsBusinessError 的形态判定：
// `模块.类别.语义` 的 key，或 `Err*` / `Msg*` 常量名）。本模块原先在 enums 里另养了一张
// `AdminFacingMessages` 白名单 —— 那是**第二份真相**：service 换了措辞、或某个 key 改由别的
// 模块提供，白名单就静默失配，而两种症状都不报错（有时把内部错误当业务文案透出、有时把业务
// 文案吞成归口）。白名单、它的 AST 对账用例、以及 decoder 都已随本批删除；模块侧只剩两样
// 自己的东西：**归口文案**（adminenums.ErrInternal）与**结构化日志**（场景 + user_id + 原文）。
//
// 三个入口：
//   · adminBindFail —— c.ShouldBind* 的失败。客户端输入问题：gin 的绑定错误只说哪个字段不合法，
//     不含库表信息。按 MsgBadRequest 返回，具体提示只进日志（日志里才需要「哪个字段」）。
//   · adminFail —— service 错误的 **JSON 出口**：业务错误 400 + 业务文案，其余 500 + 归口文案。
//   · adminErrParam —— service 错误的**页面出口**：返回可直接渲染的文本（模板数据槽 / 片段槽
//     还没有取词化，见 adminListLoadErr 的注释）。判定与 adminFail 完全同源。
//
// 为什么不用 pkg/response.ErrorAuto：它的判据是一样的，但**日志**不一样 —— 这里必须带 user_id，
// 事后才能回答「谁点了哪个按钮」；而 adminui.PageErrorFact 装的就是「同一条判据 + 这条日志」。
//
// 为什么还能看到本模块自造的形态解码：admin 这一批 enums 的值就是**常量名**（ErrAccountLocked），
// 而共享形态判据的 `|` 参数协议只挂在点分 key 上 —— 带参形态 `ErrAccountLocked|30分钟` 过不了
// 那条正则。所以 adminParamParts 按 `|` 切一刀、拿 key 段再走**同一条判据**：
// 这不是第二份白名单，只是把模块自己的参数编码解出来（与 page 的 pageDetailText 同一性质）。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"
)

// adminErrScene 日志场景名（与 admin 模块其它 logger.Scene("admin") 调用点一致）。
const adminErrScene = "admin"

// adminServerErrEvent admin 接口内部错误的结构化日志事件名。
//
// 原文只进日志这一条判据有两半：响应侧归口（adminFail / adminErrParam），
// 日志侧留痕（本事件）。只做前一半等于把错误吞掉 —— 接口级用例
// public/test/admin/feature 的 TestAdminHandlerInternalErrorIsCollected 就钉这两半：
// 响应体里查不到 uq_ / SQLSTATE，而日志里必须查得到（连事件名一起）。
const adminServerErrEvent = "admin 接口内部错误"

// adminInternalMsg 归口结论（本模块的归口 key）。
//
// 为什么用 adminenums.ErrInternal 而不是 adminui.MsgInternalError：两条出口对外说的是同一个
// 事实，而本模块的归口词条（迁移 451 seed 的中英词条）已经就位、且被
// public/test/admin/feature 的接口级断言钉住 —— 换一条全站词条属于独立的文案迁移。
func adminInternalMsg() adminui.Msg {
	return adminui.Msg{Key: adminenums.ErrInternal, Fallback: "操作失败，请稍后重试"}
}

// adminParamFallbacks 带参协议的**缺词兜底**（key → 中文模板，占位符是 `%s`）。
//
// 它只是「词条缺失时页面上显示什么」，不是可透出性判据（判据在 adminui 的形态判定）——
// 与页面的 pageenums.ErrDetailFallbacks 同一性质，所以留在模块侧。
var adminParamFallbacks = map[string]string{
	adminenums.ErrAccountLocked: "账号已被锁定，请 %s 后重试",
}

// adminErrMsg 一条 service 错误 → 可展示的结论**事实**（key + 中文兜底 + `%s` 参数）。
//
// 提示页出口用它（shell.Jump 的 MsgKey / Msg / MsgArgs）：文案是事实而不是拼好的句子，
// 「这句属于哪个语言」由模板注入的 tf 在渲染期决定（见 adminui.Prepare）。
func adminErrMsg(c *gin.Context, err error) adminui.Msg {
	msg, _ := adminErrOutcome(c, err)
	return msg
}

// adminErrOutcome 同 adminErrMsg，另把「是不是业务错误」带出来（JSON 出口据此选状态码）。
//
// 判定顺序：带参协议先拆（那一步内部的判据也是共享的那一条）→ 其余交给 adminui.PageErrorFact
// （判定 + 未命中时记一条带 user_id 的结构化日志 + 归口）。
func adminErrOutcome(c *gin.Context, err error) (adminui.Msg, bool) {
	return adminErrOutcomeFact(c, err, adminui.PageErrorFact)
}

// adminErrOutcomeForAPI 与 adminErrOutcome 同一判据，只是内部错误那一半记**接口**事件名。
//
// 为什么不能共用一条日志：共享出口写的是「后台页面操作失败（非业务错误，只对外给归口文案）」，
// 它描述**页面**路径；admin 的三条入口里有两条是页面（adminErrMsg / adminErrParam）、
// 一条是 JSON 接口（adminFail）。接口故障在日志里长得像页面故障，排障时按事件名筛就会漏
// —— 而「响应归口 + 原文进日志」这条判据的两半分别由 public/test/admin/feature 的
// TestAdminHandlerInternalErrorIsCollected（接口事件名）与
// internal/module/admin/inbound/http 的 TestAdminErrParamInternalErrorIsCollected（页面事件名）钉住。
func adminErrOutcomeForAPI(c *gin.Context, err error) (adminui.Msg, bool) {
	return adminErrOutcomeFact(c, err, adminAPIErrFact)
}

// adminErrOutcomeFact 判定本体：factFn 决定「未命中业务形态」时记哪一条事件。
//
// 三入口的判据同源（都在 pkg/facing 的形态判定上），差别只有事件名 —— 这也正是
// 把 factFn 当参数传、而不是各写一份判定的原因。
func adminErrOutcomeFact(c *gin.Context, err error,
	factFn func(*gin.Context, string, error) adminui.ErrFact) (adminui.Msg, bool) {
	if err == nil {
		return adminInternalMsg(), false
	}
	if key, args, ok := adminParamParts(err); ok {
		return adminui.Msg{Key: key, Fallback: adminParamFallback(key), Args: args}, true
	}
	fact := factFn(c, adminErrScene, err)
	if !fact.Business {
		return adminInternalMsg(), false
	}
	return adminui.Msg{Key: fact.Key, Fallback: fact.Fallback}, true
}

// adminAPIErrFact 接口路径的判据：与共享出口同一条形态判定，内部错误换成**本模块的事件名**。
func adminAPIErrFact(c *gin.Context, scene string, err error) adminui.ErrFact {
	fact := adminui.PageErrorFactQuiet(c, scene, err)
	if !fact.Business {
		logger.Scene(scene).
			With("path", c.Request.URL.Path).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, adminServerErrEvent)
	}
	return fact
}

// adminParamFallback 带参 key 的缺词兜底；未登记时回 key 本身（可见的降级，而不是空白 ——
// 与改造前 r.TranslateMessage 的缺词行为一致）。
func adminParamFallback(key string) string {
	if fb, ok := adminParamFallbacks[key]; ok {
		return fb
	}
	return key
}

// adminParamParts 拆开 `key|param1|param2` 参数协议：key 段必须自己过得了共享形态判据。
//
// 点分 key 与老式常量名都认（`admin.err.sortFieldInvalid|x` 与 `ErrAccountLocked|30分钟`）。
func adminParamParts(err error) (key string, args []string, ok bool) {
	raw := strings.TrimSpace(err.Error())
	idx := strings.IndexByte(raw, '|')
	if idx <= 0 {
		return "", nil, false
	}
	key = strings.TrimSpace(raw[:idx])
	if !r.IsBusinessError(errors.New(key)) {
		return "", nil, false
	}
	return key, strings.Split(raw[idx+1:], "|"), true
}

// adminErrWire 结论事实 → pkg/response 的 `key|param` 协议串（JSON 出口用：
// ErrorWithMessage 的 translate 会翻译 + 按 | 后的参数填 `%s`）。
func adminErrWire(msg adminui.Msg) string {
	if msg.Key == "" || len(msg.Args) == 0 {
		return msg.Key
	}
	return msg.Key + "|" + strings.Join(msg.Args, "|")
}

// adminErrText 结论事实 → 可直接渲染的文本（页面出口用：那边不经过 response 的 translate）。
//
// 两级取法，各自对应一条已经存在的协议：
//   - 带参形态走 pkg/response 的 `key|param`（与 JSON 出口同一个实现）：缺词条时它原样返回
//     协议串，而走 Sprintf 会渲染出 `%!(EXTRA …)`；
//   - 其余走 adminui.MsgText（key + 中文兜底 + `%s` 参数，与模板注入的 tf 同一实现）。
func adminErrText(c *gin.Context, msg adminui.Msg) string {
	if len(msg.Args) > 0 && msg.Fallback == msg.Key {
		return r.TranslateMessage(c, adminErrWire(msg))
	}
	return adminui.MsgText(c, msg)
}

// adminErrParam 页面路径的错误文案归口：返回可直接渲染给运营看的一句话。
//
// 页面路径（写失败由 adminPageRef.fail 渲染成整页提示，见 admin_jump.go）不能走 JSON 出口，
// 所以这里返回文本而不是写响应。判据与 adminFail **完全同源**（同一个 adminErrOutcome）。
//
// 为什么必须翻译：页面提示是**直接渲染的文本**（模板 / 提示页不经过 response 的 translate）。
// 不翻的话运营会看到裸 key（ErrInternal）而不是「操作失败，请稍后重试」。
//
// 为什么不能让调用点直传 err.Error()：i18n 词条页保存失败会带上 PostgreSQL 原文
// （表名 sys_i18n、约束名、SQLSTATE），而它最终渲染在页面上 —— 表名与约束名
// 会直接摆给运营看，与 JSON body 一样不是可信边界。
func adminErrParam(c *gin.Context, err error) string {
	msg, _ := adminErrOutcome(c, err)
	return adminErrText(c, msg)
}

// adminFail 写出 service 错误的 JSON 响应：业务错误 400 + 业务文案，内部错误 500 + 归口文案。
//
// 内部错误的状态码必须如实是 500 而不是沿用调用方习惯写的 400 —— 否则客户端与监控
// 都会把内部故障当成参数问题（与 pkg/response.ErrorAuto 的取舍一致）。
func adminFail(c *gin.Context, err error) {
	msg, business := adminErrOutcomeForAPI(c, err)
	if business {
		r.ErrorWithMessage(c, http.StatusBadRequest, adminErrWire(msg))
		return
	}
	r.ErrorWithMessage(c, http.StatusInternalServerError, adminErrWire(msg))
}

// adminWriteFailed 管理页**写操作失败**的统一出口：先过本模块的判定，再交给壳层写出 400。
//
// 与 adminErrParam 同源（同一个 adminErrOutcome：同一条判据、同一处结构化日志），
// 差别只在出口形状 —— 这一条写 HTTP 响应，那一条返回文本塞进提示页。
//
// 保留原因：它是 admin 的 JSON 写出口在页面路径上的历史形态（写 400 + 文案）。
// 页面写动作现已统一走 admin_jump.go 的整页提示，本入口不再有生产调用点 ——
// 保留它是为了让「文案由模块负责、壳层只兜空」这条契约在 admin 侧仍有一个可引用的样板
// （adminui.AdminWriteFailedText 的文档注释指向它）。
func adminWriteFailed(c *gin.Context, err error) {
	adminui.AdminWriteFailedText(c, adminErrParam(c, err))
}

// adminBindFail 写出请求绑定失败的响应（400 + MsgBadRequest）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into struct field RoleCreateReq.status of type int`），
// 那是实现细节；原文进日志，排障走日志。
func adminBindFail(c *gin.Context, err error) {
	if err != nil {
		// 绑定失败是客户端输入问题，按 warn 记（不污染错误日志）；原文进 detail 字段。
		logger.Scene(adminErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			With("detail", err.Error()).
			Warn("admin 接口请求绑定失败")
	}
	r.ErrorWithMessage(c, http.StatusBadRequest, adminenums.MsgBadRequest)
}

// —— 列表页装载失败的降级出口 ——

// adminListLoadErr 列表页 .Err 槽的取值：**只由本次装载失败填充**。
//
// 六个管理页（管理员 / 角色 / 权限点 / 菜单 / 部门 / 数据规则）原先在装载失败时
// `c.String(500, pagesMsgAdminGenericFailed)`：浏览器里没有页面，只有一块写着 i18n key 的裸文本 ——
// 侧边栏、筛选框、分页全部消失，运营看到的是 key 而不是一句人话，也无从判断
// 「是我筛错了还是系统坏了」。列表查询失败是服务端问题，不该把**页面本身**一起拿走。
//
// 现在改为降级渲染：空列表 + 一条归口提示，页面结构完好（还能改筛选、点别的菜单）。
// 写操作的结论不再经 `?err=` 回带（走 admin_jump.go 的整页提示），所以这里不再有
// 「装载失败 vs URL 旧提示」的优先级问题 —— 提示条只有这一个来源。
//
// 判据与 adminErrParam 同源，所以「归口」这件事在装载失败路径上没有被绕过 ——
// 原始错误（表名 / 约束名 / SQLSTATE / 驱动原文）只进日志，页面上拿到的要么是业务文案、
// 要么是 ErrInternal 归口文案。
//
// **这个槽位还没取词化**：七个 system 模板直接渲染 `{{.Err}}` 是**文本**，
// 所以这里取成文本传进去。要改成「传 key、模板 tr」得连带改那七份模板的数据契约
// （本批清单里列了，未做）。
func adminListLoadErr(c *gin.Context, loadErr error) string {
	if loadErr == nil {
		return ""
	}
	return adminErrParam(c, loadErr)
}

// —— 批量结论文案（提示页的事实）——

// 批量删除的结论文案模板（%s 依次是：删除数、名词译文、[未删除数]）。
//
// 文案是**事实**（key + 中文兜底 + `%s` 参数）：参数在调用点按计数 strconv 后经 WithArgs 传入，
// 取词发生在渲染期（提示页模板的 tf）。名词也是事实 —— 它会在调用点被取成当前语言后
// 当作模板的实参，所以「模板属于哪个语言」与「名词属于哪个语言」天然同源。
var (
	adminBulkDoneMsg    = adminui.Msg{Key: adminenums.BulkDoneKey, Fallback: "已删除 %s 个%s"}
	adminBulkPartialMsg = adminui.Msg{Key: adminenums.BulkPartialKey, Fallback: "已删除 %s 个%s，%s 个未能删除（受保护或被引用）"}
)

// adminBulkNouns 批量删除文案里出现过的名词（= 各列表页 adminBulkOutcome 的实参）。
//
// 新增列表页时必须在这里补一个 —— 漏登记的症状是该页的结论文案退化成中文兜底
// （可见但不致命），不会变成伪造面。
var (
	adminBulkNounAdmin      = adminui.Msg{Key: adminenums.BulkNounAdmin, Fallback: "管理员"}
	adminBulkNounRole       = adminui.Msg{Key: adminenums.BulkNounRole, Fallback: "角色"}
	adminBulkNounPermission = adminui.Msg{Key: adminenums.BulkNounPermission, Fallback: "权限点"}
	adminBulkNounMenu       = adminui.Msg{Key: adminenums.BulkNounMenu, Fallback: "菜单"}
	adminBulkNounDept       = adminui.Msg{Key: adminenums.BulkNounDept, Fallback: "部门"}
	adminBulkNounDatarule   = adminui.Msg{Key: adminenums.BulkNounDatarule, Fallback: "数据规则"}

	// adminBulkNouns 全部名词（语言一致性用例按它逐个覆盖每种结论文案）。
	adminBulkNouns = []adminui.Msg{
		adminBulkNounAdmin, adminBulkNounRole, adminBulkNounPermission,
		adminBulkNounMenu, adminBulkNounDept, adminBulkNounDatarule,
	}
)

// adminBadRequestMsg 参数级失败（缺必填 / 一个可操作项都没有）的结论事实。
var adminBadRequestMsg = adminui.Msg{Key: adminenums.MsgBadRequest, Fallback: "请求参数错误"}
