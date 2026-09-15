// module.go 业务模块骨架生成器：`plugin module <name>` 生成新业务模块目录骨架。
//
// OSS-017：新增业务模块此前要手工铺设 contract / service / model / enums /
// inbound / http 六层目录与样板代码。本生成器产出与既有模块（参照 mail /
// product 的分层）一致的骨架：生成的包相互引用闭合、只依赖标准库 + gin +
// gorm（go.mod 既有依赖），拷进 internal/module/ 后直接 go build 通过。
// README 里列出装配期仍需人工接入的触点（路由挂载、迁移注册、菜单与权限点）。
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleFiles 生成业务模块骨架文件集合（相对模块根目录的路径 → 内容，
// 不含 name 前缀；WriteModule 负责拼 name/ 落盘）。
// name 为模块目录名（小写字母开头，如 "member"）。
func ModuleFiles(name string) (map[string]string, error) {
	if !idRe.MatchString(name) {
		return nil, fmt.Errorf("模块名 %q 非法（小写字母开头，2~61 位，仅字母数字下划线连字符）", name)
	}
	pascal := pascal(name)
	return map[string]string{
		"README.md":                           moduleReadme(name, pascal),
		"contract/" + name + "_contract.go":   moduleContract(name, pascal),
		"dto/" + name + "_req.go":             moduleDTOReq(name),
		"dto/" + name + "_resp.go":            moduleDTOResp(name),
		"enums/" + name + "_enums.go":         moduleEnums(name),
		"model/" + name + "_model.go":         moduleModel(name),
		"service/" + name + "_service.go":     moduleService(name, pascal),
		"inbound/http/" + name + "_router.go": moduleRouter(name, pascal),
		"inbound/http/" + name + "_handle.go": moduleHandle(name, pascal),
	}, nil
}

