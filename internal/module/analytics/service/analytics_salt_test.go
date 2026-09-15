package analyticsservice

// analytics_salt_test.go — 匿名哈希盐的解析与轮换语义（SEC-013）。
//
// 这一组断言守的是**解耦关系**，不是哈希算法本身：
// 独立盐配置下，轮换会话密钥不得改变 IP 哈希（历史数据不失效）；
// 未配置时也不能退回「会话密钥直接当盐」的复用形态。

import (
	"strings"
	"testing"
)

// mustSalt 走装配层同一条解析路径取盐（测试里不直接传原始值，避免绕过解析逻辑）。
func mustSalt(t *testing.T, independent, sessionSecret string) string {
	t.Helper()
	salt, _ := ResolveAnonSalt(independent, sessionSecret)
	return salt
}

// TestResolveAnonSaltIndependentWins 配置了独立盐：盐就是它，且与会话密钥无关。
func TestResolveAnonSaltIndependentWins(t *testing.T) {
	const independent = "0123456789abcdef0123456789abcdef"

	salt, indep := ResolveAnonSalt(independent, "session-secret-a")
	if !indep {
		t.Fatalf("配置了独立盐却未标记为独立：salt=%q", salt)
	}
	if salt != independent {
		t.Errorf("独立盐被改写：得到 %q，期望 %q", salt, independent)
	}

	// 会话密钥变化不影响独立盐 —— 这是解耦的核心（盐不再由会话密钥决定）。
	if other := mustSalt(t, independent, "session-secret-b"); other != salt {
		t.Errorf("会话密钥变化改动了独立盐：%q → %q", salt, other)
	}

	// YAML 里写空值是常见装配失误，不能当成「独立盐 = 空白串」。
	if _, indep := ResolveAnonSalt("   ", "session-secret-a"); indep {
		t.Error("空白配置被当成了独立盐")
	}
}

// TestResolveAnonSaltDerivesFromSessionSecret 未配置时：派生，而不是直接复用会话密钥。
func TestResolveAnonSaltDerivesFromSessionSecret(t *testing.T) {
	const sessionSecret = "session-secret-a"

	salt, indep := ResolveAnonSalt("", sessionSecret)
	if indep {
		t.Error("未配置独立盐时不应标记为独立")
	}
	if salt == "" {
		t.Fatal("未配置独立盐且会话密钥非空时，派生盐不应为空")
	}
	// 复用已消除：派生盐不等于会话密钥本身，也不是写死的常量。
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

	// 空会话密钥保持旧行为（空盐），装配层负责就此告警。
	if got, indep := ResolveAnonSalt("", ""); got != "" || indep {
		t.Errorf("空会话密钥应得到空盐与非独立标记：salt=%q indep=%v", got, indep)
	}
}

// TestAnonSaltRotationWithIndependentSaltKeepsIPHash
// verification 的核心：独立盐配置下，轮换会话密钥不改变 IP 哈希。
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

	// 同时确认这不是「旧行为碰巧相等」：旧行为（会话密钥当盐）产出的是另一个值。
	legacy := NewService(nil, "session-secret-v1")
	if want == legacy.hashAnon(ip) {
		t.Error("独立盐的哈希与会话密钥当盐的哈希相同：盐没有真正解耦")
	}
}

// TestAnonSaltRotationWithoutIndependentSaltFollowsSessionSecret
// 未配置独立盐时的既知行为：派生盐随会话密钥变，历史 IP 哈希在轮换点断开
// （明确接受的降级，装配层每次启动都会告警）。这条断言把该行为钉住，
// 以免有人误以为「派生之后轮换也安全」。
func TestAnonSaltRotationWithoutIndependentSaltFollowsSessionSecret(t *testing.T) {
	const ip = "203.0.113.7"

	before := NewService(nil, mustSalt(t, "", "session-secret-v1"))
	after := NewService(nil, mustSalt(t, "", "session-secret-v2"))

	if before.hashAnon(ip) == after.hashAnon(ip) {
		t.Error("未配置独立盐时轮换会话密钥竟然不影响 IP 哈希，派生实现与文档不符")
	}
	// 但派生态下也不等于「会话密钥直接当盐」：即便未配置，密钥复用也已消除。
	legacy := NewService(nil, "session-secret-v1")
	if before.hashAnon(ip) == legacy.hashAnon(ip) {
		t.Error("派生态回落成了会话密钥直接当盐：密钥复用仍在")
	}
}

// TestWeakAnonSalt 独立盐的门槛：过短的盐等于把带盐哈希退回裸哈希。
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
		{strings.Repeat("0123456789abcdef", 4), false}, // openssl rand -hex 32 的产物形态
	}
	for _, c := range cases {
		if got := WeakAnonSalt(c.in); got != c.weak {
			t.Errorf("WeakAnonSalt(%q) = %v，期望 %v", c.in, got, c.weak)
		}
	}
}
