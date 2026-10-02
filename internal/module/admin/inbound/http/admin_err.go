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
//     产出的是要塞进 ?err= / ?errored= / 模板数据的**显示文本**：业务文案原样（翻成当前
//     语言），未命中同样记日志并给 ErrInternal 归口文案。页面靠 302 表达失败，没有地方放
//     状态码，所以它与 adminFail 的差别只有「不写字面码、只返回文本」这一点。
//
// 为什么不用 pkg/response.ErrorAuto：它的判据是**形态**（`ErrXxx` 命名或 `模块.err.语义`），
// 全局有效、零维护，但它不知道「这句话是不是本模块的文案」。本模块要的是「响应里出现的
// 每一句话都来自 admin enums」；白名单写漏只表现成一句通用提示（一眼可见，且
// internal/module/admin/enums/admin_enums_test.go 会按 enums 源文件逐个常量对账），
// 而形态判定的漏判方向恰好相反：内部字符串只要长得像 key 就会被透出。
// 另一点是日志：这里必须带 user_id，事后才能回答「谁点了哪个按钮」。

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/web/shell"
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
// 页面路径（c.Redirect 回列表页并带 ?err=/?errored=、或塞进模板数据）不能走 JSON 出口，
// 所以这里返回文本而不是写响应。判据与 adminFail **完全同源**（同一个 adminErrText：
// 同一份 adminenums.AdminFacingMessages 白名单、同一处结构化日志），只是出口不同：
//
//	· 命中白名单 → 文案原样（key|param 带参协议交给 r.TranslateMessage，与 JSON 出口一致）；
//	· 未命中 → 日志已经记过 → adminenums.ErrInternal 的归口文案。
//
// 为什么必须翻译：页面提示是**直接渲染的文本**（模板 {{.Err}} / {{.Errored}} 不经过
// response 的 translate）。不翻的话运营会看到裸 key（ErrInternal）而不是「操作失败，请稍后重试」。
//
// 为什么不能让调用点直传 err.Error()：i18n 词条页保存失败会带上 PostgreSQL 原文
// （表名 sys_i18n、约束名、SQLSTATE），而 ?err= 的值最终渲染在页面上 —— 表名与约束名
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
// 差别只在出口形状 —— 这一条写 HTTP 响应（页面上的表单 / HTMX 请求），那一条返回文本
// 塞进 ?err= / 模板数据。
//
// 为什么不能直接用 shell.AdminWriteFailed：那个入口恒给 MsgInternalError 通用提示，
// 会把「角色编码已存在 / 用户名已存在 / 该部门下有子部门」这类业务错误一并吞掉 ——
// 运营看到的是「系统出错了」，但该改的是表单里的某一个字段，于是只能反复重试。
// 这与上一批修的 22 处 r.ErrorInternal(c, "admin", err) 是同一类反向缺陷，只是发生在页面路径。
//
// 为什么不需要逐处判断「这个 err 会不会是业务错误」：分流靠白名单天然完成 ——
// 命中 → 400 + 原样业务文案；未命中（SQLSTATE / 约束名 / 表名 / 驱动原文）→ 已记日志
// → 400 + ErrInternal 归口文案。逐处判断反而会把这条判据拆成 19 份各自为政的猜测。
//
// 状态码沿用 400：页面路径的语义是「表单提交失败」，响应体给的是一句可直接展示的文案；
// 需要如实区分 400 / 500 的是 JSON 出口（adminFail），那里才有机器可读的状态码消费方。
func adminWriteFailed(c *gin.Context, err error) {
	text, _ := adminErrText(c, err)
	shell.AdminWriteFailedText(c, text)
}

