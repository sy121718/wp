// Package pluginservice 插件管理业务：安装/列表/启停/卸载 + 编译装配。
package pluginservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginmodel "go_wp/internal/module/plugin/model"

	"gorm.io/gorm"
)

// Service 插件模块业务实现。
type Service struct {
	m          *pluginmodel.Model
	adminAuthz admincontract.AuthzContextService
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *pluginmodel.Model) *Service { return &Service{m: m} }

// SetAdminAuthz 注入 admin 的对外权限上下文查询能力。
//
// 插件是未来外部插件的宿主：插件运行时需要读取「当前用户在其角色/权限/超管
// 范围内的上下文」，本方法把 admin 的只读权限契约注入插件模块，供插件经
// contract 访问器 AdminAuthz() 获取，避免插件直接依赖 admin 的 model/service。
func (s *Service) SetAdminAuthz(a admincontract.AuthzContextService) {
	s.adminAuthz = a
}

// AdminAuthz 对外暴露 admin 权限上下文查询服务（供插件/装配方经 plugin contract 消费）。
// 未注入时返回 nil，调用方应判空降级。
func (s *Service) AdminAuthz() admincontract.AuthzContextService {
	return s.adminAuthz
}

// 编译期契约断言。
var _ plugincontract.PluginService = (*Service)(nil)

// List 全部插件列表。
func (s *Service) List(ctx context.Context) (list []*plugindto.PluginResp, err error) {
	rows, err := s.m.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*plugindto.PluginResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r, false))
	}
	return out, nil
}

// Toggle 启停插件（影响编译装配与工作台组件库，无需重建——下次构建自然生效）。
func (s *Service) Toggle(ctx context.Context, req *plugindto.ToggleReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(pluginenums.ErrPluginNotFound)
		}
		return err
	}
	row.Enabled = req.Enabled
	row.UpdatedAt = time.Now().UTC()
	return s.m.Update(ctx, row)
}

// Uninstall 卸载：删注册行 + 级联 DROP schema（L1）+ 删存储目录。
func (s *Service) Uninstall(ctx context.Context, req *plugindto.UninstallReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(pluginenums.ErrPluginNotFound)
		}
		return err
	}
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return fmt.Errorf("%s: %w", pluginenums.ErrUninstallFailed, err)
	}
	// L1 数据层 schema 级联清理（docs/06 §8.2：DROP SCHEMA ... CASCADE + registry 除名）。
	if row.SchemaVersion > 0 {
		if err = s.m.Exec(ctx, dropSchemaSQL(row.PluginID)); err != nil {
			return fmt.Errorf("%s: %w", pluginenums.ErrUninstallFailed, err)
		}
	}
	removePluginStorage(row.PluginID)
	return nil
}

// Detail 插件详情（含 manifest 快照）。
func (s *Service) Detail(ctx context.Context, req *plugindto.DetailReq) (res *plugindto.PluginResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pluginenums.ErrPluginNotFound)
		}
		return nil, err
	}
	return toResp(row, true), nil
}

// toResp 实体 → 响应。
func toResp(r *pluginmodel.Entity, withManifest bool) *plugindto.PluginResp {
	resp := &plugindto.PluginResp{
		ID:             r.PluginID,
		Name:           r.Name,
		Version:        r.Version,
		SchemaVersion:  r.SchemaVersion,
		Enabled:        r.Enabled,
		ComponentCount: componentCount(r.Manifest),
		InstalledAt:    r.InstalledAt.Format("2006-01-02 15:04"),
		UpdatedAt:      r.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if withManifest && len(r.Manifest) > 0 {
		resp.Manifest = json.RawMessage(r.Manifest)
	}
	return resp
}

// componentCount 从 manifest 快照解析组件数（防御：解析失败返回 0）。
func componentCount(raw []byte) int {
	var m struct {
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0
	}
	return len(m.Components)
}
