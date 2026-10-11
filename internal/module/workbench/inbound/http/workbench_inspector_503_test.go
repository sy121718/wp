package workbenchhttp

// workbench_inspector_503_test.go — 「能力未装配」档的 503 文案契约（task-114）。
//
// 为什么单独钉它：`InspectorNavigationCreate` 是全仓**唯一**一个把 `adminui.PageInternalText`
// 的成品文案塞进 JSON 体的地方（其余 JSON 出口都走 pkg/response）。这条路径的存在理由是
// 「pkg/response 不经过模板取词层，缺词条时会把**裸 key**（"MsgInternalError"）写进 message，
// 前端弹出一串英文裸串」—— task-114 把它的来源从 `shell.PageInternalText` 换成直引
// `adminui.PageInternalText`（同实现），本用例把「换家不改行为」钉住。
//
// 这条分支在改动前**没有任何测试覆盖**（全仓没有用例引用过 InspectorNavigationCreate，
// 也没有用例把 NavigationPicker 置 nil），所以它既是最容易被顺手「统一」成 pkg/response 的地方，
// 也是唯一一处「统一之后用户会看到裸 key」的地方 —— 两个理由叠加，值得一条专门的断言。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
)

// TestInspectorNavigationCreateUnassembledKeepsTranslatedMessage 「能力未装配」= 503 + **译文**
// （缺词条时是中文兜底），且 message 里不得出现裸 key。
func TestInspectorNavigationCreateUnassembledKeepsTranslatedMessage(t *testing.T) {
	// 空 deps 就是那档的构造方式：没给的端口留 nil（见 workbenchModeDeps 的注释）。
	handle := workbenchModeDeps{}.newHandle()
	router := newWorkbenchErrorProbeRouter(t, handle.InspectorNavigationCreate)

	// 探针路由按 GET 注册；能力判定在**读参数之前**，所以参数有没有都不影响这一档。
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/probe?projectId=p-1", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配能力应是 503，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, adminui.MsgInternalError) {
		t.Fatalf("message 不许是裸 key（前端会原样弹出来）：%s", body)
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("message 应是归口译文（缺词条时回落到与 058 zh-CN 词条逐字相同的中文），got %s", body)
	}
}
