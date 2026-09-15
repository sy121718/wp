// seo_verification.go — Google Search Console 站点验证 meta 的构建期注入（审计 SEO-009）。
//
// 与 ga4.go 同一形状（三条硬约束逐条对齐）：
//
//  1. **来源是构建输入**：token 由装配层从 SiteSettings 快照读出后经
//     WithSearchConsoleVerification 传入，builder 不读进程级全局变量。
//  2. **空值零字节**：没有配置 token 时产物里一个字节都不多（与 GA4 / htmx 同口径）。
//  3. **不接受原始 HTML**：content 只接受白名单字符集（base64url：A-Za-z0-9 与 - _），
//     形状不合法的输入被丢弃并记日志，绝不原样拼进 head。
//
// 与 GA4 的唯一实质差异是**大小写**：GA4 测量 ID 官方发放全大写、用户手抄成小写
// 是纯输入噪声，故 ga4.go 归一化时 ToUpper；GSC 的 token 是**大小写敏感的 base64url**，
// 折叠大小写会把一个合法 token 变成另一个（验证必然失败），因此这里只 trim、不改大小写。
package builder

import (
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

// searchConsoleVerificationPattern Search Console 验证 token 形状：
// base64url 字符集（A-Za-z0-9 与 - _），长度 8~128。
//
// 长度不是安全边界，真正的边界是**字符集** —— 它决定了这个值无法逃逸出
// content 属性的引号，也就无法把用户输入变成 HTML 片段（把用户输入当 HTML
// 拼进 head 就是脚本注入）。Google 实际发放的是 43 位无填充 base64url，
// 上下限放宽只是为了不把将来可能变长的 token 挡在门外。
var searchConsoleVerificationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// NormalizeSearchConsoleVerification 归一化并校验 GSC 验证 token：
// 去空白 + 形状判定，不合形返回 ok=false（调用方据此拒绝保存 / 不注入）。
//
// 归一化与校验放在一处：后台表单保存与构建期注入必须用同一份判据，
// 否则会出现「后台存进去了、产物里却没有」这种最难排查的分歧。
func NormalizeSearchConsoleVerification(raw string) (token string, ok bool) {
	token = strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	return token, searchConsoleVerificationPattern.MatchString(token)
}

// ValidSearchConsoleVerification 判断 token 是否合法（后台表单校验用）。
func ValidSearchConsoleVerification(raw string) bool {
	_, ok := NormalizeSearchConsoleVerification(raw)
	return ok
}

// buildSearchConsoleHead 生成注入 <head> 的站点验证 meta。
//
// 空值、非法值都返回空串（零字节注入）；非法值额外记一条告警 ——
// 静默丢弃一个配置项会让运营以为"验证已经装上了"，而 Search Console 里
// 那条验证其实永远不会通过。
func buildSearchConsoleHead(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	token, ok := NormalizeSearchConsoleVerification(trimmed)
	if !ok {
		logger.Scene("build").With("value", trimmed).
			Warn("Search Console 验证 token 形状不合法，已跳过验证 meta 注入（base64url 字符集，8~128 位）")
		return ""
	}
	// token 已过白名单（无引号、无尖括号、无空白），拼进属性值不会逃逸；
	// 除此之外不接受任何外部字符串参与拼接。
	return `<meta name="google-site-verification" content="` + token + `">` + "\n"
}

// WithSearchConsoleVerification 注入站点 Search Console 验证 token（空 = 不注入）。
//
// 由装配层（page / presentation 构建路径）从 SiteSettings 快照读出后传入 ——
// 与 GA4 同一条链路：站点级设置属于构建上下文，读进程级全局变量会破坏确定性构建。
func WithSearchConsoleVerification(token string) CompileOption {
	return func(c *compileConfig) { c.searchConsoleVerification = strings.TrimSpace(token) }
}
