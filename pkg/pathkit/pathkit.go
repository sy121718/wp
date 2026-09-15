// Package pathkit 站点路径归一化的唯一实现（审计 CQ-012）。
//
// 项目里「路径归一化」其实有两种真实语义，混用会产生静默错误，所以这里用两个
// 名字显式区分，而不是提供一个「万能默认」：
//
//   - NormalizeRoutePath：**写入路由表之前**的严格校验口径。站点路径是页面的身份
//     —— 路由占用判断、canonical、产物目录名都由它派生 —— 畸形输入必须在入口拒绝，
//     而不是被静默纠正：纠正会让「用户以为的路径」与「数据库里的路径」分裂，线上表现
//     为重复内容、链接 404、占用判断失真（docs/03-pipeline.md §5.1）。
//   - MatchKey：**可信路径之间做比较**的匹配口径（去尾部斜杠）。它不做合法性判断、
//     不返回错误：输入都是可信来源（当前请求路径、已落库的菜单路径），这里只需要把
//     「/admin/pages/ 与 /admin/pages 是同一页」这件事表达出来。
//
// 访客上报的打点路径不属于以上两者：它是自由文本，服务端只做清洗与截断，
// 宁可丢弃也不能拒绝（见 analytics 的 sanitizeTrackPath），故不放在本包。
//
// 本包不含业务策略（例如「系统保留路径」属于发布管道的概念，由 pipeline 在
// 本包之上叠加），也不依赖任何项目内其他包。
package pathkit

import (
	"fmt"
	"strings"
)

// MaxRoutePathLen 站点路径长度上限。
//
// 超长路径会导致 FS 激活 ENAMETOOLONG 与 DB 行膨胀（docs/03-pipeline.md §5.1）。
const MaxRoutePathLen = 500

// NormalizeRoutePath 严格规范化站点访问路径。
//
// 规则（每一条都对应一种真实出现过的畸形输入）：
//   - 空路径、非 "/" 开头：拒绝 —— 相对路径在访问面无意义；
//   - 反斜杠：拒绝（Windows 分隔符，会被文件系统解释成目录层级）；
//   - 控制字符、空格：拒绝（裸空格会让 Link / canonical 头被截断，URL 里必须是 %20）；
//   - "?" 与 "#"：拒绝 —— 只保存 path，不含 query / fragment；
//   - 引号（双引号与单引号）：拒绝 —— 路径会被原样写进 href / canonical / 路由表，
//     裸引号能截断 HTML 属性（`<a href="/a"b">`），落库后还会让 SQL 片段与 URL 拼接出现歧义。
//     **这一档是审计 CQ-012 收敛时漏掉的**：收敛前的 publication.normalizePath（见 git HEAD 的
//     publication_control.go）逐字拒绝 `"` 与 `'`，pipeline.NormalizeURL 不拒；按 CQ-012 写明的
//     「取并集、采纳更严的一侧」，应拒。收敛时只把 `?` / `#` 的差异记进了 resolutionNote，引号
//     这一档连同它的用例一起丢了（pathkit_test.go 的拒绝表里没有引号），故漏档长期静默。
//     编码形态（%22 / %27）**不在**拒绝范围：访问面按已编码字符原样处理（与 `%20` 同一口径），
//     收敛前两处实现也都不拒它 —— 拒编码形态会把「已正确编码的合法路径」误杀。
//   - 根路径保留 "/"，其余去除**一个**结尾斜杠；
//   - 重复分隔符（"//"、"/a//b"）：拒绝 —— 同一路径的两种写法并存会让占用判断失真；
//   - "." / ".." 段与百分号编码的 .（%2e，大小写不敏感）：拒绝 —— 路径穿越；
//   - 超过 MaxRoutePathLen：拒绝；
//   - "/index" 与 "/index.html" 归一为根路径：三者在访问面映射同一个文件
//     （active/index），并存时后发布的会把前者的符号链接顶掉，而 DB 里两条路由行
//     各自存在 —— 线上内容与路由记录分裂且全程无报错。归一后重复占用由路由唯一约束
//     自然拒绝。
//
// 返回值是归一化后的路径；调用方负责把它写成自己的错误类型（页面层给用户看的
// 提示、路由层给接口的参数错误，文案不同）。
func NormalizeRoutePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("路径必须以 / 开头: %q", raw)
	}
	for _, r := range raw {
		switch {
		case r == '\\':
			return "", fmt.Errorf("路径含非法字符: %q", raw)
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("路径含控制字符: %q", raw)
		case r == ' ':
			return "", fmt.Errorf("路径含空格: %q", raw)
		case r == '?' || r == '#':
			return "", fmt.Errorf("路径含查询串或锚点: %q", raw)
		case r == '"' || r == '\'':
			return "", fmt.Errorf("路径含引号: %q", raw)
		}
	}

	// 根路径直接放行；"//" 这类全是分隔符的输入按重复分隔符拒绝（不是根路径）。
	if raw == "/" {
		return "/", nil
	}
	p := strings.TrimSuffix(raw, "/")
	if p == "" {
		return "", fmt.Errorf("路径含重复分隔符: %q", raw)
	}
	if len(p) > MaxRoutePathLen {
		return "", fmt.Errorf("路径过长（最大 %d 字符）: %q", MaxRoutePathLen, raw)
	}

	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, seg := range segs {
		if seg == "" {
			return "", fmt.Errorf("路径含重复分隔符: %q", raw)
		}
		if seg == "." || seg == ".." || strings.Contains(strings.ToLower(seg), "%2e") {
			return "", fmt.Errorf("路径含非法段 %q（拒绝路径穿越）: %q", seg, raw)
		}
	}

	// index 归一放在去尾斜杠之后："/index/" 与 "/index" 在访问面同样映射
	// active/index，前者若不被归一会与后者取到不同的路由字符串。
	if p == "/index" || p == "/index.html" {
		return "/", nil
	}
	return p, nil
}

// MatchKey 归一化用于「是不是同一个页面」判断的路径键：去首尾空白 + 去全部结尾斜杠。
//
// 空串、"/"、"///" 一律归一为空串，调用方据此判断「没有可比较的路径」
// （后台侧边栏高亮、导航当前项标记都按这个键做字符串相等比较）。
//
// **刻意不做合法性校验，包括不拒绝引号**（与 NormalizeRoutePath 的差异是有意保留的，不是漏改）：
// 本函数的输入是可信来源（当前请求路径、已落库的菜单路径）—— 要么已经过 NormalizeRoutePath
// 校验，要么由 net/http 解析；而它**不返回错误**，拒绝的唯一表达方式是返回空串，那与
// 「没有可比较的路径」是同一个值，会把「路径含引号」静默降级成「导航不高亮」，比不拒绝更糟。
// 校验放在写入侧，比较侧只保证「同一输入得到同一键」；历史脏数据里真出现含引号的路径时，
// 归一成稳定的键用于相等比较恰恰是正确行为。
func MatchKey(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), "/")
}
