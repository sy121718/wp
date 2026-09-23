package analyticshttp

// analytics_page_err_test.go — 工程列表装载失败时统计页必须降级渲染（不是一块裸文本）。
//
// 原先那一分支是 `c.String(500, shell.MsgInternalError)`：浏览器里只有一块纯文本，
// 而且渲染的是**未翻译的裸 key**（`c.String` 不经过任何 translate，页面上直接显示
// `MsgInternalError` 这串英文）—— 侧栏、页头、时间范围筛选全部消失。本用例把降级渲染的契约钉住：
//  1. HTTP 200 且是**完整页面**（响应体含 </html>，页面骨架在）；
//  2. 提示条是当前语言的归口文案（无词条时回落中文兜底），不是裸 key，也不含驱动原文；
//  3. 空态说的是「没读出来」，不是「还没有站点工程」—— 后者会让运营去建工程，
//     而真正的问题是这一次没读出来。
//
// 同一条判据的另一半在 analyticsFacingError：Summary 失败时的归口文案过去也是
// `shell.MsgInternalError`（值就是 key 本身），页面上同样显示裸 key，本批一并收口。

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

// analyticsProjectListErrText 工程列表装载失败的「驱动原文」（照真实形态造：表名 + SQLSTATE）。
const analyticsProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

// analyticsFailingProjects 工程契约桩：List 恒失败。
type analyticsFailingProjects struct {
	projectcontract.ProjectService
}

func (analyticsFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(analyticsProjectListErrText)
}

// TestAnalyticsPageDegradesWhenProjectListFails 装载失败：完整页面 + 归口提示 + 不误导的空态。
func TestAnalyticsPageDegradesWhenProjectListFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// analytics 传 nil：装载失败时 selected 为空，Summary 根本不该被调用 ——
	// 真调了会 nil 解引用崩掉，这本身就是一条断言（不是「恰好没崩」）。
	handle := NewAnalyticsPageHandle(nil, analyticsFailingProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/analytics", handle.AnalyticsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// ① 完整页面：模板缺键会让 Jet 在那一行中断渲染，现象是 HTTP 200 + 后面整块 HTML 消失
	//（internal/templates/CLAUDE.md），所以「有没有 </html>」是这条契约的判据。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）：%s", body)
	}
	// 页面骨架在：时间范围筛选是一张不依赖工程列表的卡（工程空时仍渲染页头与提示条）。
	if !strings.Contains(body, `role="alert"`) {
		t.Fatal("装载失败后提示条必须还在（role=alert）")
	}

	// ② 归口提示：无 DB ⇒ 无词条 ⇒ 回落中文兜底；关键是**不是裸 key**。
	for _, want := range []string{"统计服务内部错误，请稍后重试", "工程列表没读出来"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"MsgInternalError", // 裸 key（本批修掉的就是它）
		"ErrAnalyticsInternal",
		"SQLSTATE", "projects\"", "42P01", // 驱动原文只进日志
		"还没有站点工程", // 空态误导：工程明明存在，只是这次没读出来
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("装载失败页面不应出现 %q", forbidden)
		}
	}
}
