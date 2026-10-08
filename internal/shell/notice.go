package shell

// notice.go — 后台列表页**读侧回执**（?err= / ?done= / ?ok=）的受控形状判定。
//
// 背景：写侧早已把错误收敛成受控文案（各模块的 XxxErrText / shell.BulkIDsFacingText），
// 但读侧此前是把 query 参数**原样**塞进模板（`"Err": c.Query("err")`）——
// 任何人手拼一个 /admin/xxx?err=任意文案 就能往页面上塞一条顶着「上一次操作未完成」
// 样式的伪造消息（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」，
// 以及超长参数会把提示条撑破）。查询参数与响应体、模板数据一样**不是可信边界**。
//
// 本文件提供三个形状工具（判定一律要求**整体匹配**，不是「包含」）：
//
//   - NormalizeNoticeDigits —— 把连续数字归一成占位 "0"，让「已删除 12 个块。」
//     与模板「已删除 0 个块。」可比；批量结论文案里只有计数是变化的，其余逐字固定。
//   - NoticeTemplate —— 把受控模板里的 %s / %d 占位换成 "0"，与上式配套
//     （shell 的批量上限提示就是 Sprintf 出来的）。
//   - FacingNotice —— 命中受控文案集合返回原文，未命中返回空串。
//
// 调用方（各模块 inbound/http）只需给出**当前语言下的候选文案**，
// 再经 shell.FacingQueryText 落 fallback（错误提示落归口文案、成功提示落空串）。

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// NoticeMaxBytes 单条回执文案允许进模板的最大字节数。
//
// 512 字节：受控文案本身都在几十字节量级，这个上限只用来挡住
// 「一次提交几十 KB 的 err= 参数」把提示条撑破，不参与语义判定。
const NoticeMaxBytes = 512

// NoticePlaceholder 数字归一后的占位字符（模板与实际文案都用它）。
const NoticePlaceholder = "0"

// NormalizeNoticeDigits 把字符串里的连续 ASCII 数字归一成占位 "0"。
//
// 为什么需要：批量结论文案由服务端用 fmt.Sprintf 拼出（「已删除 3 个块。」），
// 计数是唯一变化的部分。归一后「实际文案」与「写侧声明的模板」只要逐字相等，
// 就说明整句话（除计数外）都出自本仓库 —— 手拼的 ?err= / ?done= 不可能命中。
func NormalizeNoticeDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inDigits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !inDigits {
				b.WriteString(NoticePlaceholder)
			}
			inDigits = true
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}
	return b.String()
}

// NoticeTemplate 把受控文案模板归一成可比形态：%s / %d / %v 占位 → "0"，再做数字归一。
//
// 与 NormalizeNoticeDigits 配套：模板里既可能有 %d（fmt.Sprintf 直接填数字），
// 也可能有 %s（shell 的批量上限提示用 strconv.Itoa 填进去），两种都归一到同一个占位。
func NoticeTemplate(tpl string) string {
	r := strings.NewReplacer("%s", NoticePlaceholder, "%d", NoticePlaceholder, "%v", NoticePlaceholder)
	return NormalizeNoticeDigits(r.Replace(tpl))
}

// FacingNotice 受控回执文案的形状判定：命中候选集合返回 TrimSpace 后的原文，未命中返回空串。
//
// 命中的三种形态（都是整体匹配）：
//
//  1. 与候选文案逐字相等；
//  2. 与候选文案数字归一后相等（计数不同）；
//  3. 以「候选文案 + ：」或「候选文案 + ": "」开头 —— 服务端会在业务文案后补一句
//     定位信息（如「该外部编码在本仓已挂到另一个商品上：外部编码 xxx」）。
//
// 未命中返回空串而不是 fallback：错误提示与成功提示的 fallback 不同
// （前者落 shell.PageInternalText(c)，后者落空串），由调用方经
// shell.FacingQueryText 决定 —— 免得这一层替它们猜。
//
// 超过 NoticeMaxBytes 一律判未命中：形态正确但长得离谱的「文案」必然是伪造的。
func FacingNotice(raw string, candidates []string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" || len(msg) > NoticeMaxBytes {
		return ""
	}
	if noticeMatches(msg, candidates) {
		return msg
	}
	return ""
}

// noticeDetailPrefixes 业务文案后接定位信息的分隔符（中英各一，与写侧拼装一致）。
var noticeDetailPrefixes = []string{"：", ": "}

// noticeMatches 判定 msg 是否命中候选文案集合（形态 1/2/3）。
func noticeMatches(msg string, candidates []string) bool {
	norm := NormalizeNoticeDigits(msg)
	for _, cand := range candidates {
		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		if msg == cand || norm == NormalizeNoticeDigits(cand) {
			return true
		}
		for _, sep := range noticeDetailPrefixes {
			if strings.HasPrefix(msg, cand+sep) {
				return true
			}
			// 形态 3 也做数字归一：带计数的文案后面如果跟了定位信息，
			// 逐字比较会因数字不同而失配（候选是模板、raw 是实际计数）。
			if strings.HasPrefix(norm, NormalizeNoticeDigits(cand+sep)) {
				return true
			}
		}
	}
	return false
}

// —— 语言切换回跳参数的收敛 ——

