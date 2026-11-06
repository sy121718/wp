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
	// 声明处只给 key：兜底留空，由词库与出口归口，绝不能等于 key（那等于把裸 key 交给前端）。
	if fact.Fallback != "" {
		t.Errorf("兜底 = %q，声明处应留空（文案归词库）", fact.Fallback)
	}
}