// adminPageErrParamMaxBytes 页面提示（?err= / ?errored=）允许进模板的最大字节数。
//
// 200 字节：中文按每字 3 字节算是 66 个字，够放「已删除 12 个角色，3 个未能删除（受保护或被引用）」
// 这类服务端拼装的整句；同时把「一次提交几十 KB 的 err= 参数」挡在页面之外。
//
// 它同时是**候选文案的尺寸上限**：超过它的候选永远不可能被命中（原始参数会先被截断），
// 表现为「写侧发了提示、页面上什么都不显示」——admin_err_texts_test.go 有一条例会把这条钉住。
const adminPageErrParamMaxBytes = 200

// adminErrTexts 页面提示参数（?err= / ?errored=）**可以原样渲染**的受控文案集合（数字归一后可比）。
//
// 候选**全部来自写侧真实生产点**，读侧不许自己发明一句话（AGENTS.md「不要新造第二份真相」）：
//
//  1. adminBulkPartialText × adminBulkNouns —— adminBulkResultURL 在 skipped > 0 时
//     Sprintf 出的「已删除 N 个角色，M 个未能删除（受保护或被引用）」（**当前语言**：
//     模板与名词都经 adminBulkTextOf 取词，与写侧同一个取法）；
//  2. adminI18nBulkAllSkipped / adminI18nBulkPartial —— adminI18nBulkDeleteResult 里
//     会进 ?err= 的两个分支（词条页批量删除「有跳过」）；
//  3. shell.BulkIDsNoticeTemplate(c) —— shell.BulkIDsFacingText 的超限提示，读侧直接取
//     写侧同一份模板（两次归一不会互相破坏：NoticeTemplate 的输出再过一遍
//     NormalizeNoticeDigits 是不动点，而 FacingNotice 对候选做的正是这一步）；
//  4. adminenums.AdminFacingMessages 的当前语言译文 —— adminErrParam 命中白名单时的返回值，
//     词条页 ?errored= 的两个写路径之一（I18nEntrySave / I18nEntryDelete）；
//  5. adminenums.ErrInternal 的当前语言译文 —— adminErrParam 未命中时的归口文案，
//     同一个 ?errored= 通道的另一半。
//
// 为什么必须带 c：第 1 条要按当前语言取批量结论模板与名词（adminBulkTextOf），第 3 条要按
// 当前语言取 shell 的模板，第 4/5 条要按当前语言翻译 —— 写侧产出的全是**当前语言**的文本，
// 候选不按同一语言取，英文页面上真实的提示会被判成伪造而静默消失。
// 本批之前只有第 3/4/5 条按语言取，批量结论文案是中文常量（不随语言变），
// 于是英文页面上那两条回执必然失配 —— adminDoneTexts 同此，两处现在都按当前语言取。
//
// 带参形态（key|param）**不在候选里**：本页路径上唯一带参的白名单文案是 ErrAccountLocked，
// 它只出现在登录 API 路径；词条页 ?errored= 的值域只有「三个缺项文案 + 归口文案」。
// 将来若页面路径引入带参文案，这里必须补参数化匹配（否则那条提示静默消失）——
// admin_err_texts_test.go 的写侧对账用例会在候选不足时失败。
func adminErrTexts(c *gin.Context) []string {
	out := make([]string, 0, len(adminBulkNouns)+2+len(adminenums.AdminFacingMessages)+1)
	// 取当前语言的模板：写侧 adminBulkResultURL / adminI18nBulkDeleteResult 也走 adminBulkTextOf，
	// 两处同源。候选若按中文原文构造，英文页面上真实的提示会被判成伪造而静默消失。
	partial := adminBulkTextOf(c, adminBulkPartialText)
	for _, noun := range adminBulkNouns {
		// 名词要 Sprintf 进去再归一：模板里的 %s 是名词（不是计数），先把占位替掉，
		// 否则 NoticeTemplate 会把名词一起归一成 "0"，候选就永远对不上真实文案。
		// 名词译文同样取自 adminBulkTextOf（写侧就是这么取的，两处必须同语言）。
		out = append(out, shell.NoticeTemplate(fmt.Sprintf(partial, "0", adminBulkTextOf(c, noun), "0")))
	}
	out = append(out,
		shell.NoticeTemplate(adminBulkTextOf(c, adminI18nBulkAllSkipped)),
		shell.NoticeTemplate(adminBulkTextOf(c, adminI18nBulkPartial)),
		shell.BulkIDsNoticeTemplate(c),
	)
	for _, m := range adminenums.AdminFacingMessages {
		out = append(out, r.TranslateMessage(c, m))
	}
	return append(out, r.TranslateMessage(c, adminenums.ErrInternal))
}

