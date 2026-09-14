package sitehttps

import (
	"testing"

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

// TestEnabled 覆盖判定优先级的四条分支。
func TestEnabled(t *testing.T) {
	t.Cleanup(ResetForTest)

	tests := []struct {
		name     string
		settings map[string]any
		nilCfg   bool
		want     bool
	}{
		{
			name:   "未初始化时 fail-closed（宁可在 HTTP 下 cookie 不生效）",
			nilCfg: true,
			want:   true,
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
