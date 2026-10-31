package sitehttps

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func newViper(t *testing.T, settings map[string]any) *viper.Viper {
	t.Helper()
	v := viper.New()
	for k, val := range settings {
		v.Set(k, val)
	}
	return v
}

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

func TestEnabled(t *testing.T) {
	t.Cleanup(ResetForTest)

	tests := []struct {
		name     string
		settings map[string]any
		nilCfg   bool
		want     bool
	}{
		{

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
