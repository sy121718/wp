package analyticsenums

import (
	"errors"
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

// 状态码是**协议**事实，声明在 StatusOf 里；文案归词库（sys_i18n），
// 所以本包不再维护一份中文兜底词表 —— 那会变成第二份文案事实源。
func TestStatusOfDeclaresRealHTTPCodes(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{ErrInvalidParam, http.StatusBadRequest},
		{ErrInvalidRange, http.StatusBadRequest},
		{ErrAnalyticsInternal, http.StatusInternalServerError},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.key] {
			t.Fatalf("用例里 %q 出现两次", c.key)
		}
		seen[c.key] = true
		if got := StatusOf(c.key); got != c.want {
			t.Errorf("StatusOf(%q) = %d，期望 %d", c.key, got, c.want)
		}
	}
}

func TestStatusOfUnknownKeyHasNoStatus(t *testing.T) {
	for _, key := range []string{"", "ErrNotDeclared", "analytics.err.unknown"} {
		if got := StatusOf(key); got != 0 {
			t.Errorf("未声明的 key %q 应返回 0（交出口按归口处理），实际 %d", key, got)
		}
	}
}

// Err 构造时就把声明里的状态码带上：调用点不再自己钉状态码。
func TestErrCarriesDeclaredStatus(t *testing.T) {
	err := Err(ErrInvalidRange)
	if err.Error() != ErrInvalidRange {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	if got, ok := facing.StatusOf(err); !ok || got != http.StatusBadRequest {
		t.Fatalf("Err(%q) 应带 400，实际 %d / %v", ErrInvalidRange, got, ok)
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrInvalidRange {
		t.Fatalf("FactOf 应给业务事实 %q，得到 %+v / %v", ErrInvalidRange, fact, ok)
	}
	if fact.Tail != "" {
		t.Errorf("纯 key 形态不该带定位信息：%q", fact.Tail)
	}
}

func TestErrIsMatchesByKey(t *testing.T) {
	sentinel := Err(ErrInvalidParam)
	if !errors.Is(Err(ErrInvalidParam), sentinel) {
		t.Error("同 key 的两条哨兵应 errors.Is 匹配")
	}
	if errors.Is(Err(ErrInvalidRange), sentinel) {
		t.Error("不同 key 的哨兵不该匹配")
	}
}

func TestErrDetailKeepsTailAndStatus(t *testing.T) {
	err := ErrDetail(ErrInvalidRange, "2026-01-01 ~ 2027-12-31")
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Tail != "2026-01-01 ~ 2027-12-31" {
		t.Fatalf("带明细的业务错误应给 Tail，得到 %+v / %v", fact, ok)
	}
	if got, ok := facing.StatusOf(err); !ok || got != http.StatusBadRequest {
		t.Errorf("带明细的错误也要保留声明的状态码，实际 %d / %v", got, ok)
	}
	if got := ErrDetail(ErrInvalidRange, "  ").Error(); got != ErrInvalidRange {
		t.Errorf("空明细应退化成纯 key，实际 %q", got)
	}
	if _, ok := facing.FactOf(errors.New(`pq: relation "page_views" does not exist`)); ok {
		t.Error("内部原文不该被判成业务事实")
	}
}
