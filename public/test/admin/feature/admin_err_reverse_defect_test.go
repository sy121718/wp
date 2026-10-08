package feature

// admin_err_reverse_defect_test.go — 任务 1 的反向缺陷回归（业务错误被吞成 500 + 通用提示）。
//
// 背景：admin 的 22 处 r.ErrorInternal(c, "admin", err) 不看白名单 —— 它只记日志、回 500 + 通用提示。
// 于是「用户名已存在」这类**业务**错误在运营侧表现成「系统故障」：运营看不到该改哪个字段，
// 只会反复重试。本批把这 22 处改成 adminFail（命中白名单 → 400 + 原样文案；未命中 → 日志 + 500 归口）。
//
// 为什么用假 service 再测一遍 handler 层：真实库很难**稳定**造出「重名」与「PostgreSQL 唯一约束
// + SQLSTATE」这两种错误（要并发撞唯一键），而这两条正是本批要钉死的分支。
// 假 service 把两种 error 直接喂给 handler，断言状态码、文案与「响应体里没有内部片段」。
//
// 依赖同文件包的既有资产：internalCopyForms / leakMarkers / dbErrText / assertNoLeak /
// withTempLogDir（都在 admin_err_response_test.go，同一个 feature 包）。

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

// adminBusinessCopyForms 几个管理员域业务错误的全部可能形态（key / zh-CN / en-US）。
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

// fakeAdminService 把同一个错误注入管理员域的全部契约方法。
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

// withStubSession 给 AdminProfile 补一个会话主体（它直接读 c.Get("user_id")）。
func withStubSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", int64(1))
		c.Next()
	}
}

// newAdminErrEngine 组一个只挂本批改过的几个端点（其余域注入 nil：本用例不会走到）。
func newAdminErrEngine(t *testing.T, svcErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// DTO 上带了自定义规则（email_strict）：生产由 config 的组件初始化注册，
	// 这里直接调同一个入口（sync.Once，重复调用无副作用），否则 ShouldBind 会 panic。
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

// adminSend 发一个请求并返回标准响应、原始响应体与状态码。
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

// adminCreateBody 一条能通过 binding 的新建管理员请求体。
const adminCreateBody = `{"username":"editor","email":"editor@example.com","password":"secret123"}`

// TestAdminInternalEndpointBusinessErrorIsNotSwallowed 业务错误在这些端点上必须是 400 + 原样文案。
//
// 每条对应本批从 ErrorInternal 改成 adminFail 的一个**业务可能**调用点：
// AdminCreate（重名/重邮箱）、AdminDetail / AdminProfile（目标不存在）、AdminList（排序字段非法）。
// 修复前这些一律 500 + 通用提示 —— 运营看不到「用户名已存在」，只会反复重试。
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

// TestAdminListSortParamErrorIsNowVisible 排序参数错误已入 enums：400 + 具体文案。
//
// 本用例的前身是 TestAdminListServiceBusinessErrorWithoutEnumsStaysCollected —— 当时 AdminList
// 直接 errors.New("无效的排序字段")（中文原文、不是 enums key），它进不了白名单，
// 于是页面与接口被归口成 500 + 「操作失败，请稍后重试」：调用方把排序方向拼成 "up" 只会看到
// 「系统故障」，不知道该改成 asc / desc。现在补了 adminenums.ErrSortFieldInvalid /
// ErrSortDirectionInvalid（迁移 271 中英各一行），所以这条钉桩同步改成 400 + 具体文案 ——
// 留着旧的 500 断言会与新行为互相矛盾。
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

// TestAdminListSortBridgeIsEnums 静态守卫：service 的排序校验必须走 enums 常量，不能退回中文原文。
//
// 上面那条用例用的是假 service，它只能证明「写 handler 侧已经能透出具体文案」；
// 一旦 service 悄悄退回 errors.New("无效的排序字段")，接口又会变成 500 + 归口，而用例照样绿。
// 所以这里按源码把两个常量钉住（判据与 scripts/check-no-internal-error-leak.sh 同源：
// 「服务端文案是不是来自拥有白名单的模块」）。
func TestAdminListSortBridgeIsEnums(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(support.RepoRoot(t),
		"internal", "module", "admin", "service", "admin.go"))
	if err != nil {
		t.Fatalf("读取 admin_crud.go 失败: %v", err)
	}
	src := string(raw)
	for _, want := range []string{
		"errors.New(adminenums.ErrSortFieldInvalid)",
		"errors.New(adminenums.ErrSortDirectionInvalid)",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("排序校验应返回 enums 常量（%s）：中文原文进不了白名单，会被归口成通用提示", want)
		}
	}
	if strings.Contains(src, `errors.New("无效的排序`) {
		t.Fatal("排序校验又出现了中文原文错误 —— 它进不了白名单，接口会退回 500 + 归口")
	}
}

// TestAdminInternalEndpointInfraErrorStaysCollected 基础设施错误仍是 500 + 归口，且原文只进日志。
//
// 覆盖面刻意包含**列表 / 查询**端点（AdminList / AdminDetail / AdminProfile）：
// 它们看起来「只读、不会出错」，但驱动层一样会抛原文，收口不能只做写操作。
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
