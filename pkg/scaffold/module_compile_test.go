package scaffold

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// moduleClosure 是生成模块真实编译所需的最小 pkg 闭包（由
// go list -deps ./pkg/response ./pkg/middleware 实测得出，只取非测试 Go 文件）。
var moduleClosure = []string{
	"pkg/facing",
	"pkg/enums",
	"pkg/response",
	"pkg/middleware",
	"pkg/logger",
	"pkg/view",
	"pkg/i18n",
	"pkg/sitehttps",
	"pkg/database",
	"pkg/database/driver",
	"pkg/rls",
	"pkg/cache",
	"pkg/cache/provider",
	"pkg/auth",
	"pkg/casbin",
	"pkg/pagination",
	"pkg/jsonx",
	// pkg/view 的页面渲染入口新增的依赖（bulkids / reqparams / view/jet / view/ui）。
	"pkg/bulkids",
	"pkg/reqparams",
	"pkg/view/jet",
	"pkg/view/ui",
	"pkg/view/ui/icons",
	"pkg/utils",
	"pkg/safepath",
	"pkg/sitetz",
}

// TestModuleGeneratedProjectCompilesAndBehaves 在真实临时目录中复制正式 pkg 闭包、
// 按 alternate 根模块重写导入、生成模块，然后 go build + go test 生成物。
// 不写真实仓库、不改 go.mod、不联网。
func TestModuleGeneratedProjectCompilesAndBehaves(t *testing.T) {
	if testing.Short() {
		t.Skip("真实编译用例在 -short 下跳过")
	}
	repo := repoRootOfScaffold(t)
	const root = "example.org/team/cms/v2"
	dir := t.TempDir()

	copyFile(t, filepath.Join(repo, "go.mod"), filepath.Join(dir, "go.mod"))
	copyFile(t, filepath.Join(repo, "go.sum"), filepath.Join(dir, "go.sum"))
	rewriteModuleLine(t, filepath.Join(dir, "go.mod"), root)

	for _, rel := range moduleClosure {
		copyAndRewritePkg(t, filepath.Join(repo, rel), filepath.Join(dir, rel), root)
	}

	// 生成物必须落在它自己 import 指向的位置：<root>/internal/module/member。
	if err := os.MkdirAll(filepath.Join(dir, "internal", "module"), 0o755); err != nil {
		t.Fatalf("准备 internal/module: %v", err)
	}
	t.Chdir(filepath.Join(dir, "internal", "module"))
	if err := WriteModule("member", root); err != nil {
		t.Fatalf("WriteModule: %v", err)
	}
	behavior := filepath.Join(dir, "internal/module/member/inbound/http/member_behavior_test.go")
	if err := os.MkdirAll(filepath.Dir(behavior), 0o755); err != nil {
		t.Fatalf("准备行为测试目录: %v", err)
	}
	if err := os.WriteFile(behavior, []byte(strings.ReplaceAll(moduleBehaviorTest, "__ROOT__", root)), 0o644); err != nil {
		t.Fatalf("写入行为测试: %v", err)
	}

	runGo(t, dir, "go", "build", "./...")
	runGo(t, dir, "go", "test", "./internal/module/member/...", "-count=1")
}

func repoRootOfScaffold(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取 %s: %v", src, err)
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatalf("写入 %s: %v", dst, err)
	}
}

// rewriteModuleLine 把临时项目的 module 声明换成 alternate 根模块路径。
func rewriteModuleLine(t *testing.T, path, root string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 go.mod: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "module ") {
			lines[i] = "module " + root
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatal("go.mod 缺 module 声明")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("写入 go.mod: %v", err)
	}
}

// copyAndRewritePkg 复制正式包源码（跳过测试），并把 go_wp/ 导入换成 alternate 根。
// copyAndRewritePkg 递归复制一个 pkg 目录，并按 alternate 根模块重写导入。
//
// 必须递归：包目录里的 //go:embed 资产（SVG 图标、公共 UI 资产）是编译期输入，
// 漏掉它们会让生成模块在 embed 模式上直接编译失败。
func copyAndRewritePkg(t *testing.T, srcDir, dstDir, root string) {
	t.Helper()
	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dstDir, rel), 0o755)
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.HasSuffix(d.Name(), ".go") {
			raw = []byte(strings.ReplaceAll(string(raw), `"go_wp/`, `"`+root+`/`))
		}
		if werr := os.MkdirAll(filepath.Dir(filepath.Join(dstDir, rel)), 0o755); werr != nil {
			return werr
		}
		return os.WriteFile(filepath.Join(dstDir, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatalf("复制 %s: %v", srcDir, err)
	}
}

