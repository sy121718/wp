package blockhttp

// block_page_handle_test.go — 块管理页「写动作出口」的护栏。
//
// 现象（用户报告）：在 /admin/blocks 点了「新建并编辑」之后，浏览器停在一张只有
// ErrBlockDuplicate 字样的空白页上 —— 没有页面壳、不是中文、也没有回列表的入口。
// 两个成因就在这里守住：
//
//  1. 业务错误必须能被认出来（blockErrKey）：认不出来的会被当成内部故障（通用提示），
//     而同名冲突、被引用、类型非法这些都是用户自己就能改正的业务失败；
//  2. 失败必须回到**页面**（提示页）：本页不再有「只输出一行裸文本」的第二条通道，
//     也不再靠 303 + ?err= 把结论文案塞进查询串。
//
// 断言的是渲染产物本身（HTTP 200 + data-jump-state + 文案），不是「函数没报错」。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// TestBlockErrKeyBusinessErrors 每一个业务 sentinel 都要映射到自己的词条 key。
//
// 漏登记一个 → 它被当成内部错误 → 用户只看到「系统内部错误，请稍后重试」，
// 而日志里那条 error 与用户看到的提示对不上号。
func TestBlockErrKeyBusinessErrors(t *testing.T) {
	sentinels := []error{
		blockcontract.ErrParamRequired,
		blockcontract.ErrNotFound,
		blockcontract.ErrProjectNotFound,
		blockcontract.ErrProjectRequired,
		blockcontract.ErrNameRequired,
		blockcontract.ErrInvalidDoc,
		blockcontract.ErrInvalidKind,
		blockcontract.ErrInvalidCategory,
		blockcontract.ErrDuplicate,
		blockcontract.ErrInvalidReuseMode,
		blockcontract.ErrBlockInUse,
	}
	for _, err := range sentinels {
		got := blockErrKey(err)
		if got == "" {
			t.Fatalf("业务错误 %v 未被识别为业务错误（会被当成内部故障）", err)
		}
		if got != err.Error() {
			t.Fatalf("业务错误的词条 key 应等于其 Error()（enums 常量即 key），实际 %q vs %q", got, err.Error())
		}
	}
	// 包装后的错误同样要认得出来（service 内部可能用 %w 套一层）。
	if got := blockErrKey(fmt.Errorf("定位块失败: %w", blockcontract.ErrNotFound)); got != blockcontract.ErrNotFound.Error() {
		t.Fatalf("包装后的业务错误应仍被识别，实际 %q", got)
	}
}

// TestBlockErrKeyNonBusinessErrors 基础设施错误一律判空（原文只进日志，不上面）。
func TestBlockErrKeyNonBusinessErrors(t *testing.T) {
	cases := []error{
		nil,
		errors.New("pq: relation \"blocks\" does not exist"),
		gorm.ErrRecordNotFound, // 驱动/ORM 的内部错误，不是本模块的业务语义
	}
	for _, err := range cases {
		if got := blockErrKey(err); got != "" {
			t.Fatalf("非业务错误 %v 应返回空串（回通用提示 + 记日志），实际 %q", err, got)
		}
	}
}

// jumpStubService 只覆盖写动作出口用例需要的 Create / Delete，其余方法由
// stubBlockService（block_save_content_err_test.go）提升 —— 它的 Create / Delete 会 panic，
// 这里覆盖掉，避免为「验证出口形状」写第二份完整桩。
type jumpStubService struct {
	stubBlockService
	createResp *blockdto.BlockResp
	createErr  error
	deleteErr  error
}

func (s *jumpStubService) Create(context.Context, *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	return s.createResp, s.createErr
}

func (s *jumpStubService) Delete(context.Context, *blockdto.DeleteReq) error {
	return s.deleteErr
}

