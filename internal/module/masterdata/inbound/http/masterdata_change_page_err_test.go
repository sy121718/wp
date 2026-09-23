package masterdatahttp

// masterdata_change_page_err_test.go — 工程列表装载失败时变更记录页必须降级渲染。
//
// 原先那一分支是 `c.String(500, shell.MsgInternalError)`：浏览器里只有一块**未翻译的裸 key**
// （页面上直接显示 MsgInternalError 这串英文），侧栏 / 页头 / 筛选栏全部消失。本用例把降级渲染
// 的契约钉住：
//  1. HTTP 200 且是**完整页面**（响应体含 </html>，筛选控件在）；
//  2. 提示条是归口文案（当前语言译文 / 中文兜底），不是裸 key，也不含驱动原文；
//  3. 空态说的是「没读出来」，不是「没有符合条件的变更记录」—— 后者会让人以为历史丢了；
//  4. 装载失败时**一次取数都不做**：所有取数都要工程作用域（selected 为空），真跑下去只会
//     再报一次「未指定工程」，把真正的故障盖成「你没选工程」。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// mdProjectListErrText 工程列表装载失败的「驱动原文」（照真实形态造：表名 + SQLSTATE）。
const mdProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

// mdFailingProjects 工程契约桩：List 恒失败。
type mdFailingProjects struct {
	projectcontract.ProjectService
}

func (mdFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(mdProjectListErrText)
}

// mdCountingChanges 变更记录契约桩：只记「有没有被调用过」。
//
// 页面会调的方法全部覆写 —— 未覆写的方法会打到嵌入的 nil 接口上 panic，
// 而那条 panic 要断言的是「不该被调用」，用计数比用崩溃更好读。
type mdCountingChanges struct {
	masterdatacontract.MasterDataService
	calls int
}

func (m *mdCountingChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	m.calls++
	return 0, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	m.calls++
	return 0, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) EntityTimeline(context.Context, *masterdatadto.EntityTimelineReq) (*masterdatadto.EntityTimelineResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

// TestMasterDataChangesPageDegradesWhenProjectListFails 装载失败：完整页面 + 归口提示 + 不误导的空态。
func TestMasterDataChangesPageDegradesWhenProjectListFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	changes := &mdCountingChanges{}
	handle := NewMasterDataChangePageHandle(changes, mdFailingProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/masterdata/changes", handle.MasterDataChangesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/masterdata/changes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// ① 完整页面：模板缺键会让 Jet 在那一行中断渲染，现象是 HTTP 200 + 后面整块 HTML 消失
	//（internal/templates/CLAUDE.md），所以「有没有 </html>」是这条契约的判据。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）：%s", body)
	}
	// 页面骨架在：筛选栏与两张面板都是页面自己的结构，不依赖取数结果。
	for _, want := range []string{`name="entityType"`, `name="entityId"`, `role="alert"`, "按实体汇总"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败后页面骨架必须还在（缺 %q）—— 不能退回一块裸文本", want)
		}
	}

	// ② 归口提示：无 DB ⇒ 无词条 ⇒ 回落中文兜底；关键是**不是裸 key**。
	for _, want := range []string{"系统内部错误，请稍后重试", "页面数据未取到：", "列表没读出来"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"MsgInternalError",                // 裸 key（本批修掉的就是它）
		"SQLSTATE", "projects\"", "42P01", // 驱动原文只进日志
		"没有符合条件的变更记录", "没有任何变更记录", // 空态误导：不是「没有记录」，是没读出来
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("装载失败页面不应出现 %q", forbidden)
		}
	}

	// ④ 一次取数都不做（否则页面会再叠一条「未指定工程」，把真正的故障盖掉）。
	if changes.calls != 0 {
		t.Fatalf("装载失败时不应调用变更记录服务，实际调用 %d 次", changes.calls)
	}
}