// adminPageErrText 页面提示参数（?err= / ?errored=）的受控出口：清洗 → 整体命中受控文案，否则空串。
//
// 这些值现在都由服务端构造（adminErrParam / adminBulkResultURL / 词条页回带），但**页面
// 不是可信边界**：?err=任意文本 谁都能手写，渲染出来就是一条顶着「系统提示」样式的伪造
// 消息（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」，以及超长参数会把
// 提示条撑破）。这是 AGENTS.md「三种泄漏形态」里形态③（模板数据）的相邻面：泄漏管的是
// 「不许出内部原文」，这里补的是「不许把调用方能控制的任意串当系统文案渲染」。
//
// 两级判据（与 ?done= / ?saved= 两条通道同一水准）：
//  1. 形状清洗 + 200 字节截断（adminPageErrParamClean）—— 挡住控制字符与超长参数；
//  2. 整体命中 adminErrTexts(c)（shell.FacingNotice：逐字 / 数字归一 / 「候选 + ：」，
//     不是 strings.Contains —— 后者只要夹带一段已知文案就能塞任意前缀后缀）。
//
// **未命中落空串**，与 ?done= / ?saved= 一致：系统文案必须是系统真的说过的话。
// 为什么不落归口文案：那会给手拼参数凭空造出一条「系统提示」（攻击者说的话被系统背书），
// 而写侧本来就会为每一次失败给出一句话 —— 「必须说点什么」由写侧保证，不由读侧兜底。
// 代价是写侧新增文案忘了登记会「静默没有提示」，所以 admin_err_texts_test.go 的
// 写侧对账用例是这一层的一部分，不是附属品。
func adminPageErrText(c *gin.Context, raw string) string {
	cleaned := adminPageErrParamClean(raw)
	if cleaned == "" {
		return ""
	}
	return shell.FacingNotice(cleaned, adminErrTexts(c))
}

// adminErrOrLoad 列表页 .Err 槽的取值：**本次装载失败**优先，其次才是 ?err= 回带（写操作失败）。
//
// 六个管理页（管理员 / 角色 / 权限点 / 菜单 / 部门 / 数据规则）原先在装载失败时
// `c.String(500, pagesMsgAdminGenericFailed)`：浏览器里没有页面，只有一块写着 i18n key 的裸文本 ——
// 侧边栏、筛选框、分页全部消失，运营看到的是 key 而不是一句人话，也无从判断
// 「是我筛错了还是系统坏了」。列表查询失败是服务端问题，不该把**页面本身**一起拿走。
//
// 现在改为降级渲染：空列表 + 一条归口提示，页面结构完好（还能改筛选、点别的菜单）。
// 装载失败与 ?err= 可能同时存在（上一次写失败回带 ?err=、这一次列表又查不出来），
// 装载失败是当前这次请求真实发生的事，必须盖住 URL 里那条旧提示。
//
// 文案与 adminErrParam 同源（同一份 adminenums.AdminFacingMessages 白名单、同一处带 user_id 的
// 结构化日志），所以「归口」这件事在装载失败路径上没有被绕过 —— 原始错误（表名 / 约束名 /
// SQLSTATE / 驱动原文）只进日志，页面上拿到的要么是白名单业务文案，要么是 ErrInternal 归口文案。
func adminErrOrLoad(c *gin.Context, loadErr error) string {
	if loadErr != nil {
		return adminErrParam(c, loadErr)
	}
	return adminPageErrText(c, c.Query("err"))
}