// runBlockWrite 用真实 Jet 模板跑一次写 handler，返回状态码与响应体。
//
// target 里带上 query（表单 action 上带的筛选上下文，shell.BackPath 从它读回）；
// form 是 POST 表单体。
func runBlockWrite(t *testing.T, target, form string, handler gin.HandlerFunc) (int, string) {
	t.Helper()
	v := viper.New()
	v.Set("log.base_dir", t.TempDir())
	if err := logger.Init(v); err != nil {
		t.Fatalf("logger init: %v", err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.POST("/write", handler)

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// assertJumpPage 断言提示页形状：200 + 状态标记 + 整页渲染完 + 指定文案片段。
func assertJumpPage(t *testing.T, code int, body, state string, wants ...string) {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("提示页应为 200（不再是 303 + ?err=），实际 %d", code)
	}
	if !strings.Contains(body, `data-jump-state="`+state+`"`) {
		t.Fatalf("提示页缺少 data-jump-state=%q：%s", state, body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("提示页未渲染到布局尾部（模板在某一行中断）")
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("提示页缺少 %q", want)
		}
	}
}

// TestCreateBlockJumpPage 新建失败 → 失败提示页（200 + err 态 + 白名单文案，不自动跳转）。
func TestCreateBlockJumpPage(t *testing.T) {
	h := NewBlockPageHandle(&jumpStubService{}, nil)
	code, body := runBlockWrite(t, "/write?project=p-1", "projectId=p-1&name=", h.CreateBlock)

	assertJumpPage(t, code, body, "err")
	if !strings.Contains(body, blockcontract.ErrNameRequired.Error()) &&
		!strings.Contains(body, "块名称不能为空") {
		t.Errorf("失败提示页没有带白名单文案（未接 i18n 时为 key，接上后为译文）：%s", body)
	}
	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("失败不应自动跳转（Seconds=0）：运营要看清楚原因")
	}
}

// TestCreateBlockSuccessJumpToWorkbench 新建成功 → 成功提示页（200 + ok 态 + 1 秒后进工作台）。
func TestCreateBlockSuccessJumpToWorkbench(t *testing.T) {
	h := NewBlockPageHandle(&jumpStubService{createResp: &blockdto.BlockResp{ID: "b-9"}}, nil)
	code, body := runBlockWrite(t, "/write?project=p-1", "projectId=p-1&name=站点页眉", h.CreateBlock)

	assertJumpPage(t, code, body, "ok", "块已创建", `href="/workbench?block=b-9"`)
	if !strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("成功应在 1 秒后自动跳转（meta refresh）")
	}
}

// TestDeleteBlockJumpPage 删除成功 → 成功提示页，文案复用批量删除的 allDeleted 模板（count=1）。
func TestDeleteBlockJumpPage(t *testing.T) {
	h := NewBlockPageHandle(&jumpStubService{}, nil)
	code, body := runBlockWrite(t, "/write?project=p-1", "id=b-1", h.DeleteBlock)

	assertJumpPage(t, code, body, "ok", "已删除 1 个块")
}

// TestDeleteBlockReferencedJumpPage 被引用拒绝 → 失败提示页（不自动跳转）。
func TestDeleteBlockReferencedJumpPage(t *testing.T) {
	h := NewBlockPageHandle(&jumpStubService{deleteErr: blockcontract.ErrBlockInUse}, nil)
	code, body := runBlockWrite(t, "/write?project=p-1", "id=b-1", h.DeleteBlock)

	assertJumpPage(t, code, body, "err")
	if !strings.Contains(body, blockcontract.ErrBlockInUse.Error()) &&
		!strings.Contains(body, "已被页面或主题引用") {
		t.Errorf("被引用拒绝应带白名单文案：%s", body)
	}
	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("失败不应自动跳转")
	}
}

// TestBlocksBulkDeleteJumpPage 批量删除：全成功 → ok 态；有跳过 → err 态。
func TestBlocksBulkDeleteJumpPage(t *testing.T) {
	t.Run("全成功", func(t *testing.T) {
		h := NewBlockPageHandle(&jumpStubService{}, nil)
		code, body := runBlockWrite(t, "/write?project=p-1", "projectId=p-1&ids=b-1&ids=b-2", h.BlocksBulkDelete)
		assertJumpPage(t, code, body, "ok", "已删除 2 个块")
	})
	t.Run("部分失败", func(t *testing.T) {
		h := NewBlockPageHandle(&jumpStubService{deleteErr: blockcontract.ErrBlockInUse}, nil)
		code, body := runBlockWrite(t, "/write?project=p-1", "projectId=p-1&ids=b-1&ids=b-2", h.BlocksBulkDelete)
		assertJumpPage(t, code, body, "err")
		if !strings.Contains(body, "未能删除") {
			t.Errorf("部分失败应说清跳过数：%s", body)
		}
	})
}

// TestBlockWriteBackKeepsProjectFilter 回跳地址从表单 action 的 query 读回工程筛选。
func TestBlockWriteBackKeepsProjectFilter(t *testing.T) {
	h := NewBlockPageHandle(&jumpStubService{}, nil)
	_, body := runBlockWrite(t, "/write?project=p-1", "id=b-1", h.DeleteBlock)
	if !strings.Contains(body, `href="/admin/blocks?project=p-1"`) {
		t.Errorf("回跳地址未保留工程筛选（表单 action 上带 project，服务端 BackPath 读回）：%s", body)
	}
}
