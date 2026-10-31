package productservice

import (
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

func wantPageFailure[T any](t *testing.T, step string, view *T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：应返回错误", step)
	}
	if view != nil {
		t.Fatalf("%s：失败应返回 nil 视图（失败出口是 JSON，不再渲染同一页）", step)
	}
	got := http.StatusInternalServerError
	if declared, ok := facing.StatusOf(err); ok {
		got = declared
	} else if _, ok := facing.FactOf(err); ok {
		got = http.StatusBadRequest
	}
	if got != want {
		t.Fatalf("%s：档位应为 %d，实际 %d（err=%v）", step, want, got, err)
	}
}
