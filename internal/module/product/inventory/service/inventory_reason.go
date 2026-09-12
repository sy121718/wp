// inventory_reason.go — 变动原因字典（issue #16 验收 4）。
//
// 「变动原因覆盖出 / 入 / 调整各枚举，含自定义原因（引用可维护字典，不用自由文本）」：
//
//	· 内置原因：迁移 103 seed，project_id IS NULL（全工程可见，不可修改）；
//	· 自定义原因：工程内 code 唯一，可改名 / 停用（停用后不再能被新变动引用，
//	  历史流水不受影响 —— 流水里存的是 code 快照）。
//
// 变动入口只接受字典里存在的 code，且原因方向必须与本次变动方向一致。
package inventoryservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
)

// maxReasonCodeLen 原因 code 长度上限。
const maxReasonCodeLen = 32

// ListReasons 变动原因列表（工程自定义 + 全部内置）。
func (s *Service) ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	filter := inventorymodel.ReasonFilter{
		Keyword:         strings.TrimSpace(req.Keyword),
		IncludeDisabled: req.IncludeDisabled,
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return nil, err
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListReasons(ctx, filter)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.ReasonResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toReasonResp(r))
	}
	return list, nil
}

// CreateReason 新建自定义变动原因（code 工程内唯一，方向一经确定不可改）。
func (s *Service) CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrReasonNameRequired)
	}
	code, err := normalizeReasonCode(req.Code)
	if err != nil {
		return nil, err
	}
	direction, err := normalizeDirection(req.Direction)
	if err != nil {
		return nil, errors.New(inventoryenums.ErrReasonDirectionInvalid)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 内置原因占用同一命名空间：自定义 code 不能与内置撞名（否则解析结果不确定）。
	if taken, cerr := s.m.ReasonCodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrReasonCodeTaken)
	}
	now := time.Now().UTC()
	pid := projectID
	e := &inventorymodel.ReasonEntity{
		ID: uuid.NewString(), ProjectID: &pid, Code: code, Name: name, Direction: direction,
		IsBuiltin: false, Status: inventoryenums.StatusActive, Sort: req.Sort,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// UpdateReason 修改自定义变动原因（内置原因一律拒绝）。
func (s *Service) UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	e, err := s.m.GetReason(ctx, req.ID)
	if err != nil {
		return nil, mapReasonNotFound(err)
	}
	if e.IsBuiltin {
		return nil, errors.New(inventoryenums.ErrReasonBuiltin)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrReasonNameRequired)
		}
		e.Name = name
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
			return nil, errors.New(inventoryenums.ErrReasonStatusInvalid)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// resolveReason 解析变动原因：必须是字典里的 active 条目，且方向与本次变动一致。
func (s *Service) resolveReason(ctx context.Context, projectID, code, direction string) (r *inventorymodel.ReasonEntity, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New(inventoryenums.ErrStockReasonRequired)
	}
	e, ferr := s.m.FindReasonByCode(ctx, projectID, code)
	if ferr != nil {
		return nil, mapReasonNotFound(ferr)
	}
	if e.Direction != direction {
		return nil, errors.New(inventoryenums.ErrReasonDirectionMismatch)
	}
	return e, nil
}

// normalizeReasonCode 归一并校验原因 code：小写字母 / 数字 / 下划线，1..maxReasonCodeLen。
func normalizeReasonCode(code string) (out string, err error) {
	out = strings.ToLower(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrReasonCodeRequired)
	}
	if len(out) > maxReasonCodeLen {
		return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
	}
	for _, r := range out {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
		}
	}
	return out, nil
}

// mapReasonNotFound 行不存在 → 业务错误，其余原样透出。
func mapReasonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrReasonNotFound)
	}
	return err
}

// toReasonResp 实体 → 响应。
func toReasonResp(e *inventorymodel.ReasonEntity) *inventorydto.ReasonResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.ReasonResp{
		ID: e.ID, Code: e.Code, Name: e.Name, Direction: e.Direction,
		IsBuiltin: e.IsBuiltin, Status: e.Status, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ProjectID != nil {
		resp.ProjectID = *e.ProjectID
	}
	return resp
}