// langRedirectMaxBytes 语言切换表单 redirect 隐藏域允许的最大字节数。
//
// 512 字节：正常后台页 URI（路径 + 少量筛选参数）远小于它；
// 这个上限挡住的是「让管理员打开一个 query 长到几 KB 的页面，语言切换表单里
// 就嵌着几 KB 的 redirect，提交后 302 到一个超长 URL」这条路。
const langRedirectMaxBytes = 512

// LangRedirectPath 语言回跳地址的**唯一一份判据**：是站内相对路径就返回原串，否则返回空串。
//
// 判据（四条，逐条都有对应的攻击面）：
//
//  1. 以 "/" 开头 —— 相对路径才谈得上「回站内」；`admin/roles` 这种不以 "/" 开头的值
//     会被浏览器按当前路径拼接，落点不是调用方以为的那个页面。
//  2. 不含 "//" —— 挡协议相对跳转（`//evil.example.com/x` 在 Location 里等价于
//     `https://evil.example.com/x`），这是开放重定向最常见的一手。
//  3. 不含反斜杠 —— 部分客户端把 `\` 当路径分隔符，`/\\evil.example.com` 可能被
//     归一成 `//evil.example.com`；经 query 解码回来的值还能带它。
//  4. 不含 C0 控制字符 / DEL，且不超过 langRedirectMaxBytes（512 字节）——
//     换行能把 `Location` 头拆成两行，超长值会把语言切换表单的隐藏域撑成好几 KB。
//
// **返回空串表示拒绝**，回落语义由调用方自己决定（渲染侧回落当前请求路径、再回落 "/"；
// 消费侧回落 "/"）—— 判据只有一份，各处的兜底不同是各自的事。
//
// 入参是字符串而不是 *gin.Context，因为两侧的**来源不同**：渲染侧拿到的是当前请求的
// RequestURI（已被转义，天然不含反斜杠与控制字符），消费侧拿到的是 query 里带回来、
// 已经解码的值（`\`、换行、NUL 都能出现）。导出它就是为了让消费侧不必再抄一份判据 ——
// 此前 admin 的 adminLangRedirectAllowed 是重抄的第二份，靠一条对照测试防漂移。
func LangRedirectPath(raw string) string {
	if !sameOriginPath(raw) || len(raw) > langRedirectMaxBytes {
		return ""
	}
	return raw
}

// LocalReturnPath 通用「站内相对路径」白名单（returnUrl 这类回跳参数用）。
//
// 与语言切换回跳**共用同一份判据**（LangRedirectPath）：以 "/" 开头、不含 "//"、
// 不含反斜杠或控制字符、且不超过 512 字节；不合规一律返回空串。
// 为什么复用而不是各写一份：开放重定向要挡的就是同一批形态（协议相对 URL
// "//evil.example.com"、绝对 URL、"\" 归一成的双斜杠、换行拆头），分支写两份必然漂移，
// 而漏掉的那份只会在某次安全复核里才被发现。**返回空串表示拒绝**，兜底由调用方决定。
func LocalReturnPath(raw string) string { return LangRedirectPath(raw) }

// LangRedirect 语言切换表单 redirect 隐藏域的取值。
//
// 取值链（改前先读过）：admin/layout.html 的
// `<input type="hidden" name="redirect" value="{{ .["lang_redirect"] }}">` →
// shell.Prepare → injectI18n → 本函数；提交给 GET /admin/lang，
// 由消费端（admin 的 adminSafeLangRedirect）再次校验为站内路径后 302。
//
// 收敛两件事：
//
//  1. **同源 + 长度上限** —— 判据全部在 LangRedirectPath（两侧共用的那一份）；
//     RequestURI 正常总是相对路径，这里是防御性收口：「回跳目标是不是站内」
//     不该只由消费端一家把关。
//  2. **超限/非法时的回落** —— 丢弃整个 URI，回落到当前请求的**路径**（丢掉 query）。
//     回跳仍然回到发起页，只是不再带上那串可能超长的参数；路径本身也异常时回首页 "/"。
//
// 为什么不用 requestURI 直接透出：requestURI 还被日志（bulk.go 的 path 字段）复用，
// 日志要的是真实 URI；渲染进 HTML 的隐藏域才是无界外泄面，两者不能共用一份取值。
func LangRedirect(c *gin.Context) string {
	if c == nil {
		return adminHomePath
	}
	if p := LangRedirectPath(requestURI(c)); p != "" {
		return p
	}
	return requestPathOrRoot(c)
}

// sameOriginPath 判定是否「站内相对路径」：以 "/" 开头、不含 "//"、不含反斜杠或控制字符。
//
// LangRedirectPath 的私有实现（长度上限在那一层补）。不导出是因为**长度属于协议的一部分**，
// 单独放出一个「不含长度判据」的入口只会给后来者多一个抄错的形状。
func sameOriginPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.Contains(p, "//") || strings.Contains(p, "\\") {
		return false
	}
	return !hasControlRune(p)
}

// hasControlRune 是否含 C0 控制字符或 DEL（换行 / 制表 / 退格等）。
func hasControlRune(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// requestPathOrRoot 回落到当前请求的路径（丢掉 query）：回跳仍回到发起页；
// 路径本身异常（空 / 非同源 / 超长）时回首页 "/"。
func requestPathOrRoot(c *gin.Context) string {
	if c != nil && c.Request != nil && c.Request.URL != nil {
		if p := c.Request.URL.Path; sameOriginPath(p) && len(p) <= langRedirectMaxBytes {
			return p
		}
	}
	return adminHomePath
}
