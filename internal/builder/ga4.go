// ga4.go — 站点统计代码（GA4 / gtag）的构建期注入。
//
// 三条硬约束（BIZ-8）：
//
//  1. **来源是构建输入**：测量 ID 由装配层从 SiteSettings 快照读出后经
//     WithGA4MeasurementID 传入，builder 不读进程级全局变量、不读时间戳 ——
//     同一 Page Document + BuildContext → 同一字节（确定性构建不变量）。
//  2. **空值零字节**：没有配置测量 ID 时产物里一个字节都不多（与 htmx 按需注入同口径）。
//  3. **不接受原始 HTML**：head 里那两段 script 由服务端拼装，测量 ID 只能出现在
//     白名单字符集（A-Z0-9 与连字符）的位置上；形状不合法的输入被丢弃并记日志，
//     绝不会被当作 HTML 片段塞进 head（把用户输入当 HTML 拼进 head 就是脚本注入）。
package builder

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

// ga4MeasurementIDPattern GA4 测量 ID 形状：G- 前缀 + 大写字母数字。
//
// 官方发放的是 G-XXXXXXXXXX（10 位），这里放宽到 4~20 位：长度不是安全边界，
// 真正的边界是**字符集**（只允许 A-Z0-9 与固定前缀里的连字符），
// 它决定了这个值无法逃逸出 src 属性与 JS 字符串字面量。
var ga4MeasurementIDPattern = regexp.MustCompile(`^G-[A-Z0-9]{4,20}$`)

// NormalizeGA4MeasurementID 归一化并校验 GA4 测量 ID：
// 去空白 + 转大写（官方发放全大写，用户手抄成小写是常见的输入噪声），
// 形状不合法返回 ok=false（调用方据此拒绝保存 / 不注入）。
//
// 归一化与校验放在一处：后台表单保存与构建期注入必须用同一份判据，
// 否则会出现「后台存进去了、产物里却没有」这种最难排查的分歧。
func NormalizeGA4MeasurementID(raw string) (id string, ok bool) {
	id = strings.ToUpper(strings.TrimSpace(raw))
	if id == "" {
		return "", false
	}
	return id, ga4MeasurementIDPattern.MatchString(id)
}

// buildGA4Head 生成注入 <head> 的 GA4 片段。
//
// 空值、非法值都返回空串（零字节注入）；非法值额外记一条告警 ——
// 静默丢弃一个配置项会让运营以为"代码已经装上了"，而统计其实是空的。
func buildGA4Head(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	id, ok := NormalizeGA4MeasurementID(trimmed)
	if !ok {
		logger.Scene("build").With("value", trimmed).
			Warn("GA4 测量 ID 形状不合法，已跳过统计代码注入（形如 G-XXXXXXXXXX）")
		return ""
	}
	// ID 已过白名单（A-Z0-9 与连字符），因此拼进属性与 JS 字符串都是安全的；
	// 除此之外不接受任何外部字符串参与拼接。
	var sb strings.Builder
	sb.WriteString(`<script async src="https://www.googletagmanager.com/gtag/js?id=`)
	sb.WriteString(id)
	sb.WriteString(`"></script>` + "\n")
	sb.WriteString("<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}" +
		"gtag('js',new Date());gtag('config','" + id + "');</script>")
	return sb.String()
}

// WithGA4MeasurementID 注入站点 GA4 测量 ID（空 = 不注入）。
//
// 由装配层（page / presentation 构建路径）从 SiteSettings 快照读出后传入 ——
// 站点级设置属于构建上下文的一部分，读进程级全局变量会破坏确定性构建
// （同一文档在两次构建之间拿到不同的 ID，产物字节就不再由输入唯一确定）。
func WithGA4MeasurementID(id string) CompileOption {
	return func(c *compileConfig) { c.ga4MeasurementID = strings.TrimSpace(id) }
}

// buildTrackConfig 生成打点脚本的运行时配置（BIZ-8 访问计数）。
//
// 页面浏览上报需要知道"这一页属于哪个工程、什么语言"：产物是同一份字节发给
// 所有访客，这两个值只能在构建期烘进页面（与系统页面槽位、导航链接同一路子）。
// 用 json.Marshal 构造而不是字符串拼接：projectID/lang 即便将来携带特殊字符，
// 也不会破坏这段行内 JS 的语法。
func buildTrackConfig(projectID, lang string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	payload, err := json.Marshal(map[string]string{
		"projectId": projectID,
		"lang":      strings.TrimSpace(lang),
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("window.__skyTrack=%s;\n", payload)
}
