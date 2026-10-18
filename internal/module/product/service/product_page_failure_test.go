package productservice

// product_page_failure_test.go — 页面读模型失败档的**唯一判据**（各页测试共用）。
//
// 页面失败的形状只有一种：读模型返回 (nil, err)，出口按声明处的状态码给 JSON。
// 不再有「失败视图」这回事 —— 所以这里断言的是三件：
//
//   - err 非 nil；
//   - 视图为 nil（失败页形态已废除，交回视图就意味着还会被渲染）；
//   - 档位与声明处一致：声明过 HTTPStatus 的按它，业务错误默认 400，其余 500。
//
// 档位的判据与 internal/adminui 的出口同源（facing.StatusOf → response 分流），
// 因此这里红了一定是「错误在声明处少写了状态」或「多写了」。
import (
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

// wantPageFailure 断言一次页面读模型失败：nil 视图 + 期望档位。
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
