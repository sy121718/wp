package workbenchhttp

// workbench_error_shape_test.go — 两个「错误出口形状」的守卫（形态与文案来源的判据见下）。
//
//	① inspector_handle.go 的 500 出口：**形态**保持 text/plain（前端 fetch → r.text() →
//	   morphHTML，不检查 r.ok），但**文案来源**必须是归口译文 —— `c.String` 不经过
//	   pkg/response 的翻译层，直接写 workbenchenums.MsgInternalError 时浏览器里就是那串英文 key。
//	   （检查器段落装配已下沉 service：取数失败在那边以 error 返回，handler 只剩一个出口，
//	   所以下面的计数与 `exits` 对齐，而不是钉死数字。）
//	② workbench_instance.go 的 InstanceSave 503 出口：必须是 JSON —— 前端 api.js 的 send()
//	   无条件 r.json()，正文是 text/plain 时解析抛异常、then 链断掉，用户看不到任何提示。
//
// 为什么要有测试：这两个出口在**正常环境里都不可达**（ComponentSchemas 只在注册表损坏时失败、
// 生成的 schema raw 一定是合法 JSON；instances 端口只在装配缺失时为空），运行时回归永远碰不到
// 它们 —— 一旦有人把文案来源退回裸 key、或把 503 退回 c.String，没有任何东西会失败。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/internal/shell"
)

// internalErrorZhText 从 058 种子迁移里读 MsgInternalError 的 zh-CN 词条值。
//
// 用词条值当断言基准而不是写死字符串，是因为它同时钉住了两件事：词条确实已 seed
// （漏了这条断言就没人发现「响应文案在库里不存在」），以及中文兜底与词条逐字一致。
func internalErrorZhText(t *testing.T) string {
	t.Helper()
	sqlPath := filepath.Join("..", "..", "..", "..", "..", "public", "migrations", "058_i18n_seed_enums.sql")
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读迁移 058 失败（路径或文件名变了？）：%v", err)
	}
	re := regexp.MustCompile(`\(\s*'MsgInternalError'\s*,\s*'zh-CN'\s*,\s*'([^']*)'`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("迁移 058 里没有 MsgInternalError 的 zh-CN 词条")
	}
	return m[1]
}

// internalErrorContext 造一个语言确定为 zh-CN 的测试上下文。
func internalErrorContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/workbench/inspector?lang=zh-CN", nil)
	return c, rec
}

// TestPageInternalTextIsControlledText 归口取词返回的是人话，不是裸 key。
//
// 这条断言在「i18n 已初始化 / 未初始化」两种环境下都成立，理由不是巧合：
// shell.PageInternalText 是 TranslateFor(c)("MsgInternalError", "系统内部错误，请稍后重试")，
// 而那个中文兜底与 058 的 zh-CN 词条**逐字相同** —— 命中词条与回落兜底返回的是同一个字符串。
// 语言固定为 zh-CN（?lang=zh-CN），所以两种情况都等于词条值；只有「有人改了兜底」或
// 「词条被改成别的措辞」时才会不等，而那正是该被测试拦下来的漂移。
func TestPageInternalTextIsControlledText(t *testing.T) {
	c, _ := internalErrorContext()
	got := shell.PageInternalText(c)
	want := internalErrorZhText(t)
	if got != want {
		t.Fatalf("归口文案 = %q，期望 %q", got, want)
	}
	if got == workbenchenums.MsgInternalError {
		t.Fatalf("归口文案不能是裸 key：%q", got)
	}
	if strings.HasPrefix(got, "Msg") || strings.HasPrefix(got, "workbench.") {
		t.Fatalf("归口文案看起来仍是 key：%q", got)
	}
}

// TestInspectorHandleInternalErrorExitsAreControlled inspector_handle.go 的每个 500 出口
// 都必须走归口取词，且整个文件不得再出现裸 key。
//
// 这个出口在正常环境里不可达（见文件头），所以只能按源码钉形状：这里扫的是**出口实参**，
// 不是「文件里出现过某个字面量」—— 将来再加 500 出口而忘了走归口，这条会红。
func TestInspectorHandleInternalErrorExitsAreControlled(t *testing.T) {
	src, err := os.ReadFile("workbench_page.go")
	if err != nil {
		t.Fatalf("读 workbench_page.go 失败：%v", err)
	}
	text := string(src)
	start := strings.Index(text, "func (h *Handle) InspectorPanel")
	end := strings.Index(text, "func (h *Handle) OutlineTree")
	if start < 0 || end < start {
		t.Fatal("检查器方法不在 workbench_page.go 里，守卫扫不到出口")
	}
	text = text[start:end]
	// 只扫非注释行：注释里解释「旧写法为什么是缺陷」时会提旧常量名，那不算泄漏。
	var code strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	src2 := code.String()
	re := regexp.MustCompile(`c\.String\(http\.StatusInternalServerError,\s*([^)]*)\)`)
	exits := re.FindAllStringSubmatch(src2, -1)
	if len(exits) == 0 {
		t.Fatal("未扫到任何 500 出口（写法变了？这条守卫已失效）")
	}
	for _, m := range exits {
		if !strings.Contains(m[1], "PageInternalText") {
			t.Errorf("500 出口的实参没有走归口取词：%q", m[1])
		}
	}
	if strings.Contains(src2, "MsgInternalError") {
		t.Error("inspector_handle.go 不得再出现 MsgInternalError（裸 key 不能进响应）")
	}
	// 与出口数对齐而不是钉死数字：出口个数是实现细节（取数失败合并或拆分都会变），
	// 「每个 500 出口都有归口取词」才是判据 —— 新增出口却忘了走归口，这条仍然会红。
	if n := strings.Count(src2, "shell.PageInternalText(c)"); n < len(exits) {
		t.Errorf("归口取词只出现 %d 次，少于 %d 个 500 出口", n, len(exits))
	}
}

// TestInstanceSaveNotAssembledReturnsJSON 能力未装配时回合法 JSON（code 503 + 受控文案），
// 而不是纯文本 —— 前端 send() 无条件 r.json()，纯文本会抛异常并把提示整条吞掉。
func TestInstanceSaveNotAssembledReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handle := &Handle{} // instances 端口未注入
	router := gin.New()
	router.POST("/workbench/instance/save", handle.InstanceSave)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/instance/save?lang=zh-CN",
		strings.NewReader(`{"id":"i1","draftDocument":{"root":[]}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 %d，期望 503", recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q，期望 JSON（text/plain 会让前端 r.json() 抛异常）", ct)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	raw := recorder.Body.String()
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("503 响应不是合法 JSON：%v\n原始正文：%q", err, raw)
	}
	if body.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d，期望 503", body.Code)
	}
	if body.Message != internalErrorZhText(t) {
		t.Errorf("message = %q，期望归口译文 %q", body.Message, internalErrorZhText(t))
	}
}
