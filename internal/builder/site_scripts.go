// site_scripts.go — 站点自定义 Head / Body 代码注入（PIPE-8）的构建期装配。
//
// 与 ga4.go / seo_verification.go 同一形状（来源是构建输入、空值零字节、服务端拼装），
// 但**有一处根本差别必须写清**：GA4 与 GSC 把外部值拼进属性 / JS 字面量，所以只能
// 接受白名单字符集；而自定义代码片段本身就是 HTML/JS 文本，无法穷举其形状 ——
// 因此这里选择**原样注入**，安全责任由信任边界承担，不靠字符集白名单。
//
// 以下四段是本项的安全论证（审计 PIPE-8 标注「需安全白名单评审」，评审要看的就是这里）。
//
//  1. **信任边界**：这两个值只能从后台站点设置页写入（POST /admin/settings/save），
//     该路由走 Session + CSRF + Casbin 三层链，权限点复用既有的 `/api/project/update`
//     —— 不新增权限点、不新增越权面。公开面（访客页面、Runtime Fragment、
//     POST /analytics/collect）没有任何写入这两个字段的路径，构建期只把它当字符串读。
//     因此「能写这两段代码的人」= 「能改站点配置的人」，与「能改主题 CSS、能填自定义
//     404 页 HTML、能上传媒体文件」同一级别的既有能力。
//
//  2. **为什么允许原样注入**（而不是白名单或转义）：这是管理员为自己的站点安装第三方
//     统计与营销脚本的产品功能（等价于 WordPress 的 wp_head / 各类标签管理器）。
//     白名单标签与属性集必然漏项 —— GA4、Facebook Pixel、Clarity、Hotjar、Intercom、
//     Crisp 各有各的写法，且随时新增；转义则把脚本变成页面上的可见文本，功能直接失效
//     （`<script>` 被写成 `&lt;script&gt;` 就只是一句话，而不是代码）。
//     **代价**：管理员可以把任意外部脚本装进自己的站点产物 —— 包括他自己信任的、
//     后来变节的第三方 CDN。这个代价由站点所有者承担（他能配就能撤：清空即零字节注入），
//     与「CMS 允许作者填任意正文 HTML」是同一笔账。
//
//  3. **防的是什么**（三条都不是「防管理员」，而是防误操作与旁路）：
//     ① 破坏产物结构：注入点是**受控的独立行** —— head 片段落在 `</head>` 之前最后一行的
//     位置，body 片段落在 `</body>` 之前，两者都不进任何属性值、不在 <title> 或
//     JSON-LD 的上下文里，因此片段不会被当作属性值或 JSON 文本解析（这正是「注入到
//     错误上下文」这一类缺陷的根因）。此外**拒绝带结构性标签的输入**
//     （`<!DOCTYPE>` / `<html>` / `<head>` / `<body>` 及其闭合形式，见
//     siteScriptStructuralTagPattern）：那类输入一定是「整页 HTML 误粘贴」，
//     产物里出现第二套骨架之后，症状是所有站点样式静默失效 —— 与其让它静默坏页，
//     不如在保存时就拒绝。
//     ② 非管理员路径进入的 XSS：没有第二条写入路径（见 1）。公开面不回显这两个字段，
//     也不把它写进任何 HTTP 响应（只烘进静态产物字节，由静态服务器直出）。
//     ③ 把未转义的用户数据拼进去：片段是**整段原样放置**，服务端不往它内部拼任何
//     运行期数据 —— 没有任何字符串插值发生（对比 GA4：测量 ID 必须拼进 src 属性与
//     JS 字符串字面量，所以那里字符集白名单是硬需求）。构建期数据（工程 ID / 语言）
//     走 json.Marshal 生成的独立字面量（buildTrackConfig），不与用户脚本混排。
//
//  4. **产物字节的确定性**：片段只来自 SiteSettings 快照（构建输入），builder 不读
//     进程级全局变量、不读时间戳；空值与非法值一律零字节注入 —— 同一 Page Document +
//     BuildContext 仍然产出同一字节（AGENTS.md 不变量 5）。
//
//  5. **作用域是站点级**（projects.settings，一处配置全站生效），与 GA4 / GSC 同链路、
//     同作用域。文档 0-A2 §2.3 描述的是**页面级**注入（每页可不同），它需要
//     PageSettings 侧字段 + 页面设置面板（0-A2 §5 提到的 internal/builder/settings.go
//     扩展点），是另一条待接的线 —— 本批不做，避免出现「有存储字段、没有配置入口」的半成品。
package builder

