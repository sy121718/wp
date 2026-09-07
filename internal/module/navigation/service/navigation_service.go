// Package navigationservice 实现 navigation 模块业务用例（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
package navigationservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/templates"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// 导航类型白名单（与迁移 046 的 CHECK 约束对齐）。
const (
	kindHeader = "header"
	kindFooter = "footer"
)

// Service navigation 模块业务实现。
type Service struct {
	m *navigationmodel.Model
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *navigationmodel.Model) *Service { return &Service{m: m} }

// 编译期契约断言。
var _ navigationcontract.NavigationService = (*Service)(nil)

// Create 新建导航项：校验 kind/path 基础规则 + 同工程同 kind 同 path 唯一。
func (s *Service) Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	title := strings.TrimSpace(req.Title)
	path := strings.TrimSpace(req.Path)
	kind := strings.TrimSpace(req.Kind)
	if projectID == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	if err = validateField(title, path, kind); err != nil {
		return nil, err
	}
	parentID := normalizeParentID(req.ParentID)

	exists, err := s.m.ExistsPath(ctx, projectID, kind, path, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New(navigationenums.ErrPathTaken)
	}

	now := time.Now().UTC()
	e := &navigationmodel.NavigationEntity{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Path: path,
		Kind: kind, ParentID: parentID, SortOrder: req.SortOrder,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新导航项：仅更新传入的非空字段，变更 path/kind 时重校验唯一性。
func (s *Service) Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	updates := map[string]any{"updated_at": time.Now().UTC()}
	title, path, kind := e.Title, e.Path, e.Kind

	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		updates["title"] = title
	}
	if req.Path != nil {
		path = strings.TrimSpace(*req.Path)
		updates["path"] = path
	}
	if req.Kind != nil {
		kind = strings.TrimSpace(*req.Kind)
		updates["kind"] = kind
	}
	if err = validateField(title, path, kind); err != nil {
		return nil, err
	}
	if req.ParentID != nil {
		updates["parent_id"] = normalizeParentID(req.ParentID)
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}

	// path/kind 任一变化时重校验同工程同 kind 同 path 唯一（排除自身）。
	if req.Path != nil || req.Kind != nil {
		exists, err := s.m.ExistsPath(ctx, e.ProjectID, kind, path, e.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New(navigationenums.ErrPathTaken)
		}
	}

	if err = s.m.Save(ctx, e.ID, updates); err != nil {
		return nil, err
	}
	updated, err := s.m.Get(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	return toResp(updated), nil
}

// Get 按 ID 查询导航项。
func (s *Service) Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, strings.TrimSpace(req.ID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(navigationenums.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// List 按工程（可选 kind）列出导航项，sort_order 升序。
func (s *Service) List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(navigationenums.ErrInvalidParam)
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "" && !isValidKind(kind) {
		return nil, errors.New(navigationenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, strings.TrimSpace(req.ProjectID), kind)
	if err != nil {
		return nil, err
	}
	out := make([]*navigationdto.NavigationResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// Delete 删除导航项。
func (s *Service) Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	if _, err = s.m.Get(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(navigationenums.ErrNotFound)
	} else if err != nil {
		return err
	}
	return s.m.Delete(ctx, id)
}

// Render 渲染该工程该 kind 的导航 HTML 片段（Jet 模板渲染）。
// 根节点平铺 <a>，子节点按 parent_id 嵌套 <ul><li>；title/path 由 Jet 默认转义。
func (s *Service) Render(ctx context.Context, projectID, kind string) (htmlStr string, err error) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" {
		return "", errors.New(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return "", errors.New(navigationenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, projectID, kind)
	if err != nil {
		return "", err
	}
	return renderNavigation(rows)
}

// renderNavigation 由导航实体构建树形视图并经 Jet 渲染 HTML 片段。
func renderNavigation(rows []*navigationmodel.NavigationEntity) (string, error) {
	children := make(map[string][]*navigationmodel.NavigationEntity)
	var roots []*navigationmodel.NavigationEntity
	for _, r := range rows {
		if r.ParentID == nil || *r.ParentID == "" {
			roots = append(roots, r)
			continue
		}
		children[*r.ParentID] = append(children[*r.ParentID], r)
	}
	rootViews := make([]navNodeView, 0, len(roots))
	for _, root := range roots {
		rootViews = append(rootViews, buildNodeView(root, children))
	}
	return templates.RenderFragment("navigation", struct {
		Roots []navNodeView
	}{Roots: rootViews})
}

// navNodeView 导航节点视图（Jet 模板渲染数据，树形）。
type navNodeView struct {
	Title    string
	Path     string
	Children []navNodeView
}

// buildNodeView 把实体树转成视图树（父 → 子递归）。
func buildNodeView(n *navigationmodel.NavigationEntity, children map[string][]*navigationmodel.NavigationEntity) navNodeView {
	v := navNodeView{Title: n.Title, Path: n.Path}
	for _, k := range children[n.ID] {
		v.Children = append(v.Children, buildNodeView(k, children))
	}
	return v
}

// validateField 校验 title/path/kind 基础规则（不含唯一性）。
func validateField(title, path, kind string) error {
	if title == "" {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New(navigationenums.ErrInvalidParam)
	}
	if !isValidKind(kind) {
		return errors.New(navigationenums.ErrInvalidKind)
	}
	return nil
}

// isValidKind 判断导航类型是否为 header/footer。
func isValidKind(kind string) bool {
	return kind == kindHeader || kind == kindFooter
}

// normalizeParentID 把空字符串父 ID 规范为 nil（表示根导航项）。
func normalizeParentID(p *string) *string {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

// toResp 实体 → 响应。
func toResp(e *navigationmodel.NavigationEntity) *navigationdto.NavigationResp {
	return &navigationdto.NavigationResp{
		ID: e.ID, ProjectID: e.ProjectID, Title: e.Title, Path: e.Path,
		Kind: e.Kind, ParentID: e.ParentID, SortOrder: e.SortOrder,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
