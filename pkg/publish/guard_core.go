package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html"
	"os"
	"path/filepath"
	"strings"

)

const (
	GuardPageFileName = "guard.html"

	GuardMetaFileName = "guard.json"
)

const guardMetaVersion = 1

const GuardBackPlaceholder = "__GW_GUARD_PATH__"

type GuardMeta struct {
	Version int    `json:"version"`
	Type    string `json:"type"`

	Lang string `json:"lang,omitempty"`

	PasswordHash string `json:"passwordHash,omitempty"`

	Fingerprint string `json:"fingerprint"`
}

type ManifestAccess struct {
	Type string `json:"type"`
}

func GuardFingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte("go_wp/access-guard\n" + passwordHash))
	return hex.EncodeToString(sum[:16])
}

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

func GuardPageBody(dir string) (body []byte, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, GuardPageFileName))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

func GuardPageWithPath(body []byte, path string) []byte {
	escaped := html.EscapeString(path)
	return []byte(strings.ReplaceAll(string(body), GuardBackPlaceholder, escaped))
}

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

func guardTextFor(lang string) guardPageText {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return guardPageTexts["zh"]
	}
	return guardPageTexts["en"]
}

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

func renderGuardPageInner(t guardPageText, kind, path string) string {
	if kind != AccessPassword {
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

func RenderGuardPage(meta GuardMeta, path string, showError bool) []byte {
	t := guardTextFor(meta.Lang)
	heading := t.HeadingPwd
	if meta.Type != AccessPassword {
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

func guardPageLang(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "zh-CN"
	}
	return lang
}