import (
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

// maxSiteScriptBytes 单个注入片段的上限（16 KiB）。
//
// 它不是安全边界（能写脚本本身就已经是全权），而是**这一列的读取代价**：
// 两个片段都存在 projects.settings 里，而站点设置每次读取都整列反序列化
// （后台页面每次打开、每条构建路径各一次）。上限与先例同一条思路 ——
// 自定义 404 页存在同一列、上限 32 KiB（那是整份 HTML 文档，正文更长）。
const maxSiteScriptBytes = 16 * 1024

// siteScriptStructuralTagPattern 结构性标签（`<!DOCTYPE>` / `<html>` / `<head>` / `<body>`
// 及其闭合形式，大小写不敏感，允许 `<` 与标签名之间有空白）。
//
// 为什么拒这一类而不是拒「未闭合标签」：未闭合标签在 HTML 里是**常态**
// （`<script>` 自身成对、`<style>` 成对，但 `<br>` / `<img>` 从来不必闭合），
// 用正则判「是否配对」一定误伤；而结构性标签是明确无歧义的信号 ——
// 它们只可能来自「整页 HTML 误粘贴」，合法第三方片段里一个都不会出现。
//
// 判据是**词边界**（`\b`）而不是前缀：`<header>` / `<html5-embed>` 这类组件标签
// 不会被误伤（`head` 与 `header` 之间没有词边界），这是这条规则敢上生产的前提。
var siteScriptStructuralTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*(!doctype|html|head|body)\b`)

// NormalizeHeadScripts 归一化并校验自定义 Head 代码：只去两端空白 —— 片段内部
// 一个字节都不改（改写管理员的脚本体是另一种静默失效：他装的脚本会以他没写过的方式运行）。
//
// 形状不合法的三种情形（空值、超长、含结构性标签）返回 ok=false，
// 调用方据此拒绝保存（后台）/ 不注入（构建期）。
//
// 与 GA4 相同的一点：**保存与注入共用这一份判据**，否则会出现「后台存进去了、
// 产物里却没有」这种最难排查的分歧。
func NormalizeHeadScripts(raw string) (script string, ok bool) {
	return normalizeSiteScript(raw)
}

// NormalizeBodyScripts 归一化并校验自定义 Body 代码。
//
// 规则与 Head 完全相同（同上），分成两个导出函数只为了调用点能表明自己在校验**哪个字段** ——
// 两者将来的形状要求也可能分叉（如 body 侧将来要禁 `</html>` 之外的更多骨架标签）。
func NormalizeBodyScripts(raw string) (script string, ok bool) {
	return normalizeSiteScript(raw)
}

// normalizeSiteScript 两个片段共用的归一化 + 形状校验（唯一出口）。
func normalizeSiteScript(raw string) (script string, ok bool) {
	script = strings.TrimSpace(raw)
	if script == "" {
		return "", false
	}
	if len(script) > maxSiteScriptBytes {
		return "", false
	}
	if siteScriptStructuralTagPattern.MatchString(script) {
		return "", false
	}
	return script, true
}

// buildHeadScripts 生成注入 <head> 末尾（</head> 之前）的自定义代码片段。
//
// 空值、非法值都返回空串（零字节注入）；非法值额外记一条告警 ——
// 静默丢弃一个配置项会让运营以为「统计代码已经装上了」，而报表其实是空的。
// 与 GA4 / GSC 同一口径：非法值在保存时已被拒绝，这里兜的是历史数据与直接改库的旁路。
func buildHeadScripts(raw string) string {
	return buildSiteScript(raw, "Head")
}

// buildBodyScripts 生成注入 </body> 之前的自定义代码片段（口径同上）。
func buildBodyScripts(raw string) string {
	return buildSiteScript(raw, "Body")
}

// buildSiteScript 两个片段共用的构建逻辑（kind 只用于日志措辞）。
//
// 返回的是**原样片段**（已去两端空白），不含首尾换行 —— 换行由 document.jet
// 提供（`{{ if .X }}{{ .X | unsafe }}\n{{ end }}`），这样空值时连一个换行都不多
// （空值零字节注入的判据是「产物字节与未配置时逐字节相同」，多一个空行就破了它）。
func buildSiteScript(raw, kind string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	script, ok := normalizeSiteScript(trimmed)
	if !ok {
		logger.Scene("build").With("kind", kind).With("value", siteScriptLogPreview(trimmed)).
			Warn("自定义注入代码形状不合法，已跳过注入（不得含 <!DOCTYPE> / <html> / <head> / <body> 结构性标签，且不超过 16 KiB）")
		return ""
	}
	return script
}

// siteScriptLogPreview 日志里只记片段开头（片段可达 16 KiB，整段进日志会把上下文淹掉）。
//
// 按 rune 截断而不是按字节：按字节切会把一个多字节字符拦腰截断，
// 日志里出现乱码反而更难读。`…` 明确表示「这里被截断了」。
func siteScriptLogPreview(s string) string {
	const maxRunes = 120
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// WithHeadScripts 注入站点自定义 Head 代码（空 = 不注入）。
//
// 由装配层（page / presentation 构建路径）从 SiteSettings 快照读出后传入 ——
// 站点级设置属于构建上下文的一部分，读进程级全局变量会破坏确定性构建
// （同一文档在两次构建之间拿到不同的脚本，产物字节就不再由输入唯一确定）。
func WithHeadScripts(script string) CompileOption {
	return func(c *compileConfig) { c.headScripts = strings.TrimSpace(script) }
}

// WithBodyScripts 注入站点自定义 Body 代码（空 = 不注入，口径同上）。
func WithBodyScripts(script string) CompileOption {
	return func(c *compileConfig) { c.bodyScripts = strings.TrimSpace(script) }
}
