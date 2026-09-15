package sitehttps

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// newViper 构造一个只含 server 段的配置实例（不走 config 包，避免循环依赖）。
func newViper(t *testing.T, settings map[string]any) *viper.Viper {
	t.Helper()
	v := viper.New()
	for k, val := range settings {
		v.Set(k, val)
	}
	return v
}

// TestEnabledWithoutConfigFallsBackToGinMode 配置未注入时按 gin 模式兜底。
//
// 回归背景：这个分支曾经「一律返回 true」，结果测试进程里所有会话 cookie 都带 Secure，
// 而 httptest 的客户端在 http:// 下不会回传 Secure cookie —— 十几个包同时表现为
// 「登录成功、下一个请求 401」的假故障。release 模式仍须为 true（fail-closed）。
func TestEnabledWithoutConfigFallsBackToGinMode(t *testing.T) {
	previous := gin.Mode()
	t.Cleanup(func() { gin.SetMode(previous) })
	t.Cleanup(ResetForTest)

	ResetForTest()
	gin.SetMode(gin.TestMode)
	if Enabled() {
		t.Fatal("测试模式下配置未注入，不应下发带 Secure 的 cookie")
	}

	gin.SetMode(gin.ReleaseMode)
	if !Enabled() {
		t.Fatal("release 模式下配置未注入，应带 Secure（fail-closed）")
	}
}

// TestEnabled 覆盖判定优先级的各条分支。
func TestEnabled(t *testing.T) {
	t.Cleanup(ResetForTest)

	tests := []struct {
		name     string
		settings map[string]any
		nilCfg   bool
		want     bool
	}{
		{
			// 配置未注入时退回 gin 模式：测试进程（gin.TestMode）不带 Secure，
			// 否则 httptest 客户端不回传 Secure cookie，整片包会表现为「登录态丢失」。
			name:   "配置未注入时退回 gin 模式",
			nilCfg: true,
			want:   gin.Mode() == gin.ReleaseMode,
		},
		{
			name:     "release 且未显式配置 → true",
			settings: map[string]any{"server.mode": "release"},
			want:     true,
		},
		{
			name:     "debug 且未显式配置 → false",
			settings: map[string]any{"server.mode": "debug"},
			want:     false,
		},
		{
			name:     "显式 true 覆盖 debug",
			settings: map[string]any{"server.mode": "debug", "server.site_https": true},
			want:     true,
		},
		{
			name:     "显式 false 覆盖 release",
			settings: map[string]any{"server.mode": "release", "server.site_https": false},
			want:     false,
		},
		{
			name:     "mode 大小写不敏感",
			settings: map[string]any{"server.mode": "Release"},
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ResetForTest()
			if !tc.nilCfg {
				Init(newViper(t, tc.settings))
			}
			if got := Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
