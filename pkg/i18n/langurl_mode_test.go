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

// TestSiteLangURLModePurePredicates 方案判定是**纯函数**（入参决定结果，无全局可变状态）：
// off 不分离路径、all_prefix 连默认语言也带前缀、default_plain 介于两者之间。
//
// 替代原先的「写入进程值后立即变化」round-trip 测试：那个 setter 已删除 ——
// 方案是**工程级**的值（projects.settings.langURLMode），按工程解析（pipeline.SiteLangURLModeOf）；
// 进程级可变值会让一个工程的设置决定另一个工程的判定，并被全局默认值的定时刷新周期打回。
func TestSiteLangURLModePurePredicates(t *testing.T) {
	cases := []struct {
		mode              SiteLangURLMode
		wantSeparated     bool
		wantPrefixDefault bool
	}{
		{SiteLangURLModeOff, false, false},
		{SiteLangURLModeDefaultPlain, true, false},
		{SiteLangURLModeAllPrefix, true, true},
	}
	for _, c := range cases {
		if got := SiteLangURLsSeparated(c.mode); got != c.wantSeparated {
			t.Errorf("SiteLangURLsSeparated(%q) = %v, 期望 %v", c.mode, got, c.wantSeparated)
		}
		if got := SiteLangURLPrefixDefault(c.mode); got != c.wantPrefixDefault {
			t.Errorf("SiteLangURLPrefixDefault(%q) = %v, 期望 %v", c.mode, got, c.wantPrefixDefault)
		}
	}
}
