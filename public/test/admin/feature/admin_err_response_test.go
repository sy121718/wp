package feature

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
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

var internalCopyForms = map[string]bool{"系统内部错误，请稍后重试": true}

var businessCopyForms = map[string]bool{
	adminenums.ErrRoleCodeExists: true,
	"角色编码已存在":                    true,
	"Role code already exists":   true,
}

var leakMarkers = []string{
	"uk_sys_role_code", "SQLSTATE", "23505", "duplicate key", "constraint",
	"sys_role", "sys_casbin_rule", "pq:", "RoleCreateReq", "sort_order", "cannot unmarshal",
}

const dbErrText = `pq: duplicate key value violates unique constraint "uk_sys_role_code" (SQLSTATE 23505): ` +
	`INSERT INTO sys_role (role_code) VALUES ($1)`

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
		files, _ := filepath.Glob(filepath.Join(dir, "*", "app-*.log"))
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

type fakeRoleService struct {
	err error

	permTree *admindto.RolePermissionTreeResp

	lastMenuSave *admindto.RoleMenuSaveReq

	menuSaveErr error
}

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

func (f *fakeRoleService) RolePermissionTree(context.Context, uint64) (*admindto.RolePermissionTreeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.permTree == nil {
		return &admindto.RolePermissionTreeResp{}, nil
	}
	return f.permTree, nil
}

func (f *fakeRoleService) RoleMenuSave(_ context.Context, req *admindto.RoleMenuSaveReq) (*admindto.RoleMenuSaveResp, error) {
	f.lastMenuSave = req
	if f.menuSaveErr != nil {
		return nil, f.menuSaveErr
	}
	return nil, f.err
}
func (f *fakeRoleService) RoleUserList(context.Context, *admindto.RoleUserListReq) (*admindto.RoleUserListResp, error) {
	return nil, f.err
}
func (f *fakeRoleService) RoleUserSave(context.Context, *admindto.RoleUserSaveReq) (*admindto.RoleUserSaveResp, error) {
	return nil, f.err
}

func newRoleErrEngine(t *testing.T, svcErr error) *gin.Engine {
	injectAdminErrorLexicon(t)
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandleWithDeps(nil, &fakeRoleService{err: svcErr}, nil, nil, nil, nil)
	engine := gin.New()
	engine.POST("/api/admin/role/create", handle.RoleCreate)
	return engine
}

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

func assertNoLeak(t *testing.T, step, body string) {
	t.Helper()
	for _, marker := range leakMarkers {
		if strings.Contains(body, marker) {
			t.Fatalf("%s：响应体里出现了内部错误片段 %q —— 内部原文只能进日志，body=%s", step, marker, body)
		}
	}
}

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

	assertNoLeak(t, "内部错误分支", body)

	logText := readLog()
	if !strings.Contains(logText, "uk_sys_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（响应收口 ≠ 吞掉错误），日志内容=%s", logText)
	}
	if !strings.Contains(logText, "handler 内部错误") || !strings.Contains(logText, "/api/admin/role/create") {
		t.Fatalf("日志里应能定位到场景与事件，日志内容=%s", logText)
	}
}

func TestAdminHandlerBindErrorDropsFieldDetail(t *testing.T) {
	engine := newRoleErrEngine(t, nil)

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

func injectAdminErrorLexicon(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(map[string]map[string]string{
		adminenums.ErrRoleCodeExists:       {"zh-CN": "角色编码已存在"},
		adminenums.ErrUsernameExists:       {"zh-CN": "用户名已存在，请修改"},
		adminenums.ErrAdminNotFound:        {"zh-CN": "管理员不存在"},
		adminenums.ErrUserNotFound:         {"zh-CN": "用户不存在"},
		adminenums.ErrSortFieldInvalid:     {"zh-CN": "无效的排序字段：请改用列表支持的排序字段"},
		adminenums.ErrSortDirectionInvalid: {"zh-CN": "无效的排序方向：只支持 asc / desc"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}
