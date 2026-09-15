package dashboardhttp

// site_settings_verification_test.go — 站点设置页保存 GSC 验证 token 的合并口径（SEO-009）。
//
// 只测 mergeSiteSettings 这一层的纯逻辑：写 token、清空删键、本页不认识的键原样保留。
// token 的形状判据由 builder.NormalizeSearchConsoleVerification 单点定义（builder 包内已有断言），
// 这里不重复实现第二份。

import (
	"encoding/json"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

// TestMergeSiteSettingsSearchConsole 保存合并：非空写键、清空删键、其它键不受影响。
func TestMergeSiteSettingsSearchConsole(t *testing.T) {
	const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_aB"
	raw := json.RawMessage(`{"siteName":"旧站名","unknownKey":"保留我"}`)
	merged, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		SiteName:                  "新站名",
		SearchConsoleVerification: token,
	})
	if err != nil {
		t.Fatalf("mergeSiteSettings: %v", err)
	}
	obj := map[string]any{}
	if err := json.Unmarshal(merged, &obj); err != nil {
		t.Fatalf("合并结果不是 JSON 对象: %v", err)
	}
	if got := obj["searchConsoleVerification"]; got != token {
		t.Fatalf("验证 token 未写入 settings，实际 %#v", got)
	}
	if obj["siteName"] != "新站名" {
		t.Fatalf("站点名未更新，实际 %#v", obj["siteName"])
	}
	// 本页不认识的键必须原样保留：整份覆盖会把别人写的配置悄悄删掉。
	if obj["unknownKey"] != "保留我" {
		t.Fatalf("本页不认识的键应原样保留，实际 %#v", obj["unknownKey"])
	}

	// 清空 = 删该键（与「清空验证 token = 停止注入」在存储层一致）。
	cleared, err := mergeSiteSettings(merged, projectcontract.SiteSettings{SiteName: "新站名"})
	if err != nil {
		t.Fatalf("mergeSiteSettings(清空): %v", err)
	}
	obj = map[string]any{}
	if err := json.Unmarshal(cleared, &obj); err != nil {
		t.Fatalf("清空后的结果不是 JSON 对象: %v", err)
	}
	if _, exists := obj["searchConsoleVerification"]; exists {
		t.Fatalf("清空后不应残留验证 token 键: %#v", obj)
	}
	if obj["unknownKey"] != "保留我" {
		t.Fatalf("清空动作不得牵连同页其它键，实际 %#v", obj["unknownKey"])
	}
}
