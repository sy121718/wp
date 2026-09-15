package presentationservice

// presentation_instance.go — 实例的读写入口（CRUD 与响应组装）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

// CreateInstance 创建自动发布实例：解析模板 → 编译 → 发布 → 记快照/产物/依赖。
func (s *Service) CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" || req.URLPath == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 实例级互斥：同一实体的「构建 → 落库 → 激活」整体串行，
	// 并发请求不会各自推进产物版本号，也不会交错覆盖 active 指针。
	lock := s.lockInstance(req.EntityType, req.EntityID)
	lock.Lock()
	defer lock.Unlock()

	// 角色（审计 EDT-004）：空 = detail，既有调用方逐字不变。
	role := strings.TrimSpace(req.InstanceRole)
	if role == "" {
		role = presentationmodel.InstanceRoleDetail
	}
	if !presentationmodel.IsValidInstanceRole(role) {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 同实体**同角色**已存在实例 → 视为幂等（返回已有；无需再解析工程）。
	// 必须带角色：同一个分类既有详情页也可能有归档页，只按实体查会把先建的当成
	// 「已存在」返回 —— 于是「给分类建归档页」静默变成「拿到详情页实例」。
	if existing, gerr := s.m.GetInstanceByEntityRole(ctx, req.EntityType, req.EntityID, role); gerr == nil {
		return s.toResp(ctx, existing)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 解析模板版本 + 实体解析器。req.TemplateID 非空 = 发布时显式指定用哪套命名模板
	// （issue #14 验收 2）；为空 = 按实体类型取默认模板（既有行为逐字不变）。
	tpl, err := s.resolveTemplate(ctx, req.EntityType, req.TemplateID)
	if err != nil {
		return nil, err
	}
	// 路径先归一化：产物 canonical、访问面符号链接与 page_routes 登记必须落在
	// 同一个字符串上（FS 侧本来就归一化），否则 /shop/x/ 与 /shop/x 会被当成
	// 两个路径，路由行指向的位置与实际内容不符。
	logicalPath, err := s.normalizeLogicalPath(ctx, projectID, req.URLPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrInvalidPath, err)
	}
	// 占用预检：逻辑路径下全部语言访问路径 + 逻辑路径本身。
	if err = s.ensureLogicalPathFree(ctx, projectID, logicalPath, ""); err != nil {
		return nil, err
	}
	// 记实例（url_path 存逻辑路径；各语言访问路径在 publication 表）。
	now := time.Now().UTC()
	inst := &presentationmodel.InstanceEntity{
		ID: uuid.NewString(), ProjectID: projectID, EntityType: req.EntityType,
		EntityID: req.EntityID, InstanceRole: role, URLPath: logicalPath, TemplateID: tpl.TemplateID,
		Stale: true, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}
	if _, err = s.publishAllLangs(ctx, inst, tpl, logicalPath); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// GetByEntity 按内容实体查询实例（后台「详情页模板」页读当前绑定与发布状态）。
func (s *Service) GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstanceByEntity(ctx, req.EntityType, req.EntityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstance(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

// List 按类型列表。
func (s *Service) List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error) {
	if req == nil {
		req = &presentationdto.ListReq{}
	}
	rows, err := s.m.ListInstances(ctx, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*presentationdto.InstanceResp, 0, len(rows))
	for _, r := range rows {
		resp, rerr := s.toResp(ctx, r)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, resp)
	}
	return out, nil
}

// Delete 删除实例（级联删本模块从属行 + 反激活 URL）。
func (s *Service) Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstance(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(presentationenums.ErrNotFound)
		}
		return err
	}
	// 反激活失败必须中止删除：否则实例行已删、URL 占用残留，后续同路径
	// 发布/激活会被「已占用」拒绝且无实例可查（状态分裂）。
	//
	// 要覆盖**全部**已激活路径而不只是当前 url_path：改过 URL 的实例还有一条
	// 旧路径的 301 链接，漏掉它 = 实例删了、线上旧路径仍 301 到一个死页面。
	for _, p := range s.instanceActivePaths(ctx, inst) {
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("删除实例前反激活 URL 失败 %s: %w", p, derr)
		}
	}
	// 路由占用同步释放：只删实例行会把 page_routes 里本实例的 active/redirect
	// 行留成悬空引用（外键指向已删除的实例），同路径再发布永远被拒。
	if s.routes != nil {
		if rerr := s.routes.DeleteRoutesByPresentation(ctx, &pubcontract.DeleteRoutesByPresentationReq{
			ProjectID: inst.ProjectID, PresentationID: inst.ID,
		}); rerr != nil {
			return fmt.Errorf("删除实例前释放路由占用失败: %w", rerr)
		}
	}
	return s.m.DeleteInstance(ctx, req.ID)
}

// instanceActivePaths 实例在访问面上已激活的全部路径（当前路径 + 历史 301 路径）。
//
// 查询失败时退化为当前 url_path：清理不完整优于因查询失败而删不掉实例 ——
// 前者是可发现、可重试的残留，后者是卡死的资源。
func (s *Service) instanceActivePaths(ctx context.Context, inst *presentationmodel.InstanceEntity) []string {
	var paths []string
	if pubs, perr := s.m.ListActivePathsForInstance(ctx, inst.ID); perr == nil && len(pubs) > 0 {
		paths = append(paths, pubs...)
	} else if inst.URLPath != "" {
		paths = append(paths, inst.URLPath)
	}
	if s.routes == nil {
		return dedupePaths(paths)
	}
	extra, err := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{
		ProjectID: inst.ProjectID, PresentationID: inst.ID,
	})
	if err != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例已激活路径失败，仅清理已登记路径: " + err.Error())
		return dedupePaths(paths)
	}
	return dedupePaths(append(paths, extra...))
}

func dedupePaths(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// findByEntityID 按内容实体 ID 反查实例。
func (s *Service) findByEntityID(ctx context.Context, entityID string) (*presentationmodel.InstanceEntity, error) {
	rows, err := s.m.ListInstances(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.EntityID == entityID {
			return r, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// toResp 实体 → 响应。
//
// Status 不再是表列：由 active_artifact_id 指针推导（active / draft），
// ArtifactHash 取活跃产物行的哈希。
func (s *Service) toResp(ctx context.Context, e *presentationmodel.InstanceEntity) (resp *presentationdto.InstanceResp, err error) {
	resp = &presentationdto.InstanceResp{
		InstanceRole: e.InstanceRole,
		ID:           e.ID, ProjectID: e.ProjectID, EntityType: e.EntityType, EntityID: e.EntityID,
		URLPath: e.URLPath, TemplateID: e.TemplateID, Stale: e.Stale,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if e.ActiveArtifactID != nil {
		resp.Status = presentationenums.StatusActive
		resp.ArtifactID = *e.ActiveArtifactID
		if art, aerr := s.m.GetArtifact(ctx, *e.ActiveArtifactID); aerr == nil {
			resp.ArtifactHash = art.ArtifactHash
		}
	} else {
		resp.Status = presentationenums.StatusDraft
	}
	if e.CurrentSnapshotID != nil {
		resp.SnapshotID = *e.CurrentSnapshotID
	}
	return resp, nil
}
