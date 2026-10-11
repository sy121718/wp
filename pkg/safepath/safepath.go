// Package safepath 「站内相对路径」判据的唯一实现。
//
// 谁在用：后台的两类**会被写进跳转目标**的输入 —— 语言切换表单的 redirect 隐藏域、
// 写操作后的回跳地址（shell.BackPath）、整页提示页的自动跳转目标（shell.RenderJump）、
// 以及 returnUrl 这类回跳参数（消费侧 LocalReturnPath）。它们的共同点是：这个字符串最终
// 会进 Location 头、href 或 meta refresh —— 一旦能放进外站地址，一个带凭据的后台入口
// 就成了开放重定向的跳板（docs/08-dev-login.md 记的正是这个面）。
//
// 判据四条，逐条对应一个真实攻击面：
//
//  1. 必须以 "/" 开头 —— 相对路径才谈得上「回站内」；`admin/roles` 这种不以 "/" 开头的值
//     会被浏览器按当前路径拼接，落点不是调用方以为的那个页面。
//  2. 不含 "//" —— 挡协议相对跳转（`//evil.example.com/x` 在 Location 里等价于
//     `https://evil.example.com/x`），这是开放重定向最常见的一手；顺带挡住 `/a//b`
//     这类重复分隔符（同一路径的两种写法）。
//  3. 不含反斜杠 —— 部分客户端把 `\` 当路径分隔符，`/\\evil.example.com` 可能被归一成
//     `//evil.example.com`；经 query 解码回来的值还能带它。
//  4. 不含 C0 控制字符 / DEL —— 换行能把 `Location` 头拆成两行（响应头注入）。
//
// 本包**只管这四条**，不把「长度上限」藏进判定函数：三个调用方的长度口径不同
// （语言切换隐藏域与回跳地址共用 512 字节，提示页的自动跳转今天不设上限），所以上限以
// 常量 MaxReturnPathBytes 的形式给出，由调用方按自己的语义去比。把上限并进判据会让
// 「不设上限的那一处」静默变严 —— 那是行为变更，不是重构。
//
// 非目标（按现状记录，**不是**安全结论）：
//
//   - 不拒 "." / ".." 段：`/a/../b` 今天是通过的（落点仍是站内，浏览器自己会归一）；
//   - 不拒 "?" / "#" / 百分号编码：回跳地址本来就带 query（`/admin/coupons?status=1`）。
//     站点**路由路径**的严格校验是另一套语义，见 pkg/pathkit（拒绝点段 / 编码点 / 引号等）；
//     两者不要互相替代 —— 这里是「跳转目标安全」，那里是「路由身份合法」。
//
// 入参是字符串而不是 *gin.Context，因为两侧**来源不同**：渲染侧拿到的是当前请求的
// RequestURI（已被转义，天然不含反斜杠与控制字符），消费侧拿到的是 query 里带回来、
// 已经解码的值（`\`、换行、NUL 都能出现）。本包只依赖标准库，不 import 项目内任何其他包
// （pkg 不得反向依赖 internal）。
package safepath

import "strings"

// MaxReturnPathBytes 回跳 / 跳转地址允许的最大字节数。
//
// 512 字节：正常后台页 URI（路径 + 少量筛选参数）远小于它；这个上限挡住的是「让管理员
// 打开一个 query 长到几 KB 的页面，语言切换表单里就嵌着几 KB 的 redirect，提交后 302 到
// 一个超长 URL」这条路。
//
// 由调用方决定是否施加：语言切换隐藏域与 shell.BackPath 会比它，shell.RenderJump 今天不比。
const MaxReturnPathBytes = 512

// IsSameOriginPath 判定是否「站内相对路径」（上面四条判据，**不含长度**）。
//
// 不含长度是刻意的：调用方的长度口径不同（见包注释），把上限塞进来会改变 RenderJump
// 这类今天不设上限的调用方的行为。
func IsSameOriginPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.Contains(p, "//") || strings.Contains(p, "\\") {
		return false
	}
	return !HasControlRune(p)
}

// HasControlRune 是否含 C0 控制字符或 DEL（换行 / 制表 / 退格等）。
//
// 单独导出是因为值级清洗也要用它（shell.BackPath 对每个透传的 query 值做同一判定），
// 而不是只服务路径 —— 判定只有一份，抄第二份必然漂移。
func HasControlRune(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// LangRedirectPath 语言回跳地址的**唯一一份判据**：是站内相对路径且不超过
// MaxReturnPathBytes 就返回原串，否则返回空串（task-75 自 internal/shell 迁入）。
//
// 它被两侧共用：渲染侧（adminui.Prepare，把值写进语言切换表单的隐藏域）与消费侧
// （admin 的 adminSafeLangRedirect，校验 query 带回来的值；returnUrl 这类回跳参数走
// LocalReturnPath）。判据此前在消费侧重抄过一遍，靠一条对照用例防漂移 —— 归口之后
// 那份抄本删掉了：**放宽 = 多一个开放重定向面，收紧 = 运营切了语言回不到原页面**，
// 而两种漂移都是静默的。
//
// **返回空串表示拒绝**，回落语义由调用方自己决定（渲染侧回落当前请求路径、再回落
// 控制面首页；消费侧一律回落控制面首页）—— 判据只有一份，各处的兜底不同是各自的事。
//
// 入参是字符串而不是 *gin.Context，因为两侧**来源不同**：渲染侧拿到的是当前请求的
// RequestURI（已被转义，天然不含反斜杠与控制字符），消费侧拿到的是 query 里带回来、
// 已经解码的值（`\`、换行、NUL 都能出现）。本包因此不认识 gin，也不替调用方选落点。
//
// 长度上限**只在这一层**施加：与长度无关的那一处（shell.RenderJump 只调
// IsSameOriginPath）不需要、也不该拿到它。
func LangRedirectPath(raw string) string {
	if !IsSameOriginPath(raw) || len(raw) > MaxReturnPathBytes {
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
