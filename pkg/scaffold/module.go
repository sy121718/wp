package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// moduleNameRe 模块名必须能直接拼成合法 Go package 名（包名为 <name>dto 等），
// 因此只接受小写字母开头的字母 / 数字 / 下划线；连字符由插件 init 保留，模块生成显式拒绝，
// 不静默改名。总长度 2~61 位（至少一个后续字符，与插件名口径一致）。
var moduleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,60}$`)

// modulePathElemRe 根模块路径的单段字符集：只接受字母数字与 . _ -，
// 段首必须是字母数字、段尾不得是点，从而排除空白、引号、反斜杠与代码注入字符。
//
// 这是 Go import path 的保守子集，不声称等同 x/mod 的完整规则：
// - 不接受 '+'、'~' 等 Go 允许但项目根模块不会使用的字符；
// - 不检查 Windows 保留段（CON / NUL 等）。
// 生成器只服务于本项目改名场景，宁窄勿宽。
var modulePathElemRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9_-])?$`)

// validateModulePath 校验根模块路径：按 '/' 切段逐段校验，禁止空段、
// 前导/尾随点与越界段，保证拼进 import 语句后仍是单一字符串字面量。
func validateModulePath(modulePath string) bool {
	if modulePath == "" || len(modulePath) > 255 {
		return false
	}
	for _, elem := range strings.Split(modulePath, "/") {
		if !modulePathElemRe.MatchString(elem) || strings.HasSuffix(elem, ".") {
			return false
		}
	}
	return true
}

// ModuleFiles 生成业务模块骨架（相对路径 → 文件内容）。
//
// name 为模块目录 / 包名前缀；modulePath 为当前项目的实际根模块路径，
// 生成代码里的全部导入（本模块包与框架 pkg 入口）都按它书写，
// 因此整个项目改 module 名时也只需传入新的根模块路径。
func ModuleFiles(name, modulePath string) (map[string]string, error) {
	if !moduleNameRe.MatchString(name) {
		return nil, fmt.Errorf("模块名 %q 非法（小写字母开头，2~61 位，仅小写字母数字下划线；模块包名由它拼接，不支持连字符）", name)
	}
	if !validateModulePath(modulePath) {
		return nil, fmt.Errorf("根模块路径 %q 非法（保守子集：按 / 分段，每段仅字母数字与 . _ -，段首字母数字、段尾非点，如 go_wp 或 example.org/team/cms/v2）", modulePath)
	}
	pascal := pascal(name)
	render := func(tpl string) string {
		return strings.NewReplacer(
			"__MODULE__", modulePath,
			"__NAME__", name,
			"__PASCAL__", pascal,
		).Replace(tpl)
	}
	return map[string]string{
		"README.md":                           render(moduleReadmeTpl),
		"contract/" + name + "_contract.go":   render(moduleContractTpl),
		"dto/" + name + "_req.go":             render(moduleDTOReqTpl),
		"dto/" + name + "_resp.go":            render(moduleDTORespTpl),
		"enums/" + name + "_enums.go":         render(moduleEnumsTpl),
		"model/" + name + "_model.go":         render(moduleModelTpl),
		"service/" + name + "_service.go":     render(moduleServiceTpl),
		"inbound/http/" + name + "_router.go": render(moduleRouterTpl),
		"inbound/http/" + name + "_handle.go": render(moduleHandlerTpl),
	}, nil
}

