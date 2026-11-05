package pubenums

import (
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrProjectMissingDeclaresBadRequest(t *testing.T) {
	err := ErrProjectMissing()

	status, ok := facing.StatusOf(err)
	if !ok {
		t.Fatalf("错误未携带状态码：%T", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400", status)
	}

	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("应命中断业务错误判据：%+v", fact)
	}
	if fact.Key != ErrProjectRequired {
		t.Errorf("key = %q，期望 %q", fact.Key, ErrProjectRequired)
	}
	if fact.Tail != "" {
		t.Errorf("这条错误没有明细段，实际 %q", fact.Tail)
	}
	if fact.Fallback != fact.Key {
		t.Errorf("兜底 = %q，应回落到 key 本身（文案归词库，声明处不再带中文）", fact.Fallback)
	}
}
