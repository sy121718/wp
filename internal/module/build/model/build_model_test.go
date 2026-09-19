package buildmodel

import (
	"context"
	"testing"
	"time"
)

// TestClaimRejectsNonPositiveLease 租约时长非正是配置错误，必须在碰数据库之前拒绝。
//
// 纯逻辑单测：NewModel(nil) 下若实现先去访问数据库，这里会直接 panic —— 它钉住的
// 正是「参数校验在读库之前」这一点（写错了立刻可见，而不是在生产里表现为连接错误）。
func TestClaimRejectsNonPositiveLease(t *testing.T) {
	m := NewModel(nil)
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := m.Claim(context.Background(), ttl); err == nil {
			t.Fatalf("租约时长 %s 应被拒绝", ttl)
		}
	}
}

// TestLeaseGuardRejectsEmptyToken 没有令牌就不允许写完成结论（防御性守卫）。
func TestLeaseGuardRejectsEmptyToken(t *testing.T) {
	m := NewModel(nil)
	for _, token := range []string{"", "   "} {
		if err := m.MarkSucceeded(context.Background(), 1, token, "artifact", time.Now()); err == nil {
			t.Fatalf("空令牌 %q 应被拒绝", token)
		}
	}
}