// WriteModule 在 name 目录下写出模块骨架。
// 目标已存在（含普通文件与符号链接）时拒绝，不覆盖用户内容。
func WriteModule(name, modulePath string) error {
	files, err := ModuleFiles(name, modulePath)
	if err != nil {
		return err
	}
	// Lstat 不跟随符号链接：目录、普通文件、指向别处的链接都算已存在。
	if _, err := os.Lstat(name); err == nil {
		return fmt.Errorf("%s 已存在，拒绝覆盖", name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for rel, content := range files {
		dst := filepath.Join(name, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// pascal 把 snake_case 模块名转成导出名（member_card → MemberCard）。
func pascal(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, "")
}

const moduleContractTpl = `package __NAME__contract

// __NAME___contract.go — __NAME__ 模块对外契约。
//
// 只放本模块对外暴露的能力，不定义外部依赖接口；跨模块调用方只 import
// 本契约与 dto，不 import model / service（internal/module 规范）。

import (
	"context"

	__NAME__dto "__MODULE__/internal/module/__NAME__/dto"
)

// __PASCAL__Service __NAME__ 域能力（骨架：一个最小读方法，按需扩展）。
type __PASCAL__Service interface {
	// GetDemo 按主键取一条示例记录（骨架占位，落地时替换为真实业务方法）。
	GetDemo(ctx context.Context, req *__NAME__dto.DemoReq) (*__NAME__dto.DemoResp, error)
}
`

const moduleDTOReqTpl = `package __NAME__dto

// __NAME___req.go — __NAME__ 模块请求 DTO。

// DemoReq 示例请求（骨架占位，按需扩展字段与校验）。
type DemoReq struct {
	ID string ` + "`json:\"id\" form:\"id\" binding:\"required,uuid\"`" + `
}
`

const moduleDTORespTpl = `package __NAME__dto

// __NAME___resp.go — __NAME__ 模块响应 DTO。

// DemoResp 示例响应（骨架占位）。
type DemoResp struct {
	ID   string ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}
`

const moduleEnumsTpl = `package __NAME__enums

// __NAME___enums.go — __NAME__ 模块权限点。
//
// 业务错误与消息复用 pkg/enums 与词库，模块内不新造点分消息 key。

const (
	// DemoRead 读取示例数据的权限点（GET 路由的真实权限对象）。
	DemoRead = "__NAME__:demo_read"
)
`

const moduleModelTpl = `package __NAME__model

// __NAME___model.go — __NAME__ 模块数据访问单元（model 层定位是 Repository）。
//
// 只暴露本模块表的具名读方法，不暴露 DB 句柄 —— 句柄是内部细节，
// service 禁止直接碰它（internal/module 规范）。

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ErrDemoNotFound 示例记录不存在（受控哨兵，service 据此映射 404 业务错误）。
var ErrDemoNotFound = errors.New("demo record not found")

// DemoModel __NAME__ 模块数据访问。
type DemoModel struct {
	db *gorm.DB
}

// NewDemoModel 构造（装配层注入连接）。
func NewDemoModel(db *gorm.DB) *DemoModel {
	return &DemoModel{db: db}
}

// tableName 示例表名（落地时改为真实表与 schema）。
func tableName() string {
	return "__NAME___demo"
}

// GetByID 按主键取一条示例记录（命名返回值）。
func (m *DemoModel) GetByID(ctx context.Context, id string) (demoName string, err error) {
	var row struct {
		Name string
	}
	err = m.db.WithContext(ctx).
		Table(tableName()).
		Where("id = ?", id).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrDemoNotFound
	}
	return row.Name, err
}
`

const moduleServiceTpl = `package __NAME__service

// __NAME___service.go — __NAME__ 模块业务服务（实现 __NAME__contract.__PASCAL__Service）。

import (
	"context"
	"errors"
	"net/http"
	"strings"

	__NAME__contract "__MODULE__/internal/module/__NAME__/contract"
	__NAME__dto "__MODULE__/internal/module/__NAME__/dto"
	__NAME__model "__MODULE__/internal/module/__NAME__/model"
	"__MODULE__/pkg/enums"
	"__MODULE__/pkg/facing"
)

// Service __NAME__ 模块业务服务。
type Service struct {
	model *__NAME__model.DemoModel
}

// NewService 构造（装配层注入 model）。
func NewService(m *__NAME__model.DemoModel) *Service {
	return &Service{model: m}
}

// 编译期断言：Service 实现模块契约（internal/module 规范）。
var _ __NAME__contract.__PASCAL__Service = (*Service)(nil)

// GetDemo 实现契约的示例读方法（命名返回值，error 放最后）。
//
// 自身再做一次参数检查：契约可能被 HTTP 之外的入口调用，
// 不能把 nil / 空 ID 直接带进 model。
func (s *Service) GetDemo(ctx context.Context, req *__NAME__dto.DemoReq) (res *__NAME__dto.DemoResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, facing.WithStatus(facing.New(enums.ErrInvalidParams), http.StatusBadRequest)
	}
	demoName, err := s.model.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, __NAME__model.ErrDemoNotFound) {
			return nil, facing.WithStatus(facing.New(enums.ErrNotFound), http.StatusNotFound)
		}
		return nil, err
	}
	return &__NAME__dto.DemoResp{ID: req.ID, Name: demoName}, nil
}
`

const moduleRouterTpl = `package __NAME__http

// __NAME___router.go — __NAME__ 模块路由自装配。
//
// 挂载点：应用装配层在 middleware.Freeze 之前调用 Setup__PASCAL__Routes(rg, db)。
// rg 的用途（受保护 / 页面）由应用提供；认证与 Casbin 必须已由应用挂好，
// 骨架不重复装配认证器。

import (
	"gorm.io/gorm"

	__NAME__contract "__MODULE__/internal/module/__NAME__/contract"
	__NAME__enums "__MODULE__/internal/module/__NAME__/enums"
	__NAME__model "__MODULE__/internal/module/__NAME__/model"
	__NAME__service "__MODULE__/internal/module/__NAME__/service"
	"__MODULE__/pkg/middleware"
)

// Setup__PASCAL__Routes 装配 __NAME__ 模块路由，返回模块契约。
func Setup__PASCAL__Routes(rg *middleware.RouteGroup, db *gorm.DB) __NAME__contract.__PASCAL__Service {
	svc := __NAME__service.NewService(__NAME__model.NewDemoModel(db))

	handle := NewHandle(svc)
	g := rg.Group("/__NAME__")
	g.GET("/demo", __NAME__enums.DemoRead, handle.GetDemo)
	return svc
}
`

const moduleHandlerTpl = `package __NAME__http

// __NAME___handle.go — __NAME__ 模块 HTTP handler（绑定 → 契约调用 → 统一响应）。

import (
	"github.com/gin-gonic/gin"

	__NAME__contract "__MODULE__/internal/module/__NAME__/contract"
	__NAME__dto "__MODULE__/internal/module/__NAME__/dto"
	"__MODULE__/pkg/response"
)

// Handle __NAME__ 模块 HTTP 出口。
type Handle struct {
	svc __NAME__contract.__PASCAL__Service
}

// NewHandle 构造。
func NewHandle(svc __NAME__contract.__PASCAL__Service) *Handle {
	return &Handle{svc: svc}
}

// GetDemo GET /demo?id=<uuid>（实际路径前缀由应用路由组决定）。
func (h *Handle) GetDemo(c *gin.Context) {
	var req __NAME__dto.DemoReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}
	resp, err := h.svc.GetDemo(c.Request.Context(), &req)
	if err != nil {
		response.ErrorStatusAuto(c, "__NAME__/demo", err)
		return
	}
	response.Success(c, resp)
}
`

const moduleReadmeTpl = "# __NAME__ 模块骨架\n" + `
由 ` + "`plugin module __NAME__ <module-path>`" + ` 生成。生成代码中的导入按传入的实际根模块路径书写；
把整个目录拷进 ` + "`internal/module/__NAME__/`" + ` 后 ` + "`go build ./...`" + ` 通过。

分层与既有模块一致：

- contract/  对外契约（跨模块调用方只 import 这里与 dto）
- dto/       请求 / 响应 DTO（ID 为字符串，form/json 绑定 + required/uuid 校验）
- model/     数据访问（只暴露本模块表的具名读方法，不暴露 DB）
- enums/     模块权限点（业务错误与消息复用 pkg/enums 与词库）
- service/   业务服务（实现契约，映射 400 / 404 业务错误）
- inbound/http/  路由自装配 + handler（JSON 数据出口）

## 装配触点（骨架不自动接线，需人工正式接入）

1. 路由挂载：应用装配层在 ` + "`middleware.Freeze()`" + ` 之前调用
   ` + "`http.Setup__PASCAL__Routes(rg, db)`" + `；` + "`rg`" + ` 是应用提供的 ` + "`*middleware.RouteGroup`" + `，
   调用方必须已经挂好认证与 Casbin，骨架不重复装配认证器。
2. 权限目录：` + "`__NAME__:demo_read`" + ` 需按项目权限迁移流程正式登记（人工接入）。
3. 迁移注册：建表 SQL 加入项目迁移目录并顺延编号（人工接入）。
4. 路由与权限登记完成后由应用调用 ` + "`middleware.Freeze()`" + `；冻结后注册会明确失败。
`
