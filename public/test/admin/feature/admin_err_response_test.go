package feature

// admin_err_response_test.go — admin handler 的错误响应「不透内部原文」的接口级守卫
//（审计 CQ-009 / CQ-010 的 admin 收口：74 处 err.Error() 拼进响应消息）。
//
// 为什么要在 handler 层用假 service 再测一遍：真实 DB 很难**稳定**造出「PostgreSQL 唯一约束名
// + SQLSTATE」这种原文（要并发撞唯一键），而这类原文一旦透出去，就是表名与约束名的泄漏。
// 这里用 NewHandleWithDeps 注入一个假 RoleService，把原文直接喂给 handler 层，
// 断言它在响应里不存在。
//
// 断言口径（刻意钉死，不是「只要出错就行」）：
//   · 业务错误（enums 白名单命中）→ 该业务文案 + 400；
//   · 其它错误 → ErrInternal 归口文案 + 500，且响应体里没有 uq_ / SQLSTATE / 表名 / Go 字段名；
//   · 请求绑定失败 → MsgBadRequest + 400，同样不带 gin 的字段级原文。
//
// 归口文案有三种形态：未加载 i18n 时就是 key 本身（pkg/i18n.GetText 未命中即原样返回），
// 加载后是中文 / 英文译文 —— 本包的 admin_shell_i18n_test 会初始化 i18n，
// 同包内先后顺序不固定，所以按集合断言，不依赖某个具体译文。

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/pkg/logger"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// internalCopyForms ErrInternal 归口文案的全部可能形态（key / zh-CN / en-US）。
var internalCopyForms = map[string]bool{
	adminenums.ErrInternal:                     true,
	"操作失败，请稍后重试":                               true,
	"Operation failed, please try again later": true,
}

// businessCopyForms ErrRoleCodeExists 的全部可能形态。
var businessCopyForms = map[string]bool{
	adminenums.ErrRoleCodeExists: true,
	"角色编码已存在":                    true,
	"Role code already exists":   true,
}

// leakMarkers 内部错误原文里那些**绝不能出现在响应里**的片段。
// dbErrText 就是照 PostgreSQL 真实报错拼的：驱动前缀 + 唯一约束名 + SQLSTATE + 表名。
var leakMarkers = []string{
	"uq_sys_role_role_code", "SQLSTATE", "23505", "duplicate key", "constraint",
	"sys_role", "sys_casbin_rule", "pq:", "RoleCreateReq", "sort_order", "cannot unmarshal",
}

// dbErrText 一条「长得就像会泄漏」的驱动错误原文（Go 原始字符串，避免转义噪声）。
const dbErrText = `pq: duplicate key value violates unique constraint "uq_sys_role_role_code" (SQLSTATE 23505): ` +
	`INSERT INTO sys_role (role_code) VALUES ($1)`

// withTempLogDir 把 logger 指到临时目录（pkg/logger 的 log.base_dir 配置键），
// 返回一个「读出该目录下 admin 场景全部日志」的闭包。
//
// 为什么要这么做：「内部错误不能进响应」与「内部错误不能丢」是**两条**判据 ——
// 只断言响应干净，把错误彻底吞掉也能通过。日志文件是那条错误唯一的落点。
// dailyRotateWriter 直接写 *os.File（无缓冲），写入即可读。
func withTempLogDir(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("log.base_dir", dir)
	cfg.Set("log.level", "debug")
	if err := logger.Init(cfg); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	return func() string {
		files, _ := filepath.Glob(filepath.Join(dir, "admin", "app-*.log"))
		var sb strings.Builder
		for _, f := range files {
			raw, rerr := os.ReadFile(f)
			if rerr != nil {
				t.Fatalf("读取日志文件 %s 失败: %v", f, rerr)
			}
			sb.Write(raw)
		}
		return sb.String()
	}
}

// fakeRoleService 只用于把错误注入 handler 的角色域契约实现（其余方法返回零值）。
//
// 另外两个字段服务于权限分配页的用例（admin_role_permissions_page_test.go）：
// 角色域一共 10 个方法，为了一页再写第二份假实现会让「给契约加方法」变成改两处 ——
// 那正是本包最容易漏的地方。
type fakeRoleService struct {
	err error
	// permTree 是 RolePermissionTree 的返回值；为 nil 时返回空视图（页面走到空状态），
	// 避免只关心错误注入的用例在这里空指针。
	permTree *admindto.RolePermissionTreeResp
	// lastMenuSave 记录最近一次 RoleMenuSave 的入参，供表单解析用例断言。
	lastMenuSave *admindto.RoleMenuSaveReq
}

// RoleList 无注入错误时返回一个空结果：页面渲染用例（admin_page_err_render_test.go）
// 要让 RolesPage 一路走到模板，而它直接读 res.List —— 返回 nil 会在那里空指针。
func (f *fakeRoleService) RoleList(context.Context, *admindto.RoleListReq) (*admindto.RoleListResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &admindto.RoleListResp{}, nil
}
func (f *fakeRoleService) RoleDetail(context.Context, *admindto.RoleDetailReq) (*admindto.RoleDetailResp, error) {
	return nil, f.err
}
func (f *fakeRoleService) RoleCreate(context.Context, *admindto.RoleCreateReq) error { return f.err }
func (f *fakeRoleService) RoleUpdate(context.Context, *admindto.RoleUpdateReq) error { return f.err }
func (f *fakeRoleService) RoleDelete(context.Context, *admindto.RoleDeleteReq) error { return f.err }
func (f *fakeRoleService) RoleMenuList(context.Context, *admindto.RoleMenuListReq) (*admindto.RoleMenuListResp, error) {
	return nil, f.err
}

