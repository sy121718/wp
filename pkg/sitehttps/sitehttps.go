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
//  3. 配置不可读（未初始化）→ true —— fail-closed：宁可在 HTTP 下让 cookie
//     不生效，也不要在 HTTPS 下少一个 Secure。
package sitehttps

import (
	"strings"

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
		// 未初始化：fail-closed。真实运行中 config.Init 一定先于任何 cookie 写入，
		// 走到这里说明是测试或异常装配，安全侧默认更可取。
		return true
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