// WriteModule 落盘到当前目录（生成 name/ 目录；已存在则报错不覆盖）。
func WriteModule(name string) error {
	files, err := ModuleFiles(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("目录 %s 已存在，拒绝覆盖", name)
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

// pascal 模块名转 PascalCase（连字符/下划线分词，"member_card" → "MemberCard"）。
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

// moduleContract 对外契约（参照 mailcontract：只放对外能力，不定义外部依赖）。
func moduleContract(name, pascal string) string {
	return fmt.Sprintf(`package %scontract

// %s_contract.go — %s 模块对外契约。
//
// 只放本模块对外暴露的能力，不定义外部依赖接口；跨模块调用方只 import
// 本契约与 dto，不 import model / service（internal/module 规范）。

import (
	"context"

	%sdto "go_wp/internal/module/%s/dto"
)

// %sService %s 域能力（骨架：一个最小读方法，按需扩展）。
type %sService interface {
	// GetDemo 按主键取一条示例记录（骨架占位，落地时替换为真实业务方法）。
	GetDemo(ctx context.Context, req *%sdto.DemoReq) (*%sdto.DemoResp, error)
}
`, name, name, name, name, name, pascal, name, pascal, name, name)
}

// q struct tag 里的反引号（模板 raw string 内无法直接书写，用常量拼接）。
const q = "\x60"

// moduleDTOReq 请求 DTO。
func moduleDTOReq(name string) string {
	return fmt.Sprintf(`package %sdto

// %s_req.go — %s 模块请求 DTO。

// DemoReq 示例请求（骨架占位，按需扩展字段与校验）。
type DemoReq struct {
	ID uint64 `+q+`json:"id" binding:"required"`+q+`
}
`, name, name, name)
}

// moduleDTOResp 响应 DTO。
func moduleDTOResp(name string) string {
	return fmt.Sprintf(`package %sdto

// %s_resp.go — %s 模块响应 DTO。

// DemoResp 示例响应（骨架占位）。
type DemoResp struct {
	ID   uint64 `+q+`json:"id"`+q+`
	Name string `+q+`json:"name"`+q+`
}
`, name, name, name)
}

// moduleEnums 模块内统一出口常量（参照 mailenums）。
func moduleEnums(name string) string {
	return fmt.Sprintf(`package %senums

// %s_enums.go — %s 模块的响应消息与业务错误（模块内统一出口）。

const (
	MsgGetSuccess   = "%s.msg.getSuccess"
	MsgParamInvalid = "%s.msg.paramInvalid"
	ErrDemoNotFound = "%s.err.demoNotFound"
)
`, name, name, name, name, name, name)
}

// moduleModel 数据访问层（Repository 定位：只暴露具名方法，不暴露 DB 句柄）。
func moduleModel(name string) string {
	return fmt.Sprintf(`package %smodel

// %s_model.go — %s 模块数据访问单元（model 层定位是 Repository）。
//
// 只暴露具名方法，不暴露 DB(ctx) 句柄 —— 句柄是内部细节，
// service 禁止直接碰它（internal/module 规范）。

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ErrDemoNotFound 示例记录不存在。
var ErrDemoNotFound = errors.New("demo record not found")

// DemoModel %s 模块数据访问。
type DemoModel struct {
	db *gorm.DB
}

// NewDemoModel 构造（装配层注入连接）。
func NewDemoModel(db *gorm.DB) *DemoModel {
	return &DemoModel{db: db}
}

// tableName 示例表名（落地时改为真实表与 schema）。
func tableName() string {
	return "%s_demo"
}

// GetByID 按主键取一条示例记录（命名返回值）。
func (m *DemoModel) GetByID(ctx context.Context, id uint64) (demoName string, err error) {
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
`, name, name, name, name, name)
}

// moduleService 业务服务（实现契约；装配层注入 model）。
func moduleService(name, pascal string) string {
	return fmt.Sprintf(`package %sservice

// %s_service.go — %s 模块业务服务（实现 %scontract.%sService）。

import (
	"context"

	%scontract "go_wp/internal/module/%s/contract"
	%sdto "go_wp/internal/module/%s/dto"
	%smodel "go_wp/internal/module/%s/model"
)

// Service %s 模块业务服务。
type Service struct {
	model *%smodel.DemoModel
}

// NewService 构造（装配层注入 model）。
func NewService(m *%smodel.DemoModel) *Service {
	return &Service{model: m}
}

// 编译期断言：Service 实现模块契约（internal/module 规范）。
var _ %scontract.%sService = (*Service)(nil)

// GetDemo 实现契约的示例读方法（命名返回值，error 放最后）。
func (s *Service) GetDemo(ctx context.Context, req *%sdto.DemoReq) (res *%sdto.DemoResp, err error) {
	demoName, err := s.model.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return &%sdto.DemoResp{ID: req.ID, Name: demoName}, nil
}
`, name, name, name, name, pascal, name, name, name, name, name, name, name, name, name, name, pascal, name, name, name)
}

// moduleRouter 路由自装配（参照 mail_router.go：Setup 返回契约）。
func moduleRouter(name, pascal string) string {
	return fmt.Sprintf(`package %shttp

// %s_router.go — %s 模块路由自装配。
//
// 挂载点：在应用装配层调用 Setup%sRoutes(rg, db)（人工接入触点，
// 见本模块 README「装配触点」）。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	%scontract "go_wp/internal/module/%s/contract"
	%smodel "go_wp/internal/module/%s/model"
	%sservice "go_wp/internal/module/%s/service"
)

// Setup%sRoutes 装配 %s 模块路由，返回模块契约。
func Setup%sRoutes(rg *gin.RouterGroup, db *gorm.DB) %scontract.%sService {
	svc := %sservice.NewService(%smodel.NewDemoModel(db))

	handle := NewHandle(svc)
	g := rg.Group("/%s")
	g.GET("/demo", handle.GetDemo)
	return svc
}
`, name, name, name, pascal, name, name, name, name, name, name, pascal, name, pascal, name, pascal, name, name, name)
}

// moduleHandle HTTP handler（绑定请求 → 调契约 → 统一响应，走 pkg/response）。
func moduleHandle(name, pascal string) string {
	return fmt.Sprintf(`package %shttp

// %s_handle.go — %s 模块 HTTP handler（绑定 → 契约调用 → 统一响应）。

import (
	"github.com/gin-gonic/gin"

	%scontract "go_wp/internal/module/%s/contract"
	%sdto "go_wp/internal/module/%s/dto"
	%senums "go_wp/internal/module/%s/enums"
	"go_wp/pkg/response"
)

// Handle %s 模块 HTTP 出口。
type Handle struct {
	svc %scontract.%sService
}

// NewHandle 构造。
func NewHandle(svc %scontract.%sService) *Handle {
	return &Handle{svc: svc}
}

// GetDemo GET /api/%s/demo?id=1。
func (h *Handle) GetDemo(c *gin.Context) {
	var q struct {
		ID uint64 `+q+`form:"id" binding:"required"`+q+`
	}
	if err := c.ShouldBindQuery(&q); err != nil {
		response.ErrorWithMessage(c, 400, %senums.MsgParamInvalid)
		return
	}
	resp, err := h.svc.GetDemo(c.Request.Context(), &%sdto.DemoReq{ID: q.ID})
	if err != nil {
		response.ErrorAuto(c, 200, "%s/demo", err)
		return
	}
	response.SuccessWithMessage(c, %senums.MsgGetSuccess, resp)
}
`, name, name, name, name, name, name, name, name, name, name, name, pascal, name, pascal, name, name, name, name, name)
}

// moduleReadme 模块 README（列出仍需人工接入的装配触点）。
func moduleReadme(name, pascal string) string {
	return fmt.Sprintf(`# %s 模块骨架

由 `+q+`plugin module %s`+q+` 生成（OSS-017）。分层与既有模块一致：

- contract/  对外契约（跨模块调用方只 import 这里与 dto）
- dto/       请求 / 响应 DTO
- model/     数据访问（Repository 定位，只暴露具名方法）
- enums/     模块内统一出口常量
- service/   业务服务（实现契约）
- inbound/http/  路由自装配 + handler

生成的包相互引用闭合、只依赖标准库 + gin + gorm，拷进
`+q+`internal/module/%s/`+q+` 后直接 `+q+`go build ./...`+q+` 通过。

## 装配触点（骨架不自动接线，需人工接入）

1. 路由挂载：应用装配层调用 `+q+`http.Setup%sRoutes(rg, db)`+q+`；
2. 迁移注册：建表 SQL 加入 `+q+`public/migrations/register.go`+q+`（编号顺延）；
3. 权限点与菜单：需要后台入口时按 030/031 迁移模式补 seed；
4. 构建期能力：需要进可视化绑定时另行注册 CollectionSource（见 docs/06）。
`, name, name, name, pascal)
}
