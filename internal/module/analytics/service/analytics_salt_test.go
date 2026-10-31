package analyticsservice

import (
	"strings"
	"testing"
)

func mustSalt(t *testing.T, independent, sessionSecret string) string {
	t.Helper()
	salt, _ := ResolveAnonSalt(independent, sessionSecret)
	return salt
}

func TestResolveAnonSaltIndependentWins(t *testing.T) {
	const independent = "0123456789abcdef0123456789abcdef"

	salt, indep := ResolveAnonSalt(independent, "session-secret-a")
	if !indep {
		t.Fatalf("配置了独立盐却未标记为独立：salt=%q", salt)
	}
	if salt != independent {
		t.Errorf("独立盐被改写：得到 %q，期望 %q", salt, independent)
	}

	if other := mustSalt(t, independent, "session-secret-b"); other != salt {
		t.Errorf("会话密钥变化改动了独立盐：%q → %q", salt, other)
	}

	if _, indep := ResolveAnonSalt("   ", "session-secret-a"); indep {
		t.Error("空白配置被当成了独立盐")
	}
}

func TestResolveAnonSaltDerivesFromSessionSecret(t *testing.T) {
	const sessionSecret = "session-secret-a"

	salt, indep := ResolveAnonSalt("", sessionSecret)
	if indep {
		t.Error("未配置独立盐时不应标记为独立")
	}
	if salt == "" {
		t.Fatal("未配置独立盐且会话密钥非空时，派生盐不应为空")
	}

	if salt == sessionSecret {
		t.Error("派生盐与会话密钥相同：密钥复用没有被消除")
	}
	if len(salt) != anonSaltLen*2 {
		t.Errorf("派生盐长度异常：%d（期望 %d）", len(salt), anonSaltLen*2)
	}
	if strings.Trim(salt, "0123456789abcdef") != "" {
		t.Errorf("派生盐不是十六进制串：%q", salt)
	}

	other, _ := ResolveAnonSalt("", "session-secret-b")
	if other == salt {
		t.Error("不同会话密钥派生出同一个盐")
	}

	if got, indep := ResolveAnonSalt("", ""); got != "" || indep {
		t.Errorf("空会话密钥应得到空盐与非独立标记：salt=%q indep=%v", got, indep)
	}
}

func TestAnonSaltRotationWithIndependentSaltKeepsIPHash(t *testing.T) {
	const (
		independent = "0123456789abcdef0123456789abcdef"
		ip          = "203.0.113.7"
	)

	before := NewService(nil, mustSalt(t, independent, "session-secret-v1"))
	after := NewService(nil, mustSalt(t, independent, "session-secret-v2"))

	got, want := after.hashAnon(ip), before.hashAnon(ip)
	if got != want {
		t.Errorf("轮换会话密钥后 IP 哈希变了：%q → %q（历史数据会失效）", want, got)
	}

	legacy := NewService(nil, "session-secret-v1")
	if want == legacy.hashAnon(ip) {
		t.Error("独立盐的哈希与会话密钥当盐的哈希相同：盐没有真正解耦")
	}
}

func TestAnonSaltRotationWithoutIndependentSaltFollowsSessionSecret(t *testing.T) {
	const ip = "203.0.113.7"

	before := NewService(nil, mustSalt(t, "", "session-secret-v1"))
	after := NewService(nil, mustSalt(t, "", "session-secret-v2"))

	if before.hashAnon(ip) == after.hashAnon(ip) {
		t.Error("未配置独立盐时轮换会话密钥竟然不影响 IP 哈希，派生实现与文档不符")
	}

	legacy := NewService(nil, "session-secret-v1")
	if before.hashAnon(ip) == legacy.hashAnon(ip) {
		t.Error("派生态回落成了会话密钥直接当盐：密钥复用仍在")
	}
}

func TestWeakAnonSalt(t *testing.T) {
	cases := []struct {
		in   string
		weak bool
	}{
		{"", true},
		{"   ", true},
		{"short-salt", true},
		{strings.Repeat("a", anonSaltMinLen-1), true},
		{strings.Repeat("a", anonSaltMinLen), false},
		{strings.Repeat("0123456789abcdef", 4), false},
	}
	for _, c := range cases {
		if got := WeakAnonSalt(c.in); got != c.weak {
			t.Errorf("WeakAnonSalt(%q) = %v，期望 %v", c.in, got, c.weak)
		}
	}
}
