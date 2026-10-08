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
// 两个入口，对应两类错误：
//   · adminBindFail —— c.ShouldBind* 的失败。客户端输入问题：gin 的绑定错误只说哪个字段不合法，
//     不含库表信息。按 MsgBadRequest 返回，具体提示只进日志（日志里才需要「哪个字段」）。
//   · adminFail —— service 返回的错误。命中本模块 enums 白名单
//     （adminenums.AdminFacingMessages）→ 原样透出（前端要据此提示「哪一项不合法」）；
//     未命中 → 记结构化日志（场景 + user_id + 原始错误）并返回 ErrInternal + 500。
//   · adminErrParam —— **页面路径**的同一条判据（同一份白名单与同一处日志），
//     产出的是可直接渲染的**显示文本**：业务文案原样（翻成当前语言），未命中同样记日志并给
//     ErrInternal 归口文案。它由页面出口（adminPageRef.fail / admin_jump.go 的 shell.RenderJump）
//     放进提示页的响应体 —— 结论不再经查询参数回带。
//
// 为什么不用 pkg/response.ErrorAuto：它的判据是**形态**（`ErrXxx` 命名或 `模块.err.语义`），
// 全局有效、零维护，但它不知道「这句话是不是本模块的文案」。本模块要的是「响应里出现的
// 每一句话都来自 admin enums」；白名单写漏只表现成一句通用提示（一眼可见，且
// internal/module/admin/enums/admin_enums_test.go 会按 enums 源文件逐个常量对账），
// 而形态判定的漏判方向恰好相反：内部字符串只要长得像 key 就会被透出。
// 另一点是日志：这里必须带 user_id，事后才能回答「谁点了哪个按钮」。

import (
	"net/http"
	"strings"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// adminErrScene 日志场景名（与 admin 模块其它 logger.Scene("admin") 调用点一致）。
const adminErrScene = "admin"

// adminErrText 把 service 错误收敛为可对外展示的文案。
//
// 命中 adminenums.AdminFacingMessages → 返回 (文案, true)；未命中 → 记一条结构化日志
// 并返回 (adminenums.ErrInternal, false)。
//
// 带参协议：文案本身是 i18n key，参数走 `key|param` 形态（见 pkg/response 的 translate），
// 所以匹配时除整串相等外还认 `key|` 前缀 —— 只放行 key 命中白名单的那些参数串，
// 原样返回交给翻译层填充。
func adminErrText(c *gin.Context, err error) (text string, business bool) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	for _, m := range adminenums.AdminFacingMessages {
		if msg == m || strings.HasPrefix(msg, m+"|") {
			return msg, true
		}
	}

	logger.Scene(adminErrScene).
		With("user_id", shell.CurrentUserID(c)).
		Error(err, "admin 接口内部错误")
	return adminenums.ErrInternal, false
}

// adminFail 写出 service 错误的响应：业务错误 400 + 业务文案，内部错误 500 + 归口文案。
//
// 内部错误的状态码必须如实是 500 而不是沿用调用方习惯写的 400 —— 否则客户端与监控
// 都会把内部故障当成参数问题（与 pkg/response.ErrorAuto 的取舍一致）。
func adminFail(c *gin.Context, err error) {
	text, business := adminErrText(c, err)
	if business {
		r.ErrorWithMessage(c, http.StatusBadRequest, text)
		return
	}
	r.ErrorWithMessage(c, http.StatusInternalServerError, text)
}

