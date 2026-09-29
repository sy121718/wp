package pipeline

// guard.go — AccessGuard（PIPE-6）：受保护页面在产物里的守卫页与守卫元数据。
//
// 为什么位置约定放在 pipeline：访问面（中间件读盘判定）与控制面（构建期写入）
// 必须共享同一份「守卫产物叫什么、放在哪、长什么样」的约定，与 ActiveRoot /
// redirect.json / 404.html 同源（notfound.go 文件头是同一理由）。两边各写一份
// 路径字符串，分叉的表现是「后台勾了密码保护、线上一点反应都没有」这种静默失效。
//
// 守卫产物放**产物目录内**（与 redirect.json 同级），不放激活目录根：
//
//   - 产物是内容寻址的不可变单元，同一份产物被多个路径激活时守卫天然一致；
//   - 激活目录根只能放「站点级真实文件」（siteRootFileNames 白名单），往里加
//     目录会让 AuditActiveLinks 的「非符号链接即异常」判据整片报噪声。
//
// 代价是它落在静态面可读的目录里 —— guard.json 含 bcrypt 哈希，**必须**由访问面
// 显式拒绝直出（builtin.AccessGuardMiddleware 的拦截分支；这条不是可选项）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"go_wp/internal/builder"
)

// 守卫产物在**产物目录内**的文件名。
const (
	// GuardPageFileName 守卫页：未解锁时直出的极简验证页（站点各语言各一份）。
	GuardPageFileName = "guard.html"
	// GuardMetaFileName 守卫元数据：访问面判定与解锁端点比对密码用。
	// **含 bcrypt 哈希**，访问面必须拒绝把它当普通静态文件直出。
	GuardMetaFileName = "guard.json"
)

// guardMetaVersion 守卫元数据版本。格式变了就升版本，老元数据判废（fail closed）。
const guardMetaVersion = 1

// GuardBackPlaceholder 守卫页里「站点路径」占位符。
//
// 产物是内容寻址的：同一份产物可能挂在多个路径上，构建期不可能知道自己的 URL，
// 所以路径只能由访问面在直出时注入。注入必须做 HTML 转义（值来自请求路径）。
const GuardBackPlaceholder = "__GW_GUARD_PATH__"

// GuardMeta 守卫元数据（产物目录内的 guard.json）。
type GuardMeta struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
	// Lang 守卫页语言：解锁端点渲染失败页时按同一语言回话，不必再查站点语言表。
	Lang string `json:"lang,omitempty"`
	// PasswordHash bcrypt 哈希（仅 password 类型）。
	PasswordHash string `json:"passwordHash,omitempty"`
	// Fingerprint 密码指纹：写进解锁 cookie，让「改密码」立刻作废旧解锁。
	Fingerprint string `json:"fingerprint"`
}

// ManifestAccess Manifest 里的访问面标记（docs/03-pipeline.md §4.2 的 AccessGuard 属性）。
//
// 只放类型，**不放 bcrypt 哈希**：Manifest 是公开可读的清单（访问面能直接取到
// manifest.json），把哈希写进去等于把「离线爆破的目标」随产物一起发布。
// 哈希只存在于同目录的 guard.json 与 Page Document。
//
// 另外，本字段刻意**不是** PublicationKind 的新取值：PublicationKind 被
// presentation_ledger.go 与 page_publish_recover.go 按 `Kind != PublicationPage`
// 判等，新增 Kind 会让这两条对账 / 恢复路径把带守卫的页面误判成「非页面」。
// 守卫只能作为页面产物上的**附加标记**。
type ManifestAccess struct {
	// Type password / members（公开页面不写该字段）。
	Type string `json:"type"`
}

