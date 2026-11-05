package builder

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

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

func TestMaxAccessPasswordBytes(t *testing.T) {
	if MaxAccessPasswordBytes != 72 {
		t.Fatalf("bcrypt 只吃前 72 字节，上限必须与之一致，实际 %d", MaxAccessPasswordBytes)
	}

	if _, err := bcrypt.GenerateFromPassword(make([]byte, MaxAccessPasswordBytes), bcrypt.MinCost); err != nil {
		t.Fatalf("上限内的密码应可用: %v", err)
	}
}
