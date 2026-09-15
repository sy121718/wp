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
)

// TestIsBusinessErrorRecognizesEnumKeys 业务错误（enums 里的 i18n key 形态，判据 1）应被认作可展示。
func TestIsBusinessErrorRecognizesEnumKeys(t *testing.T) {
	for _, msg := range []string{
		"cart.err.outOfStock",
		"order.err.alreadyPaid",
		"page.err.pathOccupied",
		"presentation.err.instanceNotFound",
		"mail.err.sendFailed",
		"page.err.rebuildRequired|3", // 参数化协议：key|param
		// masterdata 的 7 个常量：迁移 198 把它们的值从中文文案（`"参数不合法"`）改成了 key 形态。
		// 它们此前既不是 key 也不匹配常量名形态，是「判据第 3 层（纯文案启发式）」存在的唯一理由；
		// 迁完之后那一层已整层删除，判据只剩两种形态确定的模式（见 response.go 的注释）。
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

// TestIsBusinessErrorRecognizesLegacyEnumConstants 未迁形态的 enums 常量名（判据 2）必须被认作业务错误。
//
// 这一层是 CQ-010 的缺口补丁：ErrorAuto 已铺到 170 处（含 media / masterdata / inventory /
// presentation / plugin 等**未迁形态**的模块），判据只认 key 形态时，这些模块的业务错误会全部
// 变成 500 + 通用文案 —— 与「业务错误透出消息」的既定语义相反。用例取的是这些模块真实会返回的值。
func TestIsBusinessErrorRecognizesLegacyEnumConstants(t *testing.T) {
	for _, msg := range []string{
		"ErrAttachmentNotFound",  // media：附件不存在（三个已知失败点之一）
		"ErrUploadEmpty",         // media
		"ErrDownloadEmpty",       // media
		"ErrSourceFilterInvalid", // inventory：非法筛选值（三个已知失败点之一）
		"ErrSourceNotFound",      // inventory
		"ErrRouteOccupied",       // publication
		"ErrInstanceNotFound",    // presentation
		"ErrInvalidParam",        // 多模块共用（未迁形态）
		"MsgListSuccess",         // 成功消息与错误消息走同一个出口
		"MsgCreateSuccess",
	} {
		if !IsBusinessError(errors.New(msg)) {
			t.Errorf("%q 是未迁形态的 enums 常量（判据 2），应判为可展示", msg)
		}
	}
}

// legacyKeyExceptions 尚未迁到两层判据里的历史 i18n key（值形如 msg_operation_success）。
//
// 这是**显式记账**，不是被放过的漏网：只有 pkg/enums.MsgOperationSuccess 与 admin 的
// MsgSuccess / MsgLogoutSuccess 这几个常量（两个不同的值），词条在迁移 058。它们只在 Success
// 路径上被 response.translate 使用，不经过 IsBusinessError，因此对 ErrorAuto 的判据没有实际影响。
// 迁它们要连带改既有测试里硬编码的 'msg_operation_success'（pkg/response/response_test.go 与
// public/test/pkg/i18n/*），属独立批次。记住：表里每多一项，就说明还有一块没迁干净。
var legacyKeyExceptions = map[string]bool{
	"msg_operation_success":    true,
	"msg_admin_logout_success": true,
	// 注意这一项与上面两项性质不同：它不是 legacy key，而是 order 模块的兜底文案原值。
	// 它的**值被当作查表键**使用（runtimefragment.fragmentMessageKeys 的 I18N-002 过渡层，
	// 键是中文原文），要迁它必须同步把那张过渡表推进到 key 形态，属独立批次。
	"操作失败，请稍后重试": true,
}

// TestIsBusinessErrorCoversAllModuleEnums 全仓 enums 的 Err* / Msg* 常量**逐个**过判据（不是抽查）。
//
// 为什么用真清单而不是手写样本：CQ-010 的缺口正是「改造假设了 enums 都已是 key 形态」—— 手写
// 样本永远覆盖不全，而漏掉的那个模块会让它的全部业务错误静默变成 500，且不会有任何测试变红。
//
// 断言的是「值命中两层判据之一」，而不是只断言 IsBusinessError 为真：两层都是形态确定的模式，
// 逐个校验因此等价于「迁移做全了没有」的账本。这条判据是删掉第 3 层（纯文案启发式）换来的 ——
// 判据里不再留「内部错误特征清单够不够全」的余地。
func TestIsBusinessErrorCoversAllModuleEnums(t *testing.T) {
	patterns := []string{
		"../../internal/module/*/enums/*.go",
		"../../internal/module/*/*/enums/*.go", // 嵌套模块（如 product/inventory）
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
					if businessErrKey.MatchString(value) || businessErrConstant.MatchString(value) {
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
	// 例外清单过期检查：清单里的项必须仍然真实存在，否则它会掩盖「其实已经迁完」这个事实。
	for value := range legacyKeyExceptions {
		if !seenExceptions[value] {
			t.Errorf("legacyKeyExceptions 里的 %q 已不再出现（常量已迁走或删除），请从清单里删掉", value)
		}
	}
}

// TestIsBusinessErrorRejectsInternalErrors 内部错误一律不算业务错误。
//
// 这些串是从真实故障里会长出来的形状：驱动的 SQL 错误、os 的路径错误、Go 运行时栈帧、
// 格式化残留，以及带 %w 包装的调用链。它们一旦被判成业务错误，就会原样出现在客户端响应里 ——
// 而那种泄漏不会让任何既有测试变红，所以必须有这条负向断言。
//
// 「无内部特征的中文短句」（如「页面不存在」）**不在**本清单里：那一档按裁决算业务文案（判据 3），
// 原因与残余风险见 isBusinessErrText 的注释。本测试的任务是钉住「带内部特征的串绝不透出」。
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
		{"两个中文段", errors.New("页面不存在")}, // pipeline.ErrPageNotFound：纯中文内部 sentinel
		{"key 但只有两段", errors.New("cart.err")},
	}
	for _, tc := range cases {
		if IsBusinessError(tc.err) {
			t.Errorf("%s：%v 是内部错误，不该被判为可展示", tc.name, tc.err)
		}
	}
}

// decodeMessage 从响应体里取出 message 字段（本包测试用最小 JSON 解析，避免引入依赖）。
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

// TestErrorAutoShowsBusinessMessage 业务错误：消息透出（并按语言翻译），状态码沿用调用方给的。
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

// TestErrorAutoHidesInternalError 内部错误：响应里**绝不能**出现原始错误文本。
//
// 这是本条目（审计 CQ-010）的核心验收项：数据库错误不再出现在 API 响应里。
func TestErrorAutoHidesInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	leak := `ERROR: duplicate key value violates unique constraint "page_routes_path_key" (SQLSTATE 23505)`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	// 调用方按「预期内的参数错误」写了 400，但真实错误是内部故障。
	ErrorAuto(c, http.StatusBadRequest, "page", errors.New(leak))

	body := w.Body.String()
	for _, forbidden := range []string{"SQLSTATE", "page_routes_path_key", "duplicate key", "constraint"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("内部错误泄漏到响应：body 含 %q\n%s", forbidden, body)
		}
	}
	// 状态码必须如实地是 500 —— 否则客户端与监控会把内部故障当成参数问题。
	if w.Code != http.StatusInternalServerError {
		t.Errorf("内部错误应返回 500，实际 %d", w.Code)
	}
	if got := decodeMessage(t, body); got != msgInternalError {
		t.Errorf("内部错误应返回通用文案，实际 %q", got)
	}
}

// TestErrorAutoNilErrorStillSafe 传 nil 不 panic，且返回通用文案（调用点常写 if err != nil，但不该依赖它）。
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

// TestErrorAutoShowsLegacyEnumConstantMessage 未迁形态模块的业务错误必须按调用方给的状态码透出。
//
// 这是 CQ-010 缺口的回归点：判据漏掉常量形态时，同一个 ErrorAuto 调用点返回 500 + 通用文案，
// 于是「非法筛选值」「不存在的附件」「非法实体 id」这类参数错误全被当成内部故障（客户端与监控
// 都会误判）。三个用例就是三个实际出现过的失败点。
func TestErrorAutoShowsLegacyEnumConstantMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		scene string
		msg   string
	}{
		{"inventory", "ErrSourceFilterInvalid"},       // 非法筛选值（relatedParty=yes）
		{"media", "ErrAttachmentNotFound"},            // 不存在的附件
		{"masterdata", "masterdata.err.invalidParam"}, // 非法实体 id（迁移 198 后的 key 形态）
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
