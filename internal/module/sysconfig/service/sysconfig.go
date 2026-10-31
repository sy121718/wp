package sysconfigservice

import (
	"context"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/module/sysconfig/dto"
	"go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

type Service struct {
	m *sysconfigmodel.Model

	onChanged func()

	countryLabels sync.Map
}

type countryLabelEntry struct {
	index   map[string]string
	expires time.Time
}

const countryLabelTTL = 5 * time.Minute

var (
	_ sysconfigcontract.Service      = (*Service)(nil)
	_ sysconfigcontract.ConfigReader = (*Service)(nil)

	_ sysconfigcontract.DictReader = (*Service)(nil)
)

func NewService(m *sysconfigmodel.Model, onChanged func()) *Service {
	return &Service{m: m, onChanged: onChanged}
}

func groupOf(e *sysconfigmodel.SysConfigEntity) *sysconfigdto.Group {
	if e == nil {
		return nil
	}
	data := map[string]any(nil)
	if len(e.ConfigData) > 0 {
		data = make(map[string]any, len(e.ConfigData))
		for k, v := range e.ConfigData {
			data[k] = v
		}
	}
	return &sysconfigdto.Group{
		Key:        e.GroupKey,
		Name:       e.GroupName,
		Data:       data,
		Remark:     e.Remark,
		Status:     e.Status,
		Version:    e.Version,
		UpdateTime: utils.NewJSONTime(e.UpdateTime),
	}
}

func (s *Service) GetGroup(ctx context.Context, groupKey string) (res *sysconfigdto.Group, err error) {
	key := strings.TrimSpace(groupKey)
	if key == "" {
		return nil, sysconfigenums.Err(sysconfigenums.ErrInvalidParam)
	}
	e, err := s.m.FindByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, sysconfigcontract.ErrGroupNotFound
	}
	return groupOf(e), nil
}

func (s *Service) ListGroups(ctx context.Context) (res []sysconfigdto.Group, err error) {
	rows, err := s.m.ListByStatus(ctx, sysconfigmodel.StatusEnabled)
	if err != nil {
		return nil, err
	}
	res = make([]sysconfigdto.Group, 0, len(rows))
	for i := range rows {
		res = append(res, *groupOf(&rows[i]))
	}
	return res, nil
}

func (s *Service) SetGroup(ctx context.Context, req *sysconfigdto.SetGroupReq) (res *sysconfigdto.Group, err error) {
	if req == nil {
		return nil, sysconfigenums.Err(sysconfigenums.ErrInvalidParam)
	}
	key := strings.TrimSpace(req.GroupKey)
	if key == "" || len(req.Data) == 0 {
		return nil, sysconfigenums.Err(sysconfigenums.ErrInvalidParam)
	}
	if req.Version <= 0 {

		return nil, sysconfigenums.Err(sysconfigenums.ErrVersionRequired)
	}

	rows, err := s.m.UpdateDataWithVersion(ctx, key, req.Version, sysconfigmodel.JSONMap(req.Data), req.UpdateBy)
	if err != nil {
		logger.Scene("sysconfig").With("group", key).Error(err, "系统配置保存失败")
		return nil, err
	}
	if rows == 0 {
		return nil, s.reportNoRows(ctx, key, req.Version)
	}

	if s.onChanged != nil {
		s.onChanged()
	}

	saved, gerr := s.GetGroup(ctx, key)
	if gerr != nil {
		logger.Scene("sysconfig").With("group", key).Error(gerr, "系统配置已保存但读回失败（返回降级值）")
		return &sysconfigdto.Group{
			Key: key, Data: req.Data, Status: sysconfigmodel.StatusEnabled,
			Version: req.Version + 1, UpdateTime: utils.NewJSONTime(time.Now()),
		}, nil
	}
	return saved, nil
}

func (s *Service) reportNoRows(ctx context.Context, groupKey string, expectedVersion int64) error {
	current, found, err := s.m.VersionOf(ctx, groupKey)
	if err != nil {
		logger.Scene("sysconfig").With("group", groupKey).Error(err, "系统配置保存冲突归因失败（按版本冲突处理）")
		return sysconfigenums.Err(sysconfigenums.ErrVersionConflict)
	}
	if !found {
		return sysconfigcontract.ErrGroupNotFound
	}
	logger.Scene("sysconfig").
		With("group", groupKey).
		With("expectedVersion", expectedVersion).
		With("currentVersion", current).
		Warn("系统配置保存被乐观锁拒绝（库内版本已变，需重新读取后提交）")
	return sysconfigenums.Err(sysconfigenums.ErrVersionConflict)
}

func (s *Service) SetGroups(ctx context.Context, req *sysconfigdto.SetGroupsReq) (res []sysconfigdto.Group, err error) {
	if req == nil || len(req.Groups) == 0 {
		return nil, sysconfigenums.Err(sysconfigenums.ErrInvalidParam)
	}
	keys := make([]string, 0, len(req.Groups))
	for i := range req.Groups {
		g := &req.Groups[i]
		key := strings.TrimSpace(g.GroupKey)
		if key == "" || len(g.Data) == 0 {
			return nil, sysconfigenums.Err(sysconfigenums.ErrInvalidParam)
		}
		if g.Version <= 0 {
			return nil, sysconfigenums.Err(sysconfigenums.ErrVersionRequired)
		}
		keys = append(keys, key)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		for i := range req.Groups {
			g := &req.Groups[i]
			key := strings.TrimSpace(g.GroupKey)
			rows, uerr := s.m.UpdateDataWithVersionTx(tx, key, g.Version, sysconfigmodel.JSONMap(g.Data), req.UpdateBy)
			if uerr != nil {
				return uerr
			}
			if rows == 0 {

				_, found, verr := s.m.VersionOfTx(tx, key)
				if verr != nil {
					logger.Scene("sysconfig").With("group", key).Error(verr, "多组保存冲突归因失败（按版本冲突处理）")
					return sysconfigenums.Err(sysconfigenums.ErrVersionConflict)
				}
				if !found {
					return sysconfigenums.Err(sysconfigenums.ErrGroupNotFound)
				}
				return sysconfigenums.Err(sysconfigenums.ErrVersionConflict)
			}
		}
		return nil
	})
	if err != nil {
		logger.Scene("sysconfig").With("groups", strings.Join(keys, ",")).Error(err, "系统配置多组保存失败（整批回滚）")
		return nil, err
	}

	if s.onChanged != nil {
		s.onChanged()
	}

	res = make([]sysconfigdto.Group, 0, len(keys))
	for _, key := range keys {
		saved, gerr := s.GetGroup(ctx, key)
		if gerr != nil {
			logger.Scene("sysconfig").With("group", key).Error(gerr, "系统配置已保存但读回失败")
			continue
		}
		res = append(res, *saved)
	}
	return res, nil
}
