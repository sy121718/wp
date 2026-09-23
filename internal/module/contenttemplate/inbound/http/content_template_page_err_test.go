package contenttemplatehttp

// content_template_page_err_test.go — 工程列表装载失败时内容模板页必须降级渲染。
//
// 原先那一分支是 `c.String(500, shell.MsgInternalError)`：浏览器里只有一块纯文本，
// 且渲染的是**未翻译的裸 key**（页面上直接显示 MsgInternalError 这串英文）—— 侧栏、页头、
// 筛选栏、列表全部消失。本用例把降级渲染的契约钉住：
//  1. HTTP 200 且是**完整页面**（响应体含 </html>，筛选控件与列表骨架在）；
//  2. 提示条是归口文案（当前语言译文 / 中文兜底），不是裸 key，也不含驱动原文；
//  3. 空态说的是「没读出来」，不是「还没有内容模板」；
//  4. **不能**说成「能力未装配」：那是 h.templates == nil 的语义，挂在一次取数失败上就是误报
//     （Ready 仍为 true 才谈得上「本页可用」）；
//  5. 引用面一并标成「查不出来」：工程都没读到，此时说「无引用」会让人以为可以放心删。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// ctProjectListErrText 工程列表装载失败的「驱动原文」（照真实形态造：表名 + SQLSTATE）。
const ctProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

// ctFailingProjects 工程契约桩：List 恒失败。
type ctFailingProjects struct {
	projectcontract.ProjectService
}

func (ctFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(ctProjectListErrText)
}

// TestContentTemplatesPageDegradesWhenProjectListFails 装载失败：完整页面 + 归口提示 + 不误导的空态。
func TestContentTemplatesPageDegradesWhenProjectListFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// templates / products / contents 都传 nil：装载失败时早退，一个都不该被碰到。
	handle := NewContentTemplatePageHandle(nil, ctFailingProjects{}, nil, nil)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/content-templates", handle.ContentTemplatesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/content-templates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// ① 完整页面：模板缺键会让 Jet 在那一行中断渲染，现象是 HTTP 200 + 后面整块 HTML 消失
	//（internal/templates/CLAUDE.md），所以「有没有 </html>」是这条契约的判据。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）：%s", body)
	}
	// 页面骨架在：页头、筛选栏、列表容器都还在（降级的是数据，不是页面）。
	for _, want := range []string{`name="entityType"`, `role="alert"`, "内容模板"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败后页面骨架必须还在（缺 %q）—— 不能退回一块裸文本", want)
		}
	}

	// ② 归口提示：无 DB ⇒ 无词条 ⇒ 回落中文兜底；关键是**不是裸 key**。
	for _, want := range []string{
		"系统内部错误，请稍后重试",
		"列表没读出来",
		contentTemplateImpactLoadFailedFallback, // 引用面「没读到」也要说出来（且与「能力未装配」区分）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"MsgInternalError",                // 裸 key（本批修掉的就是它）
		"SQLSTATE", "projects\"", "42P01", // 驱动原文只进日志
		"还没有内容模板",           // 空态误导：不是「没有模板」，是没读出来
		"内容模板能力未装配，本页暂不可用。", // 误报：能力装着呢，是这一次没读到工程
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("装载失败页面不应出现 %q", forbidden)
		}
	}
}