// GuardFingerprint 密码指纹（16 字节 hex）。
//
// 用途只有一个：让解锁 cookie 与「当前这一份密码哈希」绑定。改密码 → 哈希变 →
// 指纹变 → 旧解锁 cookie 当场失效。没有它，「改密码」只能等到 cookie 自然过期
// 才生效 —— 而对一个刚把密码从泄漏状态改掉的人来说，那正好是最需要立刻生效的时刻。
//
// 前缀是领域分隔：同一个 sha256 在别处也被用作内容寻址，加盐前缀保证两类值
// 不可能相等（否则「指纹」可能恰好等于某个产物 hash，读起来会误导）。
func GuardFingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte("go_wp/access-guard\n" + passwordHash))
	return hex.EncodeToString(sum[:16])
}

// ReadGuardMeta 读取产物目录里的守卫元数据。
//
// 返回值语义（访问面按此判定，务必保持）：
//   - (nil, nil)：该产物**没有**守卫（guard.json 不存在）→ 放行；
//   - (meta, nil)：有守卫 → 按 meta.Type 判身份；
//   - (nil, err)：文件存在但读不出来 / 解析不了 → **fail closed**（当作受限处理）。
//
// 第三条与 redirect.json 刻意相反：重定向解析失败只是少一次 301，内容仍然正确；
// 守卫解析失败若按「无守卫」放行，等于把受限内容直接交给未授权访客 —— 不可逆。
// 两个方向的代价不对称，判据就不能对称。
func ReadGuardMeta(dir string) (*GuardMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, GuardMetaFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取守卫元数据失败: %w", err)
	}
	var m GuardMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("守卫元数据解析失败: %w", err)
	}
	if m.Type == "" {
		return nil, fmt.Errorf("守卫元数据缺少类型")
	}
	return &m, nil
}

// GuardPageBody 读取产物目录里的守卫页。
//
// ok=false 表示守卫页缺失或为空：访问面必须退回**内置兜底页**，绝不能因此放行
// 内容（守卫页读不到是「产物不完整」，不是「本页公开」）。
func GuardPageBody(dir string) (body []byte, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, GuardPageFileName))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// GuardPageWithPath 把守卫页里的路径占位符替换为实际站点路径。
//
// path 来自请求（经访问面归一），因此必须 HTML 转义后再落进 value 属性 ——
// 否则 `/a"onmouseover=...` 这类路径就是一处反射型 XSS。
func GuardPageWithPath(body []byte, path string) []byte {
	escaped := html.EscapeString(path)
	return []byte(strings.ReplaceAll(string(body), GuardBackPlaceholder, escaped))
}

// BuildGuardEntries 按页面访问设置生成守卫产物（构建期调用）。
//
// meta 为 nil 表示该页面公开：不生成任何守卫文件（产物字节与没有这个能力时
// 逐字节一致 —— 确定性构建不变量 5；存量文档没有 access 字段，走的就是这条路）。
//
// 生成的两个文件的哈希都会进 Manifest.Files（由 NewArtifactWithEntries 统一登记），
// 因此「改了密码」会改变产物 hash → 新产物目录 → 新 guard.json。这条链路是必需的：
// 若密码哈希不影响产物 hash，PutArtifact 会命中「同 hash 目录已存在」的幂等分支，
// 新密码永远写不进去，表现是「改了密码还是旧密码生效」。
func BuildGuardEntries(access *builder.AccessGuardSettings, lang string) (files map[string][]byte, meta *ManifestAccess) {
	if access.IsPublic() {
		return nil, nil
	}
	kind := access.TypeOrDefault()
	gm := GuardMeta{
		Version: guardMetaVersion,
		Type:    kind,
		Lang:    lang,
	}
	if kind == builder.AccessPassword {
		gm.PasswordHash = strings.TrimSpace(access.PasswordHash)
		gm.Fingerprint = GuardFingerprint(gm.PasswordHash)
	}
	// 未知类型（白名单在 validateSettings 拦过一道，这里是纵深）：**照样生成守卫**，
	// 而不是「不认识就放行」。访问面对未知类型的判据是 false（见 guardAllows），
	// 于是页面变成「谁都进不去」—— 比「谁都进得去」正确。
	metaJSON, err := json.Marshal(gm)
	if err != nil {
		// GuardMeta 全是字符串与整数，编码不会失败；真失败说明结构被改坏了，
		// 此时宁可不生成守卫也不能生成半个（调用方按 nil 当公开）。
		return nil, nil
	}
	return map[string][]byte{
		GuardPageFileName: RenderGuardPage(gm, GuardBackPlaceholder, false),
		GuardMetaFileName: metaJSON,
	}, &ManifestAccess{Type: kind}
}

