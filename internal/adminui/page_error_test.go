package adminui

// page_error_test.go — 页面出口**错误事实**的判据守卫（不碰数据库）。
//
// 守的是「这个 error 能不能给用户看」这一条边界。它以前是各模块自己维护的白名单
// （page_err.go / siteSlotFacingMessages / productErrControlledMessages …），
// 每份都是第二真相：service 换措辞或某个 key 改由别的模块提供，判据就静默失配，
// 而两种症状都不报错 —— 有时把内部错误当业务文案透出，有时把业务文案吞成归口。
//
// 现在判据只有一条（pkg/response.IsBusinessError 的形态正则），所以这里的用例直接钉形态：
// `a.b.c` key、`Err*` / `Msg*` 常量名都算业务错误；其余一律归口。

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func errFactCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/admin/pages/create", nil)
	return c
}

func TestPageErrorFactClassifiesBusinessErrors(t *testing.T) {
	c := errFactCtx(t)

	t.Run("nil 错误给零值（没有错误与归口文案必须区分得开）", func(t *testing.T) {
		fact := PageErrorFact(c, "page", nil)
		if fact.Key != "" || fact.Fallback != "" || fact.Business {
			t.Fatalf("nil err 应给零值事实，实际 %+v", fact)
		}
		if ErrorFactText(c, fact) != "" {
			t.Fatal("零值事实的文案应为空串")
		}
	})

	t.Run("key 形态与常量名形态都算业务错误", func(t *testing.T) {
		for _, raw := range []string{"cart.err.outOfStock", "ErrInvalidParam", "MsgFieldRequired"} {
			fact := PageErrorFact(c, "page", errors.New(raw))
			if !fact.Business || fact.Key != raw || fact.Fallback != raw {
				t.Fatalf("%q 应被判定成业务错误并原样带出 key，实际 %+v", raw, fact)
			}
		}
	})

	t.Run("key：明细 拆成两段", func(t *testing.T) {
		fact := PageErrorFact(c, "page", fmt.Errorf("%w: 顶级节点 0 超过上限", errors.New("ErrInvalidDocument")))
		if !fact.Business || fact.Key != "ErrInvalidDocument" {
			t.Fatalf("应带出冒号前的 key，实际 %+v", fact)
		}
		if strings.TrimSpace(fact.Tail) == "" {
			t.Fatal("冒号后的明细段应原样带出（由模块自己解码）")
		}
	})

	t.Run("基础设施错误一律归口，不透出原文", func(t *testing.T) {
		raw := `pq: relation "pages" does not exist (SQLSTATE 42P01)`
		fact := PageErrorFact(c, "page", errors.New(raw))
		if fact.Business || fact.Key != MsgInternalError {
			t.Fatalf("未命中的错误应落归口 key，实际 %+v", fact)
		}
		text := ErrorFactText(c, fact)
		for _, tok := range []string{"SQLSTATE", "relation", "pq:"} {
			if strings.Contains(text, tok) {
				t.Fatalf("归口文案泄漏了内部细节 %q：%s", tok, text)
			}
		}
	})

	t.Run("形态判据是锚定整体匹配，夹带不算命中", func(t *testing.T) {
		for _, raw := range []string{
			"<script>alert(1)</script>ErrInvalidParam",
			"ErrInvalidParam<script>alert(1)</script>",
			"前缀 ErrInvalidParam",
		} {
			if fact := PageErrorFact(c, "page", errors.New(raw)); fact.Business {
				t.Fatalf("夹带内容不该命中业务错误形态：%q", raw)
			}
		}
	})
}

func TestTranslateArgsFillsOnlyStringPlaceholders(t *testing.T) {
	c := errFactCtx(t)

	t.Run("无参数时原样取词", func(t *testing.T) {
		if got := TranslateArgs(c, "admin.page.langs.excluded", "已排除该语言", nil); got != "已排除该语言" {
			t.Fatalf("无参数应回落兜底原文，实际 %q", got)
		}
	})

	t.Run("有参数时按 %s 位置填充", func(t *testing.T) {
		got := TranslateArgs(c, "page.bulk.pageAllDeleted", "已删除 %s 个页面。", []string{"3"})
		if got != "已删除 3 个页面。" {
			t.Fatalf("参数应填进 %%s，实际 %q", got)
		}
	})

	t.Run("占位符与参数个数不符时按 Sprintf 语义暴露", func(t *testing.T) {
		// 参数少一个 → %!s(MISSING)：宁可页面上出现明显的坏串，也不要静默显示一句完整的假话。
		got := TranslateArgs(c, "page.bulk.pagePartial", "已删除 %s 个，%s 个未能删除。", []string{"2"})
		if !strings.Contains(got, "%!s(MISSING)") {
			t.Fatalf("参数不足应当场暴露（Sprintf 语义），实际 %q", got)
		}
	})
}
