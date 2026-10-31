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

	if row.SchemaVersion > 0 {
		if err = s.m.Exec(ctx, dropSchemaSQL(row.PluginID)); err != nil {
			return fmt.Errorf("%s: %w", pluginenums.ErrUninstallFailed, err)
		}
	}
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return fmt.Errorf("%s: %w", pluginenums.ErrUninstallFailed, err)
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