// adminErrParam 页面路径的错误文案归口：返回可直接渲染给运营看的一句话。
//
// 页面路径（写失败由 adminPageRef.fail 渲染成整页提示，见 admin_jump.go）不能走 JSON 出口，
// 所以这里返回文本而不是写响应。判据与 adminFail **完全同源**（同一个 adminErrText：
// 同一份 adminenums.AdminFacingMessages 白名单、同一处结构化日志），只是出口不同：
//
//	· 命中白名单 → 文案原样（key|param 带参协议交给 r.TranslateMessage，与 JSON 出口一致）；
//	· 未命中 → 日志已经记过 → adminenums.ErrInternal 的归口文案。
//
// 为什么必须翻译：页面提示是**直接渲染的文本**（模板 / 提示页不经过 response 的 translate）。
// 不翻的话运营会看到裸 key（ErrInternal）而不是「操作失败，请稍后重试」。
//
// 为什么不能让调用点直传 err.Error()：i18n 词条页保存失败会带上 PostgreSQL 原文
// （表名 sys_i18n、约束名、SQLSTATE），而它最终渲染在页面上 —— 表名与约束名
// 会直接摆给运营看，与 JSON body 一样不是可信边界。
func adminErrParam(c *gin.Context, err error) string {
	text, business := adminErrText(c, err)
	if business {
		return r.TranslateMessage(c, text)
	}
	return r.TranslateMessage(c, adminenums.ErrInternal)
}