// PageGuardEntries 从冻结的 Page Document 取访问设置并生成守卫产物。
//
// 解析失败返回 error（调用方按构建失败处理），**不是**「当公开放行」：
// 走到这里说明编译器已经产出过 HTML，而默认编译器用的是同一份 DocJSON 且内部
// 先 ParsePage —— 解析不了意味着这条构建链本身不成立（自定义编译器注入了
// 非 Page 文档）。此时按公开出产物，等于把「不知道权限的页面」当公开页发布。
func PageGuardEntries(docJSON []byte, lang string) (map[string][]byte, *ManifestAccess, error) {
	page, err := builder.ParsePage(docJSON)
	if err != nil {
		return nil, nil, err
	}
	if page == nil {
		return nil, nil, fmt.Errorf("页面文档为空")
	}
	files, meta := BuildGuardEntries(page.Settings.Access, lang)
	return files, meta, nil
}

// guardPageText 守卫页内置文案（按语言）。
type guardPageText struct {
	Title      string
	HeadingPwd string
	HeadingMem string
	HintPwd    string
	HintMem    string
	Password   string
	Submit     string
	Error      string
}

// guardPageTexts 首批语言的内置文案。
//
// 为什么不做成后台可编辑的词条：守卫页在**访问面**直出，必须在「站点配置读不出来、
// Redis 不可用、站点语言表还没发布」的最坏情况下也能渲染 —— 它不能依赖任何数据源。
// 站点语言表里没有的语言回退英文（守卫页上的每个字都只是「告诉人这里要密码」）。
var guardPageTexts = map[string]guardPageText{
	"zh": {
		Title:      "受保护的页面",
		HeadingPwd: "此页面受密码保护",
		HeadingMem: "此页面仅对已登录用户开放",
		HintPwd:    "请输入访问密码后继续。",
		HintMem:    "请先登录站点账号，然后回到这个页面。",
		Password:   "访问密码",
		Submit:     "进入",
		Error:      "密码不正确，请重试。",
	},
	"en": {
		Title:      "Protected page",
		HeadingPwd: "This page is password protected",
		HeadingMem: "This page is for signed-in visitors only",
		HintPwd:    "Enter the access password to continue.",
		HintMem:    "Please sign in to your account, then come back to this page.",
		Password:   "Access password",
		Submit:     "Enter",
		Error:      "Incorrect password. Please try again.",
	},
}

// guardTextFor 按语言码取内置文案（前缀匹配 zh → 中文，其余英文）。
func guardTextFor(lang string) guardPageText {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return guardPageTexts["zh"]
	}
	return guardPageTexts["en"]
}

