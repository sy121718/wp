package dashboardhttp

// site_settings_handle_test.go — 站点设置保存的合并口径（BIZ-8 加 GA4 字段时确立）。
//
// 钉住的是「只动本页管的键」这条口径：站点设置是 projects.settings 这一列 JSON，
// 其它能力（现在的、将来的）也会往同一列写。整份覆盖会把别人写的配置悄悄删掉，
// 而这种丢失在页面上完全看不出来 —— 只有对应功能失效时才暴露。

import (
	"encoding/json"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

// TestMergeSiteSettingsKeepsForeignKeys 合并时保留本页不认识的键，并写入本页字段。
func TestMergeSiteSettingsKeepsForeignKeys(t *testing.T) {
	raw := json.RawMessage(`{"siteName":"旧名","seoKeywords":"a,b","nested":{"k":1}}`)
	out, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		SiteName:         "新名",
		GA4MeasurementID: "G-ABC1234567",
	})
	if err != nil {
		t.Fatalf("mergeSiteSettings: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("结果不是合法 JSON 对象: %v（%s）", err, out)
	}
	if got["siteName"] != "新名" {
		t.Errorf("本页字段未写入，siteName=%v", got["siteName"])
	}
	if got["ga4MeasurementId"] != "G-ABC1234567" {
		t.Errorf("GA4 测量 ID 未写入，ga4MeasurementId=%v", got["ga4MeasurementId"])
	}
	if got["seoKeywords"] != "a,b" {
		t.Errorf("其它能力的键被删掉了: seoKeywords=%v", got["seoKeywords"])
	}
	if _, ok := got["nested"].(map[string]any); !ok {
		t.Errorf("嵌套键被破坏: nested=%v", got["nested"])
	}
}

// TestMergeSiteSettingsClearsEmptyString 空串即删除该键：
// 「清空测量 ID = 停止注入统计代码」在存储层与实际行为必须一致（不落空值噪声）。
func TestMergeSiteSettingsClearsEmptyString(t *testing.T) {
	raw := json.RawMessage(`{"ga4MeasurementId":"G-ABC1234567","contactEmail":"a@b.c"}`)
	out, err := mergeSiteSettings(raw, projectcontract.SiteSettings{SiteName: "站点"})
	if err != nil {
		t.Fatalf("mergeSiteSettings: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("结果不是合法 JSON 对象: %v（%s）", err, out)
	}
	if _, ok := got["ga4MeasurementId"]; ok {
		t.Errorf("清空的字段应删除键而不是留空值: %s", out)
	}
	if _, ok := got["contactEmail"]; ok {
		t.Errorf("未提交的字段同样按清空处理（表单回显与提交一致）: %s", out)
	}
}

// TestParseSiteSettingsGA4 解析容错：缺字段、非对象都得到零值而不报错。
func TestParseSiteSettingsGA4(t *testing.T) {
	if s := projectcontract.ParseSiteSettings(nil); s.GA4MeasurementID != "" {
		t.Errorf("空 settings 应得零值，实际 %+v", s)
	}
	if s := projectcontract.ParseSiteSettings(json.RawMessage(`["not-an-object"]`)); s.SiteName != "" {
		t.Errorf("非对象 settings 应得零值，实际 %+v", s)
	}
	s := projectcontract.ParseSiteSettings(json.RawMessage(`{"ga4MeasurementId":"G-ABC1234567"}`))
	if s.GA4MeasurementID != "G-ABC1234567" {
		t.Errorf("GA4 测量 ID 未解析出来: %+v", s)
	}
}