func runGo(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOWORK=off",
		"GOFLAGS=-mod=mod",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("%s %s 超时: %v\n%s", name, strings.Join(args, " "), ctx.Err(), out)
		}
		t.Fatalf("%s %s 失败: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	t.Logf("%s %s 通过\n%s", name, strings.Join(args, " "), out)
}

// moduleBehaviorTest 写入生成模块内，覆盖 HTTP 400/404/500/200、错误不泄漏、
// 大 ID（标准 uuid 样例）保持字符串、Service 错误映射与路由真实登记。
const moduleBehaviorTest = `package memberhttp_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	membercontract "__ROOT__/internal/module/member/contract"
	memberdto "__ROOT__/internal/module/member/dto"
	memberhttp "__ROOT__/internal/module/member/inbound/http"
	memberservice "__ROOT__/internal/module/member/service"
	membermodel "__ROOT__/internal/module/member/model"
	"__ROOT__/pkg/enums"
	"__ROOT__/pkg/facing"
	json "__ROOT__/pkg/jsonx"
	"__ROOT__/pkg/middleware"
	"__ROOT__/pkg/response"
)

const sampleUUID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

type stubService struct {
	resp   *memberdto.DemoResp
	err    error
	gotReq *memberdto.DemoReq
	calls  int
}

func (s *stubService) GetDemo(_ context.Context, req *memberdto.DemoReq) (*memberdto.DemoResp, error) {
	s.calls++
	s.gotReq = req
	return s.resp, s.err
}

var _ membercontract.MemberService = (*stubService)(nil)

func doGET(t *testing.T, svc membercontract.MemberService, target string) (int, map[string]any, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/demo", memberhttp.NewHandle(svc).GetDemo)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v body=%s", err, w.Body.String())
	}
	return w.Code, body, w.Body.String()
}

func messageOf(body map[string]any) string {
	s, _ := body["message"].(string)
	return s
}

func TestHandlerParamErrorsAre400(t *testing.T) {
	for _, target := range []string{"/demo", "/demo?id=", "/demo?id=not-a-uuid"} {
		code, body, _ := doGET(t, &stubService{}, target)
		if code != http.StatusBadRequest {
			t.Fatalf("%s 状态码 = %d，want 400（body=%v）", target, code, body)
		}
	}
}

func TestHandlerNotFoundIs404(t *testing.T) {
	svc := &stubService{err: facing.WithStatus(facing.New(enums.ErrNotFound), http.StatusNotFound)}
	code, body, raw := doGET(t, svc, "/demo?id="+sampleUUID)
	if code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，want 404", code)
	}
	if strings.Contains(raw, "ErrNotFound") || strings.Contains(messageOf(body), "ErrNotFound") {
		t.Fatalf("404 泄漏内部 key: %s", raw)
	}
}

func TestHandlerInternalErrorIs500AndDoesNotLeak(t *testing.T) {
	svc := &stubService{err: errors.New("ERROR: relation \"member_demo\" does not exist (SQLSTATE 42P01)")}
	code, body, raw := doGET(t, svc, "/demo?id="+sampleUUID)
	if code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，want 500", code)
	}
	for _, leak := range []string{"SQLSTATE", "member_demo", "relation"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("内部错误泄漏 %q: %s", leak, raw)
		}
	}
	if got := messageOf(body); got != response.InternalFallback {
		t.Fatalf("内部错误 message = %q，want 归口文案 %q", got, response.InternalFallback)
	}
}

func TestHandlerSuccessKeepsLargeIDAsString(t *testing.T) {
	svc := &stubService{resp: &memberdto.DemoResp{ID: sampleUUID, Name: "demo"}}
	code, body, _ := doGET(t, svc, "/demo?id="+sampleUUID)
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d，want 200", code)
	}
	if svc.gotReq == nil || svc.gotReq.ID != sampleUUID {
		t.Fatalf("Service 未收到原始字符串 ID: %+v", svc.gotReq)
	}
	data, _ := body["data"].(map[string]any)
	if data == nil || data["id"] != sampleUUID {
		t.Fatalf("响应 ID 未保持字符串: %v", body)
	}
	if data["name"] != "demo" {
		t.Fatalf("响应 name = %v，want demo", data["name"])
	}
}

func TestRoutesRegisterRealPermission(t *testing.T) {
	middleware.Reset()
	t.Cleanup(middleware.Reset)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := middleware.NewRouteGroup(r.Group("/api"), middleware.RouteProtected)
	memberhttp.SetupMemberRoutes(rg, nil)

	var found *middleware.RouteSpec
	for _, spec := range middleware.Snapshot() {
		if spec.Method == http.MethodGet && spec.Path == "/api/member/demo" {
			copied := spec
			found = &copied
		}
	}
	if found == nil {
		t.Fatalf("路由未真实登记，已登记=%+v", middleware.Snapshot())
	}
	if found.Perm != "member:demo_read" || found.Kind != middleware.RouteProtected {
		t.Fatalf("权限登记错误: %+v", found)
	}
	if len(middleware.RoutesOf("member:demo_read")) != 1 {
		t.Fatalf("权限点未绑定唯一路由: %+v", middleware.RoutesOf("member:demo_read"))
	}
}

func TestServiceRejectsNilAndEmptyWithoutPanic(t *testing.T) {
	svc := memberservice.NewService(nil)
	for _, req := range []*memberdto.DemoReq{nil, {ID: ""}, {ID: "   "}} {
		_, err := svc.GetDemo(context.Background(), req)
		if status, ok := facing.StatusOf(err); !ok || status != http.StatusBadRequest {
			t.Fatalf("req=%+v err=%v，want 400 业务错误", req, err)
		}
	}
}

func TestServiceMapsNotFoundTo404(t *testing.T) {
	svc := memberservice.NewService(membermodel.NewDemoModel(stubDB(t, "", nil)))
	_, err := svc.GetDemo(context.Background(), &memberdto.DemoReq{ID: sampleUUID})
	if status, ok := facing.StatusOf(err); !ok || status != http.StatusNotFound {
		t.Fatalf("err=%v，want 404 业务错误", err)
	}
	if !facing.IsBusinessError(err) {
		t.Fatalf("模型哨兵应映射为业务错误，实际 %v", err)
	}
	if err.Error() != enums.ErrNotFound {
		t.Fatalf("错误 key = %q，want %q", err.Error(), enums.ErrNotFound)
	}
}

func TestServicePassesThroughInternalDBError(t *testing.T) {
	boom := errors.New("ERROR: relation \"member_demo\" does not exist (SQLSTATE 42P01)")
	svc := memberservice.NewService(membermodel.NewDemoModel(stubDB(t, "", boom)))
	_, err := svc.GetDemo(context.Background(), &memberdto.DemoReq{ID: sampleUUID})
	if err == nil {
		t.Fatal("数据库错误不应被吞掉")
	}
	if facing.IsBusinessError(err) {
		t.Fatalf("内部数据库错误不应被当成业务错误: %v", err)
	}
	if !strings.Contains(err.Error(), "SQLSTATE 42P01") {
		t.Fatalf("内部错误应原样交给安全出口，实际 %v", err)
	}
}

var stubSeq atomic.Int64

func stubDB(t *testing.T, name string, queryErr error) *gorm.DB {
	t.Helper()
	driverName := "scaffoldmemberstub" + string(rune('a'+stubSeq.Add(1)))
	sql.Register(driverName, stubDriver{name: name, queryErr: queryErr})
	db, err := gorm.Open(postgres.New(postgres.Config{DriverName: driverName}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("构造测试数据库: %v", err)
	}
	return db
}

type stubDriver struct {
	name     string
	queryErr error
}

func (d stubDriver) Open(string) (driver.Conn, error) { return stubConn(d), nil }

type stubConn struct {
	name     string
	queryErr error
}

func (c stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unsupported") }
func (c stubConn) Close() error                        { return nil }
func (c stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("tx unsupported") }

func (c stubConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	return &stubRows{name: c.name}, nil
}

type stubRows struct {
	name string
	done bool
}

func (r *stubRows) Columns() []string { return []string{"name"} }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.done || r.name == "" {
		return io.EOF
	}
	r.done = true
	dest[0] = r.name
	return nil
}
`
