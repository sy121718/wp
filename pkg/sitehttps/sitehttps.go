// Package sitehttps 提供「当前部署对外是不是 HTTPS」的单一判定。
//
// 为什么需要它：cookie 的 Secure 属性必须由**部署事实**决定，而不是由请求决定。
// 历史上这个判断散在三处、口径各异：
//   - pkg/auth        → 按 server.mode（release 才带 Secure）
//   - pkg/response    → 按 gin.Mode()
//   - runtimefragment → 按请求头 X-Forwarded-Proto
//
// 最后一处是真问题：X-Forwarded-Proto 由上游产生、可被请求方伪造，
// 凭它决定 Secure 等于把 cookie 的安全属性交给调用方；而自托管最常见的误配
// 恰恰是反代没传这个头 —— 表现是「HTTPS 站点的 cookie 悄悄丢了 Secure」，
// 看起来一切正常，直到有人做降级攻击。
//
// 判定优先级：
//  1. 显式配置 server.site_https（true/false）—— 部署方声明的事实，最高优先；
//  2. 未显式配置时按 server.mode 推导：release → true，其余 → false；
//  3. 配置未注入时退回 gin 的运行模式（见 Enabled 的注释：这里不能用「一律 true」兜底）。
package sitehttps

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// siteHTTPSKey 显式声明站点对外协议的配置键（server 段）。
//
//	server:
//	  site_https: true   # 站点对外是 HTTPS（反代终结 TLS 时也要写 true）
const siteHTTPSKey = "server.site_https"

var (
	// cfg 由 config.Init 注入（单向依赖 config → sitehttps，不反向 import config，
	// 否则 config 与 pkg/auth 之间会成环：config → auth → config）。
	cfg *viper.Viper
)

// Init 注入全局配置实例。由 config.Init() 在配置读取成功后调用。
func Init(v *viper.Viper) {
	cfg = v
}

// Enabled 报告当前部署是否应按「HTTPS 站点」处理（即 cookie 是否带 Secure）。
func Enabled() bool {
	if cfg == nil {
		// 配置未注入（测试夹具或异常装配）：退回 gin 的运行模式。
		//
		// 这里**不能**一律返回 true 兜底。曾经这么写过，代价是测试进程里所有会话 cookie
		// 都带上 Secure —— 而 httptest 的客户端在 http:// 下不会回传 Secure cookie，
		// 表现是整片「登录态莫名其妙丢失」的假故障（登录成功、下一个请求就是 401），
		// 排查方向会被完全带偏。release 模式仍带 Secure：fail-closed 的诉求针对的是
		// 生产部署，而生产部署一定先调用 config.Init，走的是下面那条分支。
		return gin.Mode() == gin.ReleaseMode
	}
	if cfg.IsSet(siteHTTPSKey) {
		return cfg.GetBool(siteHTTPSKey)
	}
	return strings.EqualFold(strings.TrimSpace(cfg.GetString("server.mode")), "release")
}

// ResetForTest 清空注入的配置（仅测试使用）。
func ResetForTest() {
	cfg = nil
}
