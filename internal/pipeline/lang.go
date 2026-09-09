package pipeline

// lang.go — 多语言访问路径映射（docs/06-D §5 方案 A）。
//
// 语言是 BuildContext 的维度：同一 Page Document 在 N 个语言下做 N 次独立构建，
// 各自独立 hash、独立激活、独立回滚（docs/06-D §2.2）。因此「逻辑路径 → 访问路径」
// 的映射必须单点实现，禁止调用方各自拼 "/" + lang + path。
//
// 决策（docs/06-D §14 D1/D1'）：
//   - D1 全语言带前缀，默认语言也带：/zh-CN/about、/en-US/about；
//   - D1' 语言根页面映射为 /{lang}/index（方案 ②）。
//
// 为什么语言根不占 /{lang}（§4.4 硬坑，实测确认）：page_routes 只做精确路径唯一、
// 无父子前缀互斥；LocalPublicationStore.Activate 对 /{lang}/about 会先 MkdirAll
// 父目录，若 /{lang} 已是「指向产物目录的符号链接」，MkdirAll 会跟随该链接在
// 不可变产物目录内部建目录，随后落地的符号链接进入 artifacts/{hash}，既污染
// 不可变产物又因上溯层数错算成为悬空链接。改为 /{lang}/index 后语言根与同语言
// 子路径是兄弟节点，冲突面彻底消失。

import (
	"fmt"
	"strings"
)

// maxLangLen 语言代码长度上限（BCP 47 常见形如 zh-CN / en-US / zh-Hans-CN）。
const maxLangLen = 35

// NormalizeLang 规范化并校验语言代码。
//
// 语言码会作为 URL 路径段与文件系统目录名使用，因此必须是严格白名单：
// 字母、数字与连字符，长度 ≤ 35；空串、含 "." / "/" / "\" / 空格 / 控制字符
// 一律拒绝（防路径穿越与保留路径污染）。
func NormalizeLang(raw string) (string, error) {
	lang := strings.TrimSpace(raw)
	if lang == "" {
		return "", fmt.Errorf("语言代码不能为空")
	}
	if len(lang) > maxLangLen {
		return "", fmt.Errorf("语言代码过长（最大 %d）: %q", maxLangLen, raw)
	}
	for _, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return "", fmt.Errorf("语言代码含非法字符 %q: %q", r, raw)
		}
	}
	return lang, nil
}

// LangPath 逻辑访问路径 → 多语言访问路径（默认语言同样带前缀，决策 D1）。
//
//	LangPath("zh-CN", "/")      → "/zh-CN/index"
//	LangPath("zh-CN", "/about") → "/zh-CN/about"
//
// 返回值为规范化路径（NormalizeURL：拒绝保留路径、路径穿越、超长、含空格）。
// 语言或路径非法时返回 error——构建期即失败，绝不产出半截前缀的路径。
func LangPath(lang, path string) (string, error) {
	l, err := NormalizeLang(lang)
	if err != nil {
		return "", err
	}
	// 容错：调用方可能传入未加前导斜杠的逻辑路径（"about"），补一个再规范化；
	// 其余非法形态（穿越、保留路径、空串）仍由 NormalizeURL 拒绝。
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := NormalizeURL(path)
	if err != nil {
		return "", err
	}
	// D1'：语言根不单独占 /{lang}，映射为 /{lang}/index（见文件头说明）。
	if base == "/" {
		base = "/index"
	}
	return NormalizeURL("/" + l + base)
}

// StripLangPath LangPath 的逆运算：/{lang}/path → 逻辑路径（无前缀时原样返回）。
//
// 用于「访问路径 → 草稿逻辑路径」的反查（路由行、诊断信息展示）；
// 语言段非法时不猜测，原样返回输入。
func StripLangPath(lang, path string) string {
	l, err := NormalizeLang(lang)
	if err != nil {
		return path
	}
	p, err := NormalizeURL(path)
	if err != nil {
		return path
	}
	prefix := "/" + l
	if p == prefix || p == prefix+"/index" {
		return "/"
	}
	if strings.HasPrefix(p, prefix+"/") {
		return strings.TrimPrefix(p, prefix)
	}
	return p
}
