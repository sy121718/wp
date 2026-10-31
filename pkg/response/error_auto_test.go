package response

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
)

func TestIsBusinessErrorRecognizesEnumKeys(t *testing.T) {
	for _, msg := range []string{
		"cart.err.outOfStock",
		"order.err.alreadyPaid",
		"page.err.pathOccupied",
		"presentation.err.instanceNotFound",
		"mail.err.sendFailed",
		"page.err.rebuildRequired|3",

		"masterdata.err.invalidParam",
		"masterdata.err.entityTypeInvalid",
		"masterdata.err.actionInvalid",
		"masterdata.err.entityIDRequired",
		"masterdata.err.projectRequired",
		"masterdata.err.timeRangeInvalid",
		"masterdata.msg.listSuccess",
	} {
		if !IsBusinessError(errors.New(msg)) {
			t.Errorf("%q 是业务错误（enums key 或业务文案形态），应判为可展示", msg)
		}
	}
}

func TestIsBusinessErrorRecognizesLegacyEnumConstants(t *testing.T) {
	for _, msg := range []string{
		"ErrAttachmentNotFound",
		"ErrUploadEmpty",
		"ErrDownloadEmpty",
		"ErrSourceFilterInvalid",
		"ErrSourceNotFound",
		"ErrRouteOccupied",
		"ErrInstanceNotFound",
		"ErrInvalidParam",
		"MsgListSuccess",
		"MsgCreateSuccess",
	} {
		if !IsBusinessError(errors.New(msg)) {
			t.Errorf("%q 是未迁形态的 enums 常量（判据 2），应判为可展示", msg)
		}
	}
}

var legacyKeyExceptions = map[string]bool{
	"msg_operation_success":    true,
	"msg_admin_logout_success": true,

	"操作失败，请稍后重试": true,
}

func TestIsBusinessErrorCoversAllModuleEnums(t *testing.T) {
	patterns := []string{
		"../../internal/module/*/enums/*.go",
		"../../internal/module/*/*/enums/*.go",
	}
	files := map[string]bool{}
	for _, p := range patterns {
		matches, gerr := filepath.Glob(p)
		if gerr != nil {
			t.Fatalf("枚举路径匹配失败: %v", gerr)
		}
		for _, m := range matches {
			files[m] = true
		}
	}
	if len(files) == 0 {
		t.Skip("未匹配到模块 enums 目录（仓库结构变化），跳过")
	}

	var checked int
	seenExceptions := map[string]bool{}
	for path := range files {
		parsed, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", path, perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Err") && !strings.HasPrefix(name.Name, "Msg") {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", path, name.Name, uerr)
					}
					checked++
					if facing.IsBusinessKey(value) {
						continue
					}
					if legacyKeyExceptions[value] {
						seenExceptions[value] = true
						continue
					}
					t.Errorf("%s 的 %s = %q 不命中任何一层判据（key 形态 / enums 常量名形态）：经 ErrorAuto 会变成"+
						" 500 + 通用文案。要么把值改成 key 形态并在迁移里补词条，要么加进 legacyKeyExceptions 并写明理由",
						filepath.Base(path), name.Name, value)
				}
			}
		}
	}
	t.Logf("已逐个校验 %d 个 enums 常量", checked)
	if checked == 0 {
		t.Fatal("没有校验到任何常量，说明解析逻辑失效（测试变成了空转）")
	}

	for value := range legacyKeyExceptions {
		if !seenExceptions[value] {
			t.Errorf("legacyKeyExceptions 里的 %q 已不再出现（常量已迁走或删除），请从清单里删掉", value)
		}
	}
}

func TestIsBusinessErrorRejectsInternalErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"pg 唯一约束", errors.New(`ERROR: duplicate key value violates unique constraint "page_routes_path_key" (SQLSTATE 23505)`)},
		{"pg 类型错误", errors.New(`ERROR: invalid input syntax for type uuid: "abc"def" (SQLSTATE 22P02)`)},
		{"文件路径", errors.New("open /srv/gowp/data/artifacts/a3f/index.html: no such file or directory")},
		{"目录误当文件", errors.New("open /tmp/uploads: is a directory")},
		{"连接串", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")},
		{"网络超时", errors.New("dial tcp 10.0.0.1:6379: i/o timeout")},
		{"包装链", fmt.Errorf("加载页面失败: %w", errors.New("sql: no rows in result set"))},
		{"格式化残留", errors.New("构造查询失败: %!s(int=1)")},
		{"运行时 panic", errors.New("panic: runtime error: index out of range [3] with length 2")},
		{"goroutine 转储", errors.New("goroutine 1 [running]:\n\t/srv/gowp/main.go:42 +0x1a")},
		{"超长中文串", errors.New(strings.Repeat("记录不存在", 30))},
		{"空错误", nil},
		{"只有一段", errors.New("somethingWentWrong")},
		{"两个中文段", errors.New("页面不存在")},
		{"key 但只有两段", errors.New("cart.err")},
	}
	for _, tc := range cases {
		if IsBusinessError(tc.err) {
			t.Errorf("%s：%v 是内部错误，不该被判为可展示", tc.name, tc.err)
		}
	}
}

func decodeMessage(t *testing.T, body string) string {
	t.Helper()
	const marker = `"message":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("响应里没有 message 字段: %s", body)
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("message 字段未闭合: %s", body)
	}
	return rest[:j]
}

func TestErrorAutoShowsBusinessMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	ErrorAuto(c, http.StatusBadRequest, "presentation", errors.New("presentation.err.instanceNotFound"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("业务错误应沿用调用方给的状态码 400，实际 %d", w.Code)
	}
	msg := decodeMessage(t, w.Body.String())
	if msg == msgInternalError {
		t.Errorf("业务错误不该被替换成通用文案，实际 %q", msg)
	}
	if !strings.Contains(msg, "presentation.err.instanceNotFound") {
		t.Errorf("业务错误应透出 key 或它的译文，实际 %q", msg)
	}
}

func TestErrorAutoHidesInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	leak := `ERROR: duplicate key value violates unique constraint "page_routes_path_key" (SQLSTATE 23505)`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	ErrorAuto(c, http.StatusBadRequest, "page", errors.New(leak))

	body := w.Body.String()
	for _, forbidden := range []string{"SQLSTATE", "page_routes_path_key", "duplicate key", "constraint"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("内部错误泄漏到响应：body 含 %q\n%s", forbidden, body)
		}
	}

	if w.Code != http.StatusInternalServerError {
		t.Errorf("内部错误应返回 500，实际 %d", w.Code)
	}
	if got := decodeMessage(t, body); got != msgInternalError {
		t.Errorf("内部错误应返回通用文案，实际 %q", got)
	}
}

func TestErrorAutoNilErrorStillSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)

	ErrorAuto(c, http.StatusBadRequest, "x", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("nil 错误也该按内部错误处理，实际 %d", w.Code)
	}
}

func TestErrorAutoShowsLegacyEnumConstantMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		scene string
		msg   string
	}{
		{"inventory", "ErrSourceFilterInvalid"},
		{"media", "ErrAttachmentNotFound"},
		{"masterdata", "masterdata.err.invalidParam"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)

		ErrorAuto(c, http.StatusBadRequest, tc.scene, errors.New(tc.msg))

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s：业务错误应沿用调用方给的 400，实际 %d", tc.scene, w.Code)
		}
		if got := decodeMessage(t, w.Body.String()); got == msgInternalError {
			t.Errorf("%s：业务错误不该被替换成通用文案，实际 %q", tc.scene, got)
		}
	}
}
