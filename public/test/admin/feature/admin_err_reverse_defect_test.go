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
	pkgvalidate "go_wp/pkg/validate"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

var adminBusinessCopyForms = map[string]bool{
	adminenums.ErrUsernameExists: true,
	"用户名已存在，请修改":                 true,
	"Username already exists":    true,
	adminenums.ErrAdminNotFound:  true,
	"管理员不存在":                     true,
	"Admin not found":            true,
	adminenums.ErrUserNotFound:   true,
	"用户不存在":                      true,
	"User not found":             true,
}

type fakeAdminService struct{ err error }

func (f *fakeAdminService) AdminList(context.Context, *admindto.AdminListReq) (*admindto.AdminListResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminLogin(context.Context, *admindto.AdminLoginReq, string) (*admindto.AdminLoginResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) DevLogin(context.Context, string) (*admindto.AdminLoginResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminLogout(context.Context, uint64) error { return f.err }
func (f *fakeAdminService) AdminProfile(context.Context, uint64) (*admindto.AdminProfileResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminCreate(context.Context, *admindto.AdminCreateReq) (*admindto.AdminCreateResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminEdit(context.Context, *admindto.AdminEditReq) (*admindto.AdminEditResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminDetail(context.Context, *admindto.AdminDetailReq) (*admindto.AdminDetailResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminDelete(context.Context, *admindto.AdminDeleteReq) (*admindto.AdminDeleteResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminRoleList(context.Context, *admindto.AdminRoleListReq) (*admindto.AdminRoleListResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminRoleSave(context.Context, *admindto.AdminRoleSaveReq) (*admindto.AdminRoleSaveResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminMenuList(context.Context, *admindto.AdminMenuListReq) (*admindto.AdminMenuListResp, error) {
	return nil, f.err
}
func (f *fakeAdminService) AdminMenuSave(context.Context, *admindto.AdminMenuSaveReq) (*admindto.AdminMenuSaveResp, error) {
	return nil, f.err
}

func withStubSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", int64(1))
		c.Next()
	}
}

func newAdminErrEngine(t *testing.T, svcErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	if err := pkgvalidate.RegisterCustomRules(); err != nil {
		t.Fatalf("注册自定义校验规则失败: %v", err)
	}
	handle := adminhttp.NewHandleWithDeps(&fakeAdminService{err: svcErr}, nil, nil, nil, nil, nil)
	engine := gin.New()
	engine.POST("/api/admin/create", handle.AdminCreate)
	engine.GET("/api/admin/list", handle.AdminList)
	engine.GET("/api/admin/detail", handle.AdminDetail)
	engine.GET("/api/admin/profile", withStubSession(), handle.AdminProfile)
	return engine
}

func adminSend(t *testing.T, engine *gin.Engine, method, path, rawBody string) (*support.StandardResponse, string, int) {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  method,
		Path:    path,
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

const adminCreateBody = `{"username":"editor","email":"editor@example.com","password":"secret123"}`

func TestAdminInternalEndpointBusinessErrorIsNotSwallowed(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		svcErr error
	}{
		{"AdminCreate 用户名重名", http.MethodPost, "/api/admin/create", adminCreateBody, errors.New(adminenums.ErrUsernameExists)},
		{"AdminDetail 目标不存在", http.MethodGet, "/api/admin/detail?id=9", "", errors.New(adminenums.ErrAdminNotFound)},
		{"AdminProfile 目标不存在", http.MethodGet, "/api/admin/profile", "", errors.New(adminenums.ErrUserNotFound)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newAdminErrEngine(t, tc.svcErr)
			std, body, code := adminSend(t, engine, tc.method, tc.path, tc.body)

			if code != http.StatusBadRequest {
				t.Fatalf("业务错误必须 400 + 原样文案（修复前这里是 500 + 通用提示），got %d body=%s", code, body)
			}
			if internalCopyForms[std.Message] {
				t.Fatalf("业务错误被吞成了通用提示：message=%q body=%s", std.Message, body)
			}
			if !adminBusinessCopyForms[std.Message] {
				t.Fatalf("业务错误应原样透出 enums 文案，got message=%q body=%s", std.Message, body)
			}
			assertNoLeak(t, tc.name, body)
		})
	}
}

func TestAdminListSortParamErrorIsNowVisible(t *testing.T) {
	forms := map[string]map[string]bool{
		adminenums.ErrSortFieldInvalid: {
			adminenums.ErrSortFieldInvalid:                                              true,
			"无效的排序字段：请改用列表支持的排序字段":                                                      true,
			"Invalid sort field: use one of the sortable fields supported by this list": true,
		},
		adminenums.ErrSortDirectionInvalid: {
			adminenums.ErrSortDirectionInvalid:                      true,
			"无效的排序方向：只支持 asc / desc":                                true,
			"Invalid sort direction: only asc / desc are supported": true,
		},
	}
	for _, key := range []string{adminenums.ErrSortFieldInvalid, adminenums.ErrSortDirectionInvalid} {
		t.Run(key, func(t *testing.T) {
			engine := newAdminErrEngine(t, errors.New(key))
			std, body, code := adminSend(t, engine, http.MethodGet, "/api/admin/list", "")

			if code != http.StatusBadRequest {
				t.Fatalf("入 enums 后应是 400 + 具体文案（改造前是 500 + 归口），got %d body=%s", code, body)
			}
			if internalCopyForms[std.Message] {
				t.Fatalf("参数错误被吞成了归口文案，调用方不知道该改哪一项：message=%q body=%s", std.Message, body)
			}
			if !forms[key][std.Message] {
				t.Fatalf("应原样透出 %s 的文案，got message=%q body=%s", key, std.Message, body)
			}
			assertNoLeak(t, "AdminList "+key, body)
		})
	}
}

func TestAdminListSortBridgeIsEnums(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(support.RepoRoot(t),
		"internal", "module", "admin", "service", "admin.go"))
	if err != nil {
		t.Fatalf("读取 admin_crud.go 失败: %v", err)
	}
	src := string(raw)

	for _, want := range []string{
		"adminenums.Err(adminenums.ErrSortFieldInvalid)",
		"adminenums.Err(adminenums.ErrSortDirectionInvalid)",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("排序校验应返回带兜底的业务错误（%s）：裸 key 在词条缺席时会被直出给客户端", want)
		}
	}

	for _, forbidden := range []string{
		"errors.New(adminenums.ErrSortFieldInvalid)",
		"errors.New(adminenums.ErrSortDirectionInvalid)",
		`errors.New("无效的排序`,
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("排序校验不该再用 %s —— 中文原文进不了白名单；裸 key 也没有兜底", forbidden)
		}
	}
}

func TestAdminInternalEndpointInfraErrorStaysCollected(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"AdminCreate", http.MethodPost, "/api/admin/create", adminCreateBody},
		{"AdminList", http.MethodGet, "/api/admin/list", ""},
		{"AdminDetail", http.MethodGet, "/api/admin/detail?id=9", ""},
		{"AdminProfile", http.MethodGet, "/api/admin/profile", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readLog := withTempLogDir(t)
			engine := newAdminErrEngine(t, errors.New(dbErrText))
			std, body, code := adminSend(t, engine, tc.method, tc.path, tc.body)

			if code != http.StatusInternalServerError {
				t.Fatalf("内部错误应如实 500，got %d body=%s", code, body)
			}
			if !internalCopyForms[std.Message] {
				t.Fatalf("内部错误应返回归口文案，got %q body=%s", std.Message, body)
			}
			assertNoLeak(t, tc.name, body)

			logText := readLog()
			if !strings.Contains(logText, "uk_sys_role_code") || !strings.Contains(logText, "SQLSTATE") {
				t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
			}
		})
	}
}
