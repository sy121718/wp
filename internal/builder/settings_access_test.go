package builder

// settings_access_test.go — 页面访问权限设置的 JSON 往返与校验（PIPE-6）。
//
// 判据只有两条，但两条都容易在「加字段」时静默破掉：
//  1. 公开页面（绝大多数存量文档）不得因为新增字段而改变序列化字节 ——
//     Document 字节进产物 hash，字段多一个 null 就会让全站产物 hash 变化（不变量 5）；
//  2. 「密码保护但没有哈希」必须被拒绝，而不是降级成公开 —— 降级等于把受限内容
//     直接放给所有人，不可逆。

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestPageSettingsAccessRoundTripStable 无 access 字段的文档往返后字节不变。
func TestPageSettingsAccessRoundTripStable(t *testing.T) {
	const raw = `{"layout":{"mode":"full"},"seo":{"title":"t"}}`
	var s PageSettings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if s.Access != nil {
		t.Fatalf("无 access 字段时应为 nil，实际 %+v", s.Access)
	}
	out, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(out), "access") {
		t.Fatalf("公开设置不应写出 access 键：%s", out)
	}
	if !s.Access.IsPublic() {
		t.Fatal("nil 设置必须按公开处理（存量文档的默认语义）")
	}
}

// TestPageSettingsAccessRoundTripKeepsHash 带 access 的文档往返保留哈希与类型。
func TestPageSettingsAccessRoundTripKeepsHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	src := PageSettings{Access: &AccessGuardSettings{Type: AccessPassword, PasswordHash: string(hash)}}
	body, err := json.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var back PageSettings
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	if back.Access == nil || back.Access.Type != AccessPassword || back.Access.PasswordHash != string(hash) {
		t.Fatalf("往返丢了访问设置：%+v", back.Access)
	}
	if back.Access.IsPublic() {
		t.Fatal("password 类型不应被判为公开")
	}
}

// TestValidateAccessSettings 白名单与 fail closed 判据。
func TestValidateAccessSettings(t *testing.T) {
	validHash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		access  *AccessGuardSettings
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", &AccessGuardSettings{}, false},
		{"public", &AccessGuardSettings{Type: AccessPublic}, false},
		{"members", &AccessGuardSettings{Type: AccessMembers}, false},
		{"password ok", &AccessGuardSettings{Type: AccessPassword, PasswordHash: string(validHash)}, false},
		// 关键：不能降级成公开。
		{"password without hash", &AccessGuardSettings{Type: AccessPassword}, true},
		{"password with plaintext", &AccessGuardSettings{Type: AccessPassword, PasswordHash: "s3cret"}, true},
		{"password with sha256", &AccessGuardSettings{Type: AccessPassword, PasswordHash: strings.Repeat("a", 64)}, true},
		{"unknown type", &AccessGuardSettings{Type: "sso"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &PageSettings{Layout: PageLayout{Mode: LayoutFull}, Access: c.access}
			err := validateSettings(s)
			if c.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过（会把受限页面降级成公开）")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望通过，实际失败: %v", err)
			}
		})
	}
}

// TestMaxAccessPasswordBytes 上限与 bcrypt 的 72 字节口径一致。
func TestMaxAccessPasswordBytes(t *testing.T) {
	if MaxAccessPasswordBytes != 72 {
		t.Fatalf("bcrypt 只吃前 72 字节，上限必须与之一致，实际 %d", MaxAccessPasswordBytes)
	}
	// 边界值本身可被 bcrypt 接受（超长由入口拒绝，而不是让 bcrypt 静默截断）。
	if _, err := bcrypt.GenerateFromPassword(make([]byte, MaxAccessPasswordBytes), bcrypt.MinCost); err != nil {
		t.Fatalf("上限内的密码应可用: %v", err)
	}
}
