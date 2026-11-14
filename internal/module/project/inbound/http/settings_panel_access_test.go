package projecthttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// postSettingsPanelDoc 以面板的真实输入协议（明确 JSON）提交整份文档。
func postSettingsPanelDoc(t *testing.T, document, password string) string {
	t.Helper()
	payload := map[string]any{"document": json.RawMessage(document)}
	if password != "" {
		payload["accessPassword"] = password
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	code, rendered := panelJSON(t, newSettingsRouter(t), "/workbench/settings", string(body))
	if code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", code, rendered)
	}
	return rendered
}

func TestSettingsPanelAccessSegmentRenders(t *testing.T) {
	body := postSettingsPanelDoc(t, `{"settings":{"access":{"type":"password"}},"root":[]}`, "")
	for _, want := range []string{
		`data-wb-setting="settings.access.type" data-wb-value=""`,
		`data-wb-setting="settings.access.type" data-wb-value="password"`,
		`data-wb-setting="settings.access.type" data-wb-value="members"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %s", want)
		}
	}

	if !strings.Contains(body, "wb-access-password") {
		t.Error("密码类型下应出现密码重设输入框")
	}

	if !strings.Contains(body, "未设置") {
		t.Error("未设密码时应显示「未设置」")
	}
	if strings.Contains(body, "data-wb-apply") {
		t.Error("未重设密码时不应渲染 data-wb-apply 字段")
	}
}

func TestSettingsPanelAccessPasswordHashedServerSide(t *testing.T) {
	const password = "s3cret-Passw0rd"
	body := postSettingsPanelDoc(t, `{"settings":{"access":{"type":"password"}},"root":[]}`, password)
	if strings.Contains(body, password) {
		t.Fatal("响应里出现了明文密码")
	}
	if !strings.Contains(body, "data-wb-apply") {
		t.Fatal("重设密码后应渲染 data-wb-apply 回填字段")
	}

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

	if !strings.Contains(body, "已设置") {
		t.Error("重设后应显示「已设置」")
	}
}

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
	body := postSettingsPanelDoc(t, string(doc), "")
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

func TestSettingsPanelAccessPasswordTooLongRejected(t *testing.T) {
	body := postSettingsPanelDoc(t, `{"settings":{"access":{"type":"password"}},"root":[]}`, strings.Repeat("a", 80))
	if strings.Contains(body, "data-wb-apply") {
		t.Fatal("超长密码不应产生回填字段")
	}
	if !strings.Contains(body, "密码过长") {
		t.Error("应给出可读的拒绝原因")
	}
}

func TestSettingsPanelAccessPublicHidesPasswordRow(t *testing.T) {
	body := postSettingsPanelDoc(t, `{"settings":{"access":{"type":"public"}},"root":[]}`, "")
	if strings.Contains(body, "wb-access-password") {
		t.Error("公开类型不应出现密码重设输入框")
	}
}
