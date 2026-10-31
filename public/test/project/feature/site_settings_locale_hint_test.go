package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