// adminWriteFailed 管理页**写操作失败**的统一出口：先过本模块三件套，再交给 shell 写出 400。
//
// 与 adminErrParam 同源（同一个 adminErrText：同一份白名单、同一处结构化日志），
// 差别只在出口形状 —— 这一条写 HTTP 响应，那一条返回文本塞进提示页。
//
// 保留原因：它是 admin 的 JSON 写出口在页面路径上的历史形态（写 400 + 文案）。
// 页面写动作现已统一走 admin_jump.go 的整页提示，本入口不再有生产调用点 ——
// 保留它是为了让「文案由模块负责、壳层只兜空」这条契约在 admin 侧仍有一个可引用的样板
// （shell.AdminWriteFailedText 的文档注释指向它）。
func adminWriteFailed(c *gin.Context, err error) {
	text, _ := adminErrText(c, err)
	shell.AdminWriteFailedText(c, text)
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
			With("user_id", shell.CurrentUserID(c)).
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
// 文案与 adminErrParam 同源（同一份 adminenums.AdminFacingMessages 白名单、同一处带 user_id 的
// 结构化日志），所以「归口」这件事在装载失败路径上没有被绕过 —— 原始错误（表名 / 约束名 /
// SQLSTATE / 驱动原文）只进日志，页面上拿到的要么是白名单业务文案，要么是 ErrInternal 归口文案。
func adminListLoadErr(c *gin.Context, loadErr error) string {
	if loadErr == nil {
		return ""
	}
	return adminErrParam(c, loadErr)
}

// —— 批量结论文案（提示页的成品文案）——

// adminBulkText 批量结论文案的一条模板 / 名词（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧 adminBulkOutcome / adminI18nBulkDeleteResult 拿它
// Sprintf 出整句，经 admin_jump.go 的提示页渲染。读侧判定（?done= 白名单）已随传输通道
// 整批删除，这一份现在是**唯一的**取词来源。
type adminBulkText struct{ key, fallback string }

// adminBulkTextOf 取一条批量结论文案的当前语言文本（写侧**唯一**取法）。
//
// 词条被写坏（混进 %d 之类协议外占位符）时回落中文原文：本文件里的模板一律只允许 %s
// （数字在 Go 侧 strconv.Itoa 之后再填），混进 %d 会让 Sprintf 把参数渲染成 int 而不是字符串 ——
// 而这条路是直接给运营看的。与 shell.BulkIDsFacingText 的兜底同一判据。
func adminBulkTextOf(c *gin.Context, t adminBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 批量删除的结论文案模板（%s 依次是：删除数、名词译文、[未删除数]）。
var (
	adminBulkDoneText    = adminBulkText{adminenums.BulkDoneKey, "已删除 %s 个%s"}
	adminBulkPartialText = adminBulkText{adminenums.BulkPartialKey, "已删除 %s 个%s，%s 个未能删除（受保护或被引用）"}
)

// adminBulkNouns 批量删除文案里出现过的名词（= 各列表页 adminBulkOutcome 的实参）。
//
// 新增列表页时必须在这里补一个 —— 漏登记的症状是该页的结论文案退化成 fallback
// （可见但不致命），不会变成伪造面。
var (
	adminBulkNounAdmin      = adminBulkText{adminenums.BulkNounAdmin, "管理员"}
	adminBulkNounRole       = adminBulkText{adminenums.BulkNounRole, "角色"}
	adminBulkNounPermission = adminBulkText{adminenums.BulkNounPermission, "权限点"}
	adminBulkNounMenu       = adminBulkText{adminenums.BulkNounMenu, "菜单"}
	adminBulkNounDept       = adminBulkText{adminenums.BulkNounDept, "部门"}
	adminBulkNounDatarule   = adminBulkText{adminenums.BulkNounDatarule, "数据规则"}

	// adminBulkNouns 全部名词（语言一致性用例按它逐个覆盖每种结论文案）。
	adminBulkNouns = []adminBulkText{
		adminBulkNounAdmin, adminBulkNounRole, adminBulkNounPermission,
		adminBulkNounMenu, adminBulkNounDept, adminBulkNounDatarule,
	}
)

// 词条页批量删除的四个结论分支。
var (
	adminI18nBulkNoneSelected = adminBulkText{adminenums.BulkI18nNoneSelected, "没有勾选任何词条，列表未改动。"}
	adminI18nBulkAllDeleted   = adminBulkText{adminenums.BulkI18nAllDeleted, "已删除 %s 条词条（构建时回退到组件包内的中文兜底）。"}
	adminI18nBulkAllSkipped   = adminBulkText{adminenums.BulkI18nAllSkipped, "%s 条词条都未能删除，列表未改动。"}
	adminI18nBulkPartial      = adminBulkText{adminenums.BulkI18nPartial, "已删除 %s 条，%s 条未能删除（可能已被删除）。"}
)

// —— 词条页：写侧构造 `<key> · <lang>` 身份串（供提示页回执）——

// adminI18nIdentitySep 词条身份的分隔符（写侧构造与校验共用）。
//
// 选它是因为 key 的 charset 不含空格（见 adminI18nKeyShapeOK），切分因此没有歧义。
const adminI18nIdentitySep = " · "

const (
	adminI18nKeyMaxLen  = 120
	adminI18nLangMaxLen = 16
)

// adminI18nKeyShapeOK 词条 key 是否是可安全回显的形状：[A-Za-z0-9._-]，1..120 字节。
//
// 词条 key 本来就是点分标识（admin.i18n.title）；把「能进页面的字符」收死之后，
// 这条回执里就塞不进整句话，也塞不进标签。
func adminI18nKeyShapeOK(key string) bool {
	if key == "" || len(key) > adminI18nKeyMaxLen {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// adminI18nLangShapeOK 语言标签是否是可安全回显的形状：
// 两到八个 ASCII 字母，可跟一个 `-` 加一到八位 ASCII 字母数字（zh-CN / en-US / en）。
func adminI18nLangShapeOK(lang string) bool {
	if lang == "" || len(lang) > adminI18nLangMaxLen {
		return false
	}
	base, region, hasRegion := strings.Cut(lang, "-")
	if !adminASCIIAlnumShape(base, false) {
		return false
	}
	return !hasRegion || adminASCIIAlnumShape(region, true)
}

// adminASCIIAlnumShape 判定 s 是否全是 ASCII 字母（digitsAllowed=false）或字母数字（true）。
func adminASCIIAlnumShape(s string, digitsAllowed bool) bool {
	minLen := 2
	if digitsAllowed {
		minLen = 1
	}
	if len(s) < minLen || len(s) > 8 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case digitsAllowed && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// adminI18nEntryIdentity 词条身份串 `<key> · <lang>`（写侧构造，供提示页回执）。
//
// 任一段不合形状就返回空串：写侧拿到空串时退回全站成功文案（提示页不显示身份），
// 而不是把任意文本摊到「已保存：」后面。
func adminI18nEntryIdentity(key, lang string) string {
	key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
	if !adminI18nKeyShapeOK(key) || !adminI18nLangShapeOK(lang) {
		return ""
	}
	return key + adminI18nIdentitySep + lang
}
