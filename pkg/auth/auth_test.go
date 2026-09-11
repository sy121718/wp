package auth

// 纯函数单元测试：会话密钥弱校验 + Redis key 派生 + 会话 ID 生成。
// 覆盖认证组件的关键安全判定（弱密钥拒绝）与 key 命名约定，
// 不依赖 Redis/数据库（会话 ID 生成用 crypto/rand，独立可测）。

import (
	"encoding/hex"
	"testing"
)

func TestWeakSessionSecret(t *testing.T) {
	cases := []struct {
		secret string
		want   bool
	}{
		{"", true},
		{"gosky-dev-session-secret-change-me-in-production", true}, // 默认值
		{"your-session-secret-key-change-this", true},              // 文档示例密钥
		{"your-secret-key", true},
		{"a-strong-random-secret-9f3b2c1d", false},
	}
	for _, c := range cases {
		if got := weakSessionSecret(c.secret); got != c.want {
			t.Fatalf("weakSessionSecret(%q) = %v, want %v", c.secret, got, c.want)
		}
	}
}

func TestSessionKeyDerivation(t *testing.T) {
	if got := sessionKey(42); got != "user:session:42" {
		t.Fatalf("sessionKey 派生失败: %q", got)
	}
	if got := blockedKey(42); got != "user:blocked:42" {
		t.Fatalf("blockedKey 派生失败: %q", got)
	}
	if got := onlineKey(42); got != "online:42" {
		t.Fatalf("onlineKey 派生失败: %q", got)
	}
}

func TestNewSessionID(t *testing.T) {
	id, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID 失败: %v", err)
	}
	if len(id) != 32 { // 16 字节 hex = 32 字符
		t.Fatalf("会话 ID 长度应为 32，got %d", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("会话 ID 应为 hex 编码: %v", err)
	}
	// 两次生成应不同（随机性）。
	id2, _ := NewSessionID()
	if id == id2 {
		t.Fatal("连续生成的会话 ID 不应相同")
	}
}
