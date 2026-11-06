package pluginservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sync"
	"time"

	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginmodel "go_wp/internal/module/plugin/model"

	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

type Service struct {
	m          *pluginmodel.Model
	adminAuthz admincontract.AuthzContextService

	asmMu          sync.Mutex
	asmFingerprint string
	asmCache       *plugincontract.Assembly
}

func NewService(m *pluginmodel.Model) *Service { return &Service{m: m} }

func (s *Service) SetAdminAuthz(a admincontract.AuthzContextService) {
	s.adminAuthz = a
}

func (s *Service) AdminAuthz() admincontract.AuthzContextService {
	return s.adminAuthz
}

var _ plugincontract.PluginService = (*Service)(nil)

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

func (s *Service) Toggle(ctx context.Context, req *plugindto.ToggleReq) (err error) {
	if req == nil || req.ID == "" {
		return pluginenums.Err(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return pluginenums.Err(pluginenums.ErrPluginNotFound)
		}
		return err
	}
	row.Enabled = req.Enabled
	row.UpdatedAt = time.Now().UTC()
	return s.m.Update(ctx, row)
}

func (s *Service) Uninstall(ctx context.Context, req *plugindto.UninstallReq) (err error) {
	if req == nil || req.ID == "" {
		return pluginenums.Err(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return pluginenums.Err(pluginenums.ErrPluginNotFound)
		}
		return err
	}

	// 删注册行先于删 schema：注册行是「这个插件存在」的唯一事实源，
	// 先删它，schema 与文件即使随后清理失败也只是可重试的残留（幂等 DROP / RemoveAll）。
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return fmt.Errorf("%s: %w", pluginenums.ErrUninstallFailed, err)
	}
	if row.SchemaVersion > 0 {
		if err = s.m.Exec(ctx, dropSchemaSQL(row.PluginID)); err != nil {
			// 注册行已删：对外的结论是「插件已卸载，残留 schema 待清理」，
			// 下一轮同 id 安装会重建 schema，不会悬空。
			logger.Scene("plugin").With("plugin_id", row.PluginID).
				Error(err, "插件已卸载，但 schema 清理失败（残留待人工或下次安装清理）")
		}
	}
	removePluginStorage(row.PluginID)
	return nil
}

func (s *Service) Detail(ctx context.Context, req *plugindto.DetailReq) (res *plugindto.PluginResp, err error) {
	if req == nil || req.ID == "" {
		return nil, pluginenums.Err(pluginenums.ErrInvalidParam)
	}
	row, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pluginenums.Err(pluginenums.ErrPluginNotFound)
		}
		return nil, err
	}
	return toResp(row, true), nil
}

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

func componentCount(raw []byte) int {
	var m struct {
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0
	}
	return len(m.Components)
}