// guardPageHTML 守卫页骨架。
//
// 独立成页、**不带站点头尾**：它要在「未解锁」时就渲染出来，而站点导航/页脚本身
// 可能来自受限内容或需要查库的片段。守卫页只做一件事：挡住内容并收密码。
//
// 内联样式而不是引用站点 CSS：站点样式表在受限页面上同样不该被当作前置依赖
// （它是产物的一部分，但守卫页必须在 CSS 404 时仍然可读、可提交）。
// noindex 是硬要求：少了它搜索引擎会把「要密码」这一页收进索引，
// 用户搜到的就是这个空壳。样式跟随系统深浅色，宽度用 min(100%, …) 适配三端。
const guardPageHTML = `<!DOCTYPE html>
<html lang="{{GW_LANG}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{GW_TITLE}}</title>
<style>
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;
font-family:system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue","PingFang SC","Microsoft YaHei",sans-serif;
background:#f5f6f8;color:#1c1e21}
.gw-guard{width:min(100%,420px);background:#fff;border:1px solid #e3e5e8;border-radius:14px;
padding:28px 24px;box-shadow:0 8px 28px rgba(16,24,40,.06)}
.gw-guard h1{margin:0 0 10px;font-size:1.15rem;line-height:1.4}
.gw-guard p{margin:0 0 18px;font-size:.9rem;line-height:1.6;color:#5b6472}
.gw-guard label{display:block;margin-bottom:6px;font-size:.82rem;color:#5b6472}
.gw-guard input[type=password]{width:100%;padding:11px 12px;font-size:1rem;border:1px solid #cfd4da;
border-radius:9px;background:#fff;color:inherit;min-height:44px}
.gw-guard input[type=password]:focus{outline:2px solid #2f6fed;outline-offset:1px;border-color:#2f6fed}
.gw-guard button{margin-top:14px;width:100%;padding:12px;font-size:.95rem;border:0;border-radius:9px;
background:#2f6fed;color:#fff;cursor:pointer;min-height:44px}
.gw-guard button:hover{background:#2559c7}
.gw-guard-err{margin:0 0 14px;padding:9px 11px;border-radius:8px;background:#fdecec;color:#b3261e;font-size:.85rem}
@media (prefers-color-scheme:dark){
body{background:#14161a;color:#e8eaed}
.gw-guard{background:#1d2025;border-color:#2c3138;box-shadow:none}
.gw-guard p,.gw-guard label{color:#a3abb6}
.gw-guard input[type=password]{background:#14161a;border-color:#3a4048}
.gw-guard-err{background:#3a1d1c;color:#f6b3ae}
}
</style>
</head>
<body>
<main class="gw-guard">
<h1>{{GW_HEADING}}</h1>
{{GW_ERROR}}{{GW_BODY}}
</main>
</body>
</html>
`

// renderGuardPageInner 生成 {{GW_BODY}} 片段（密码表单 / 登录提示）。
func renderGuardPageInner(t guardPageText, kind, path string) string {
	if kind != builder.AccessPassword {
		return "<p>" + t.HintMem + "</p>"
	}
	return "<p>" + t.HintPwd + "</p>\n" +
		`<form method="post" action="/access/unlock">` + "\n" +
		`<input type="hidden" name="path" value="` + path + `">` + "\n" +
		`<label for="gw-guard-password">` + t.Password + `</label>` + "\n" +
		`<input id="gw-guard-password" type="password" name="password" autocomplete="current-password" required maxlength="72">` + "\n" +
		"<button type=\"submit\">" + t.Submit + "</button>\n" +
		"</form>"
}

// RenderGuardPage 渲染守卫页字节。
//
// path 为空时用 GuardBackPlaceholder（构建期写入产物时），否则按给定路径 HTML 转义后
// 直接落值（解锁端点渲染「密码不正确」时自己知道路径）。
// showError 只在解锁失败后由解锁端点使用。
func RenderGuardPage(meta GuardMeta, path string, showError bool) []byte {
	t := guardTextFor(meta.Lang)
	heading := t.HeadingPwd
	if meta.Type != builder.AccessPassword {
		heading = t.HeadingMem
	}
	errBlock := ""
	if showError {
		errBlock = "<p class=\"gw-guard-err\">" + t.Error + "</p>\n"
	}
	value := GuardBackPlaceholder
	if path != "" {
		value = html.EscapeString(path)
	}
	out := guardPageHTML
	out = strings.ReplaceAll(out, "{{GW_LANG}}", html.EscapeString(guardPageLang(meta.Lang)))
	out = strings.ReplaceAll(out, "{{GW_TITLE}}", html.EscapeString(t.Title))
	out = strings.ReplaceAll(out, "{{GW_HEADING}}", html.EscapeString(heading))
	out = strings.ReplaceAll(out, "{{GW_ERROR}}", errBlock)
	out = strings.ReplaceAll(out, "{{GW_BODY}}", renderGuardPageInner(t, meta.Type, value))
	return []byte(out)
}

// guardPageLang 守卫页 <html lang> 取值（空则 zh-CN，与站点默认语言口径一致）。
func guardPageLang(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "zh-CN"
	}
	return lang
}
