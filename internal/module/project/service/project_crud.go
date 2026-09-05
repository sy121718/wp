package projectservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Create 创建站点工程与初始 SiteSettings。
func (s *Service) Create(ctx context.Context, req *projectdto.CreateReq) (res *projectdto.ProjectResp, err error) {
	if req == nil {
		return nil, errors.New(projectenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return nil, errors.New(projectenums.ErrInvalidName)
	}
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &projectmodel.ProjectEntity{
		ID: uuid.NewString(), Name: name, Settings: settings, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.model.Create(ctx, e); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Detail 查询站点工程详情。
func (s *Service) Detail(ctx context.Context, req *projectdto.DetailReq) (res *projectdto.ProjectResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(projectenums.ErrProjectNotFound)
	}
	e, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(projectenums.ErrProjectNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新站点工程名称与 SiteSettings。
func (s *Service) Update(ctx context.Context, req *projectdto.UpdateReq) (res *projectdto.ProjectResp, err error) {
	if req == nil {
		return nil, errors.New(projectenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(projectenums.ErrProjectNotFound)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return nil, errors.New(projectenums.ErrInvalidName)
	}
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, err
	}
	if _, err = s.model.GetByID(ctx, req.ID); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(projectenums.ErrProjectNotFound)
	} else if err != nil {
		return nil, err
	}
	if err = s.model.Update(ctx, req.ID, name, settings, time.Now().UTC()); err != nil {
		return nil, err
	}
	e, err := s.model.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Exists 判断站点工程是否存在，供其他模块通过契约校验归属。
func (s *Service) Exists(ctx context.Context, id string) (exists bool, err error) {
	if strings.TrimSpace(id) == "" {
		return false, nil
	}
	if _, err = s.model.GetByID(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// normalizeSettings 校验设置必须为 JSON 对象；空值规范为 {}。
func normalizeSettings(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New(projectenums.ErrInvalidSettings)
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, errors.New(projectenums.ErrInvalidSettings)
	}
	return normalized, nil
}

func toResp(e *projectmodel.ProjectEntity) *projectdto.ProjectResp {
	return &projectdto.ProjectResp{
		ID: e.ID, Name: e.Name, Settings: e.Settings, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}