// RolePermissionTree 返回预置的分配树（未预置时给空视图）。
func (f *fakeRoleService) RolePermissionTree(context.Context, uint64) (*admindto.RolePermissionTreeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.permTree == nil {
		return &admindto.RolePermissionTreeResp{}, nil
	}
	return f.permTree, nil
}

// RoleMenuSave 记录入参后按注入的错误返回（返回语义与改动前一致：成功路径给 nil resp，
// 现有的错误注入用例断言的就是错误文案，不看 data）。
func (f *fakeRoleService) RoleMenuSave(_ context.Context, req *admindto.RoleMenuSaveReq) (*admindto.RoleMenuSaveResp, error) {
	f.lastMenuSave = req
	return nil, f.err
}
func (f *fakeRoleService) RoleUserList(context.Context, *admindto.RoleUserListReq) (*admindto.RoleUserListResp, error) {
	return nil, f.err
}
func (f *fakeRoleService) RoleUserSave(context.Context, *admindto.RoleUserSaveReq) (*admindto.RoleUserSaveResp, error) {
	return nil, f.err
}

// newRoleErrEngine 组一个只挂 RoleCreate 的最小引擎（其余域注入 nil：本用例不会走到）。
func newRoleErrEngine(t *testing.T, svcErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandleWithDeps(nil, &fakeRoleService{err: svcErr}, nil, nil, nil, nil)
	engine := gin.New()
	engine.POST("/api/admin/role/create", handle.RoleCreate)
	return engine
}

// postRoleCreate 提交一个请求，返回标准响应、原始响应体与 HTTP 状态码。
func postRoleCreate(t *testing.T, engine *gin.Engine, rawBody string) (*support.StandardResponse, string, int) {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/api/admin/role/create",
		RawBody: []byte(rawBody),
	})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	std, perr := support.ParseStandardResponse(recorder)
	if perr != nil {
		t.Fatalf("解析响应失败: %v", perr)
	}
	return std, recorder.Body.String(), recorder.Code
}

// assertNoLeak 响应体里不得出现任何内部错误片段（与具体分支无关，三个用例都要过）。
func assertNoLeak(t *testing.T, step, body string) {
	t.Helper()
	for _, marker := range leakMarkers {
		if strings.Contains(body, marker) {
			t.Fatalf("%s：响应体里出现了内部错误片段 %q —— 内部原文只能进日志，body=%s", step, marker, body)
		}
	}
}

// TestAdminHandlerBusinessErrorExposesEnumsText 业务错误必须原样透出（前端要据此提示）。
func TestAdminHandlerBusinessErrorExposesEnumsText(t *testing.T) {
	engine := newRoleErrEngine(t, errors.New(adminenums.ErrRoleCodeExists))
	std, body, code := postRoleCreate(t, engine, `{"role_code":"editor","role_name":"编辑"}`)

	if code != http.StatusBadRequest {
		t.Fatalf("业务错误应是 400，got %d（body=%s）", code, body)
	}
	if !businessCopyForms[std.Message] {
		t.Fatalf("业务错误应透出 enums 文案，got message=%q body=%s", std.Message, body)
	}
	assertNoLeak(t, "业务错误分支", body)
}

// TestAdminHandlerInternalErrorIsCollected 内部错误只给归口文案 + 500，原文进日志。
func TestAdminHandlerInternalErrorIsCollected(t *testing.T) {
	readLog := withTempLogDir(t)
	engine := newRoleErrEngine(t, errors.New(dbErrText))
	std, body, code := postRoleCreate(t, engine, `{"role_code":"editor","role_name":"编辑"}`)

	if code != http.StatusInternalServerError {
		t.Fatalf("内部错误应是 500（不能沿用调用方的 400，否则监控会把故障当参数问题），got %d（body=%s）", code, body)
	}
	if !internalCopyForms[std.Message] {
		t.Fatalf("内部错误应返回归口文案，got message=%q body=%s", std.Message, body)
	}
	// 这条是整批收口的核心判据：uq_ / SQLSTATE / 表名 / Go 字段名一个都不能出现。
	assertNoLeak(t, "内部错误分支", body)

	// 另一半判据：原文不能消失 —— 必须落在日志里，否则「收口」会退化成「吞掉」。
	logText := readLog()
	if !strings.Contains(logText, "uq_sys_role_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（响应收口 ≠ 吞掉错误），日志内容=%s", logText)
	}
	if !strings.Contains(logText, "admin 接口内部错误") {
		t.Fatalf("日志里应能定位到场景与事件，日志内容=%s", logText)
	}
}

// TestAdminHandlerBindErrorDropsFieldDetail 绑定失败只给 MsgBadRequest，不放 gin 的字段级原文。
func TestAdminHandlerBindErrorDropsFieldDetail(t *testing.T) {
	engine := newRoleErrEngine(t, nil)
	// sort_order 是 int，传字符串会触发 json 反序列化错误（原文里带结构体名与字段名）
	std, body, code := postRoleCreate(t, engine, `{"role_code":"editor","role_name":"编辑","sort_order":"not-an-int"}`)

	if code != http.StatusBadRequest {
		t.Fatalf("绑定失败应是 400，got %d（body=%s）", code, body)
	}
	if strings.Contains(body, "cannot unmarshal") || strings.Contains(body, "RoleCreateReq") ||
		strings.Contains(body, "sort_order") {
		t.Fatalf("绑定失败的响应不应带 Go 结构体 / 字段级原文，body=%s", body)
	}
	if std.Message == "" {
		t.Fatalf("绑定失败应给一条可翻译的参数错误文案，body=%s", body)
	}
	assertNoLeak(t, "绑定失败分支", body)
}
