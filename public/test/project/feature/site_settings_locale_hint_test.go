package feature

// site_settings_locale_hint_test.go — 站点设置页「语言 URL 方案」说明文案的口径（V3）。
//
// 为什么值得一条渲染断言：那段说明原先指向 `i18n.site_lang_url_mode` —— 一个**已被删除**的
// config.yaml 键。文案错了不会报错，只会让运营去找一个不存在的地方；而它的取值链是
// t(key, fallback)，**词条命中时 fallback 根本不参与**，所以「模板改了」不等于「页面改了」
// （词条由迁移 493 覆盖，本用例断言 fallback 侧，词条侧由迁移的幂等回读验证）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSiteSettingsLocaleHintHasNoRemovedYamlKey 设置页那段说明不含已删的 yaml 键，且是新口径。
func TestSiteSettingsLocaleHintHasNoRemovedYamlKey(t *testing.T) {
	router, _, projectID := newSiteShippingEnv(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/settings?project="+projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("设置页应渲染成功，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("设置页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	if strings.Contains(body, "i18n.site_lang_url_mode") || strings.Contains(body, "site_lang_prefix") {
		t.Fatalf("设置页仍出现已删除的 config.yaml 键名：说明文案没跟上口径")
	}
	if !strings.Contains(body, "语言 URL 方案") {
		t.Fatalf("设置页应说明「语言 URL 方案」在哪里配（工程级开关 + 全局默认兜底）")
	}
}
