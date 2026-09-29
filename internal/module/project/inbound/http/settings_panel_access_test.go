package projecthttp

// settings_panel_access_test.go — 页面设置面板的访问权限 segment（PIPE-6）。
//
// 钉住三条判据：
//  1. 三个类型按钮回写 settings.access.type（空串 = 公开，与 builder 的口径一致）；
//  2. 刚重设的密码以**服务端算出的哈希**回填，且**明文与既有哈希都不进页面**；
//  3. 没有重设动作时不渲染 data-wb-apply 字段 —— 否则面板每次渲染都会把
//     「已设置的密码」清成空串（客户端文档里的哈希被空值覆盖）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// postSettingsPanel 以给定表单请求设置面板片段。
func postSettingsPanel(t *testing.T, form url.Values) string {
	t.Helper()
	router := newSettingsRouter(t)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestSettingsPanelAccessSegmentRenders 访问权限 segment 渲染三个类型按钮。
func TestSettingsPanelAccessSegmentRenders(t *testing.T) {
	body := postSettingsPanel(t, url.Values{
		"document": {`{"settings":{"access":{"type":"password"}},"root":[]}`},
	})
	for _, want := range []string{
		`data-wb-setting="settings.access.type" data-wb-value=""`,
		`data-wb-setting="settings.access.type" data-wb-value="password"`,
		`data-wb-setting="settings.access.type" data-wb-value="members"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %s", want)
		}
	}
	// 选了密码类型才出现密码重设行。
	if !strings.Contains(body, "wb-access-password") {
		t.Error("密码类型下应出现密码重设输入框")
	}
	// 未设过密码：显示「未设置」，且不渲染回填字段。
	if !strings.Contains(body, "未设置") {
		t.Error("未设密码时应显示「未设置」")
	}
	if strings.Contains(body, "data-wb-apply") {
		t.Error("未重设密码时不应渲染 data-wb-apply 字段")
	}
}

// TestSettingsPanelAccessPasswordHashedServerSide 明文只进不出：响应里只有哈希。
func TestSettingsPanelAccessPasswordHashedServerSide(t *testing.T) {
	const password = "s3cret-Passw0rd"
	body := postSettingsPanel(t, url.Values{
		"document":        {`{"settings":{"access":{"type":"password"}},"root":[]}`},
		"access-password": {password},
	})
	if strings.Contains(body, password) {
		t.Fatal("响应里出现了明文密码")
	}
	if !strings.Contains(body, "data-wb-apply") {
		t.Fatal("重设密码后应渲染 data-wb-apply 回填字段")
	}
	// 取出回填的哈希，确认它是可用的 bcrypt 哈希。
	idx := strings.Index(body, `data-wb-setting="settings.access.passwordHash"`)
	if idx < 0 {
		t.Fatal("缺少密码哈希回填字段")
	}
	tail := body[idx:]
	valueStart := strings.Index(tail, `value="`)
	if valueStart < 0 {
		t.Fatal("回填字段缺 value")
	}
	tail = tail[valueStart+len(`value="`):]
	valueEnd := strings.Index(tail, `"`)
	hash := tail[:valueEnd]
	if _, err := bcrypt.Cost([]byte(hash)); err != nil {
		t.Fatalf("回填值不是合法 bcrypt 哈希: %q (%v)", hash, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatal("回填的哈希与提交的密码不匹配")
	}
	// 已设置态同步显示。
	if !strings.Contains(body, "已设置") {
		t.Error("重设后应显示「已设置」")
	}
}

// TestSettingsPanelAccessNeverEchoesStoredHash 既有哈希绝不回填进面板 value。
func TestSettingsPanelAccessNeverEchoesStoredHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("old-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := json.Marshal(map[string]any{
		"settings": map[string]any{"access": map[string]any{"type": "password", "passwordHash": string(hash)}},
		"root":     []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := postSettingsPanel(t, url.Values{"document": {string(doc)}})
	if strings.Contains(body, string(hash)) {
		t.Fatal("面板把已存的 bcrypt 哈希回填进了页面（应只显示「已设置」）")
	}
	if !strings.Contains(body, "已设置") {
		t.Error("已设密码时应显示「已设置」")
	}
	if strings.Contains(body, "data-wb-apply") {
		t.Error("没有重设动作时不得渲染 data-wb-apply（会把哈希覆盖成空串）")
	}
}

// TestSettingsPanelAccessPasswordTooLongRejected 超长密码被拒（bcrypt 只吃前 72 字节）。
func TestSettingsPanelAccessPasswordTooLongRejected(t *testing.T) {
	body := postSettingsPanel(t, url.Values{
		"document":        {`{"settings":{"access":{"type":"password"}},"root":[]}`},
		"access-password": {strings.Repeat("a", 80)},
	})
	if strings.Contains(body, "data-wb-apply") {
		t.Fatal("超长密码不应产生回填字段")
	}
	if !strings.Contains(body, "密码过长") {
		t.Error("应给出可读的拒绝原因")
	}
}

// TestSettingsPanelAccessPublicHidesPasswordRow 公开类型不出现密码重设行。
func TestSettingsPanelAccessPublicHidesPasswordRow(t *testing.T) {
	body := postSettingsPanel(t, url.Values{
		"document": {`{"settings":{"access":{"type":"public"}},"root":[]}`},
	})
	if strings.Contains(body, "wb-access-password") {
		t.Error("公开类型不应出现密码重设输入框")
	}
}
