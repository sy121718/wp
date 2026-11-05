package workbenchhttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

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

func internalErrorContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/workbench/inspector?lang=zh-CN", nil)
	return c, rec
}

func TestPageInternalTextIsControlledText(t *testing.T) {
	c, _ := internalErrorContext()
	got := adminui.PageInternalText(c)
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

func TestInspectorHandleInternalErrorExitsAreControlled(t *testing.T) {
	src := pageLogicSource(t)
	text := string(src)
	start := strings.Index(text, "func (h *Handle) InspectorPanel")
	end := strings.Index(text, "func (h *Handle) OutlineTree")
	if start < 0 || end < start {
		t.Fatal("检查器方法不在 workbench_page.go 里，守卫扫不到出口")
	}
	text = text[start:end]

	var code strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	src2 := code.String()
	re := regexp.MustCompile(`response\.ErrorWithMessage\(c,\s*http\.StatusInternalServerError,\s*([^)]*)\)`)
	exits := re.FindAllStringSubmatch(src2, -1)
	if len(exits) == 0 {
		t.Fatal("未扫到任何 500 出口（写法变了？这条守卫已失效）")
	}
	for _, m := range exits {
		if !strings.Contains(m[1], "InternalKey") {
			t.Errorf("500 出口的实参没有走归口取词：%q", m[1])
		}
	}
	if strings.Contains(src2, "uiInternalErr") {
		t.Error("inspector_handle.go 不得再出现 MsgInternalError（裸 key 不能进响应）")
	}

}

func TestInstanceSaveNotAssembledReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handle := &Handle{}
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
