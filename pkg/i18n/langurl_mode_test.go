package i18n

import "testing"

// TestParseSiteLangURLMode 语言 URL 方案取值校验（站点设置页保存与启动恢复共用同一判据）：
// 三枚举 + 空串回退默认方案 + 非法值拒绝。保存入口放行一个非法值，构建期才会以
// 「访问路径莫名其妙」的形式暴露 —— 校验必须在这里钉死。
func TestParseSiteLangURLMode(t *testing.T) {
	cases := []struct {
		raw    string
		want   SiteLangURLMode
		wantOK bool
	}{
		{raw: "off", want: SiteLangURLModeOff, wantOK: true},
		{raw: "default_plain", want: SiteLangURLModeDefaultPlain, wantOK: true},
		{raw: "all_prefix", want: SiteLangURLModeAllPrefix, wantOK: true},
		// 空串 = 未配置，回退默认方案（不是错误：设置页不提交该字段是合法状态）。
		{raw: "", want: SiteLangURLModeDefaultPlain, wantOK: true},
		{raw: "   ", want: SiteLangURLModeDefaultPlain, wantOK: true},
		// 非法值必须拒绝：大小写敏感（URL 方案是存储值不是展示文案）。
		{raw: "OFF", wantOK: false},
		{raw: "default", wantOK: false},
		{raw: "prefix", wantOK: false},
	}
	for _, c := range cases {
		got, err := ParseSiteLangURLMode(c.raw)
		if c.wantOK {
			if err != nil {
				t.Errorf("ParseSiteLangURLMode(%q) 不该报错: %v", c.raw, err)
				continue
			}
			if got != c.want {
				t.Errorf("ParseSiteLangURLMode(%q) = %q, 期望 %q", c.raw, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseSiteLangURLMode(%q) 应该拒绝非法值，实际返回 %q", c.raw, got)
		}
	}
}

// TestSetSiteLangURLModeRoundTrip 设置页保存路径的热更新语义：写入后进程值立即变化
//（保存即生效），这是 saveLangURLMode 双步（落库 + 热更新）中第二步的依据。
func TestSetSiteLangURLModeRoundTrip(t *testing.T) {
	orig := SiteLangURLModeValue()
	defer SetSiteLangURLMode(orig)
	SetSiteLangURLMode(SiteLangURLModeAllPrefix)
	if got := SiteLangURLModeValue(); got != SiteLangURLModeAllPrefix {
		t.Errorf("SetSiteLangURLMode 后进程值 = %q, 期望 all_prefix", got)
	}
}