// adminPageErrParamClean 页面提示参数的第一道形状清洗（不含任何文案白名单）。
//
// 清洗只有三步，都是形状判定：
//  1. 去掉全部 C0 控制字符与 DEL（退格、制表、回车、换行一并去掉）：控制字符能把一行提示
//     拆成多行、伪造出第二条「系统消息」的观感，也是终端 / 日志注入的常见载体；
//  2. 去首尾空白（上一步已去掉换行，这里再兜住普通空格与全角空白）；
//  3. 超过 200 字节则按**字节**截断并补省略号 —— 截断点可能落在多字节字符中间，所以回退到
//     最近的 UTF-8 边界，否则页面上会多出一个替换字符。
//
// 为什么截断这一步留在白名单之前（而白名单本身就能拒掉超长串）：它把交给匹配器的串先夹到
// 200 字节，是「不让请求方决定我们扫描多长的输入」这条边界；截断后的串必然对不上任何候选
// （候选都在 adminPageErrParamMaxBytes 以内），于是超长参数的结果同样是空串。
//
// 为什么不放 shell：这是 admin 页面自己的展示协议（?err= 由 admin 的页面路由与 adminErrParam
// 产生），别的模块有各自的页面出口与各自的 return URL 形状；提升到 shell 会变成「给所有
// inbound/http 目录定的通用约定」，而壳层并不认识这些协议。任务 1 新增的
// shell.AdminWriteFailedText 之所以放 shell，是因为它承载的是「响应出口」这一与文案来源无关
// 的形状；本函数承载的是 admin 页面的参数清洗，与 admin 的文案来源绑在一起。
func adminPageErrParamClean(raw string) string {
	cleaned := strings.Map(cleanAdminPageErrRune, raw)
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) <= adminPageErrParamMaxBytes {
		return cleaned
	}
	cut := cleaned[:adminPageErrParamMaxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

// cleanAdminPageErrRune 把控制字符映射成「删除」（strings.Map 的 -1 语义）。
// 单独抽出来是为了让上面那条注释里的「三步」与实现一一对应，而不是埋在一个闭包里。
func cleanAdminPageErrRune(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

// adminBindFail 写出请求绑定失败的响应（400 + MsgBadRequest）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into Go struct field RoleCreateReq.status of type int`），
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

// —— ?done= / ?saved= 两条回执通道的受控出口 ——
//
// ?err= / ?errored= 一侧已在上面收口（adminPageErrText = 形状清洗 + adminErrTexts 白名单
// 整体匹配，未命中落空串）；本段补的是另外两条**同样由页面直接渲染**的查询参数通道：
//
//	· ?done= —— 批量删除「全成功」的结论（adminBulkResultURL / adminI18nBulkDeleteResult
//	  拼出的整句，带计数），渲染在 7 个列表页的 `.Done` 提示条上；
//	· ?saved= —— 词条页保存 / 删除成功后回带的词条身份，渲染在
//	  internal/templates/admin/i18n.html:45 的「已保存：{{.Saved}}」上。
//
// 两条此前都是 `strings.TrimSpace(c.Query("done"))` 原样进模板：手拼一个
// /admin/roles?done=任意文案 就能往页面上塞一条顶着「成功」样式的伪造消息
//（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」，以及超长参数撑破提示条）。
// 查询参数与响应体、模板数据一样**不是可信边界**。
//
// 判据：
//	· ?done= 与写侧**共用同一份模板字面量**，整体匹配（数字归一后相等，因此计数可以变）；
//	· ?saved= 只认「[已删除 ]<key> · <lang>」这一种机器形状（charset 受限，塞不进整句话），
//	  形状的构造与校验同样由写读共用。
// 未命中一律落空串：成功提示没有「必须说点什么」的语义，落归口文案反而会凭空多出一条错误提示。

// adminBulkText 批量结论文案的一条模板 / 名词（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧 adminBulkResultURL / adminI18nBulkDeleteResult 拿它
// Sprintf 出整句，读侧 adminDoneTexts / adminErrTexts 拿**同一个值**、经同一处取词
// （adminBulkTextOf）得到当前语言模板再归一比对。读侧若另抄一份中文字面量，
// 词条一改措辞候选就静默失配 —— 写侧提示可见，页面上却变成「没有这条提示」（两个通道都落空串），
// 既没有报错也没有日志。
type adminBulkText struct{ key, fallback string }

// adminBulkTextOf 取一条批量结论文案的当前语言文本（写侧与读侧**共用这一个取法**）。
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
//
// **写侧与读侧共用这一份**：写侧 adminBulkResultURL 用它 Sprintf 出文案
// （全成功进 ?done=、有跳过进 ?err=），读侧 adminDoneTexts 与 adminErrTexts 取同一份
// 当前语言模板（经 shell.NoticeTemplate 归一）判定 URL 回显。
var (
	adminBulkDoneText    = adminBulkText{adminenums.BulkDoneKey, "已删除 %s 个%s"}
	adminBulkPartialText = adminBulkText{adminenums.BulkPartialKey, "已删除 %s 个%s，%s 个未能删除（受保护或被引用）"}
)

// adminBulkNouns 批量删除文案里出现过的名词（= 各列表页 adminBulkResultURL 的实参）。
//
// 读侧要靠它派生候选（模板里的 %s 是名词而不是计数，不能被归一成占位），所以新增列表页时
// 必须在这里补一个 —— 漏登记的症状是该页「删完了却没有回执」，可见但不致命；不会变成伪造面。
var (
	adminBulkNounAdmin      = adminBulkText{adminenums.BulkNounAdmin, "管理员"}
	adminBulkNounRole       = adminBulkText{adminenums.BulkNounRole, "角色"}
	adminBulkNounPermission = adminBulkText{adminenums.BulkNounPermission, "权限点"}
	adminBulkNounMenu       = adminBulkText{adminenums.BulkNounMenu, "菜单"}
	adminBulkNounDept       = adminBulkText{adminenums.BulkNounDept, "部门"}
	adminBulkNounDatarule   = adminBulkText{adminenums.BulkNounDatarule, "数据规则"}

	adminBulkNouns = []adminBulkText{
		adminBulkNounAdmin, adminBulkNounRole, adminBulkNounPermission,
		adminBulkNounMenu, adminBulkNounDept, adminBulkNounDatarule,
	}
)

// 词条页批量删除的四个结论分支（写读共用）。
//
// 前两条在 skipped == 0 时进 ?done=，后两条进 ?err=（?err= 一侧另有 adminPageErrText 做形状
// 清洗）。拆成两个切片登记，是为了让 ?done= 的候选**不多不少**正好是写侧会放进去的那几条。
var (
	adminI18nBulkNoneSelected = adminBulkText{adminenums.BulkI18nNoneSelected, "没有勾选任何词条，列表未改动。"}
	adminI18nBulkAllDeleted   = adminBulkText{adminenums.BulkI18nAllDeleted, "已删除 %s 条词条（构建时回退到组件包内的中文兜底）。"}
	adminI18nBulkAllSkipped   = adminBulkText{adminenums.BulkI18nAllSkipped, "%s 条词条都未能删除，列表未改动。"}
	adminI18nBulkPartial      = adminBulkText{adminenums.BulkI18nPartial, "已删除 %s 条，%s 条未能删除（可能已被删除）。"}
)

// adminI18nDoneTexts 其中会走 ?done= 的两条（skipped == 0 的两支）；
// 另外两条只在 ?err= 通道上（那一侧由 adminPageErrText 做形状清洗，不在这里登记）。
var adminI18nDoneTexts = []adminBulkText{adminI18nBulkNoneSelected, adminI18nBulkAllDeleted}

// adminDoneTexts 列表页 ?done= 可以原样展示的受控文案（**当前语言**，数字归一后可比）。
//
// 与写侧 adminBulkResultURL / adminI18nBulkDeleteResult 共用 adminBulkTextOf 这一个取法 ——
// 这是「英文页面上真实回执不被判成伪造」的全部依据（详见 adminBulkText 的注释）。
func adminDoneTexts(c *gin.Context) []string {
	out := make([]string, 0, len(adminBulkNouns)+len(adminI18nDoneTexts))
	done := adminBulkTextOf(c, adminBulkDoneText)
	for _, noun := range adminBulkNouns {
		out = append(out, shell.NoticeTemplate(fmt.Sprintf(done, "0", adminBulkTextOf(c, noun))))
	}
	for _, tpl := range adminI18nDoneTexts {
		out = append(out, shell.NoticeTemplate(adminBulkTextOf(c, tpl)))
	}
	return out
}

// adminPageDone 列表页 ?done= 的受控出口：整体命中受控文案才展示，未命中落空串。
//
// 判定用 shell.FacingNotice 的**整体**匹配（逐字 / 数字归一 / 「候选 + ：」），不是
// strings.Contains —— 后者只要夹带一段已知文案就能往页面上塞任意前缀 / 后缀。
func adminPageDone(c *gin.Context, raw string) string {
	return shell.FacingQueryText(raw, "", func(msg string) string {
		return shell.FacingNotice(msg, adminDoneTexts(c))
	})
}

// —— 词条页 ?saved=（「已保存：<key> · <lang>」）——

// adminI18nIdentitySep 词条身份的分隔符（写读共用）。
//
// 选它是因为 key 的 charset 不含空格（见 adminI18nKeyShapeOK），切分因此没有歧义：
// 第一个「 · 」必然是分隔符。
const adminI18nIdentitySep = " · "

// adminI18nDeletedPrefix 删除成功回带身份串的前缀。
const adminI18nDeletedPrefix = "已删除 "

const (
	adminI18nKeyMaxLen  = 120
	adminI18nLangMaxLen = 16
)

// adminI18nKeyShapeOK 词条 key 是否是可安全回显的形状：[A-Za-z0-9._-]，1..120 字节。
//
// 词条 key 本来就是点分标识（admin.i18n.title）；把「能进页面的字符」收死之后，
// 这条通道里就塞不进整句话，也塞不进标签。
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

// adminI18nIdentityShapeOK 身份串是否为 `<key> · <lang>` 形状。
func adminI18nIdentityShapeOK(identity string) bool {
	key, lang, ok := strings.Cut(identity, adminI18nIdentitySep)
	if !ok {
		return false
	}
	return adminI18nKeyShapeOK(key) && adminI18nLangShapeOK(lang)
}

// adminI18nEntryIdentity 词条身份串 `<key> · <lang>`（写侧构造、读侧校验，同一份形状）。
//
// 任一段不合形状就返回空串：写侧拿到空串时回带一个空值（页面不显示身份），
// 而不是把任意文本摊到「已保存：」后面。
func adminI18nEntryIdentity(key, lang string) string {
	key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
	if !adminI18nKeyShapeOK(key) || !adminI18nLangShapeOK(lang) {
		return ""
	}
	return key + adminI18nIdentitySep + lang
}

// adminI18nDeletedIdentity 删除成功回带的身份串：`已删除 <key> · <lang>`。
func adminI18nDeletedIdentity(key, lang string) string {
	identity := adminI18nEntryIdentity(key, lang)
	if identity == "" {
		return ""
	}
	return adminI18nDeletedPrefix + identity
}

// adminPageSaved 词条页 ?saved= 的受控出口：只放行写侧构造的身份形状，未命中落空串。
//
// 未命中不落归口文案：一条「保存成功」的回执没命中时，正确表现是**不显示**，
// 而不是在成功的位置上顶一条错误提示。
func adminPageSaved(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" || len(msg) > shell.NoticeMaxBytes {
		return ""
	}
	identity := strings.TrimPrefix(msg, adminI18nDeletedPrefix)
	if !adminI18nIdentityShapeOK(identity) {
		return ""
	}
	return msg
}
