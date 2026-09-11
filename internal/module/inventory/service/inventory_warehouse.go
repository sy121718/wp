// inventory_warehouse.go — 仓库实体读写（issue #15 验收 1）。
//
// 「必须有一个默认仓」是本文件维护的不变量：
//
//	· 工程内第一个仓自动成为默认仓（建仓即满足「未指定仓库时有兜底」）；
//	· 显式 IsDefault 即切换默认仓，同工程唯一（数据库部分唯一索引兜底并发）；
//	· 默认仓不能删除、不能停用、不能取消默认 —— 取消/删除的后果是
//	  「未指定仓库」再也找不到兜底，属于必须显式暴露的数据缺陷。
package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// CreateWarehouse 新建仓库（验收 1）。
func (s *Service) CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrWarehouseNameRequired)
	}
	code, err := normalizeCode(req.Code)
	if err != nil {
		return nil, err
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return nil, err
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.CodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrWarehouseCodeTaken)
	}
	// 工程内第一个仓自动成为默认仓：默认仓是「未指定仓库」的兜底，
	// 让「建了仓却没有默认仓」这种中间态不可能出现。
	existing, lerr := s.m.ListWarehouses(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	asDefault := req.IsDefault || len(existing) == 0
	now := time.Now().UTC()
	e := &inventorymodel.WarehouseEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, Status: status,
		IsDefault: asDefault, Sort: req.Sort,
		Metadata:  orJSON(req.Metadata, "{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateWarehouse(ctx, e, asDefault); err != nil {
		return nil, err
	}
	return toWarehouseResp(e), nil
}

// UpdateWarehouse 修改仓库（逐字段可选；nil = 本次不改）。
func (s *Service) UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	e, err := s.m.GetWarehouse(ctx, req.ID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	if req.Code != nil {
		code, cerr := normalizeCode(*req.Code)
		if cerr != nil {
			return nil, cerr
		}
		if taken, xerr := s.m.CodeExists(ctx, e.ProjectID, code, e.ID); xerr != nil {
			return nil, xerr
		} else if taken {
			return nil, errors.New(inventoryenums.ErrWarehouseCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrWarehouseNameRequired)
		}
		e.Name = name
	}
	if req.Status != nil {
		status, serr := normalizeStatus(*req.Status)
		if serr != nil {
			return nil, serr
		}
		// 默认仓必须保持启用：停用默认仓等于抽掉「未指定仓库」的兜底。
		if e.IsDefault && status != inventoryenums.StatusActive {
			return nil, errors.New(inventoryenums.ErrWarehouseIsDefault)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	if req.Metadata != nil {
		e.Metadata = orJSON(req.Metadata, "{}")
	}
	// 取消默认标记被拒绝（先指定另一个默认仓）：兜底不能凭空消失。
	if req.IsDefault != nil && !*req.IsDefault && e.IsDefault {
		return nil, errors.New(inventoryenums.ErrWarehouseIsDefault)
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateWarehouse(ctx, e); err != nil {
		return nil, err
	}
	if req.IsDefault != nil && *req.IsDefault && !e.IsDefault {
		if err = s.m.SetDefaultWarehouse(ctx, e.ProjectID, e.ID); err != nil {
			return nil, err
		}
		e.IsDefault = true
	}
	return toWarehouseResp(e), nil
}

// GetWarehouse 仓库详情。
func (s *Service) GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	e, err := s.m.GetWarehouse(ctx, req.ID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	return toWarehouseResp(e), nil
}

// ListWarehouses 某工程的仓库列表（默认仓在最前）。
func (s *Service) ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error) {
	projectID := ""
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
	}
	if projectID == "" {
		if projectID, err = s.resolveProjectID(ctx, ""); err != nil {
			return nil, err
		}
	}
	rows, err := s.m.ListWarehouses(ctx, projectID)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseResp, 0, len(rows))
	for _, e := range rows {
		list = append(list, toWarehouseResp(e))
	}
	return list, nil
}

// DeleteWarehouse 删除仓库（默认仓与仓内有非零库存时拒绝）。
func (s *Service) DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(inventoryenums.ErrInvalidParam)
	}
	e, err := s.m.GetWarehouse(ctx, req.ID)
	if err != nil {
		return mapWarehouseNotFound(err)
	}
	if e.IsDefault {
		return errors.New(inventoryenums.ErrWarehouseIsDefault)
	}
	// 库存行随仓删除（外键级联）—— 有货被删掉就是静默丢账，必须先清货 / 调拨。
	n, cerr := s.m.CountNonZeroStocks(ctx, e.ID)
	if cerr != nil {
		return cerr
	}
	if n > 0 {
		return errors.New(inventoryenums.ErrWarehouseHasStock)
	}
	return s.m.DeleteWarehouse(ctx, e.ID)
}

// resolveWarehouse 解析归属仓：显式指定优先（必须同工程且启用），为空则兜底默认仓。
//
// 这是「未指定仓库 → 默认仓」的唯一解析入口：商品模块建变体时经端口调用它拿短码
// 参与 SKU 编码，库存记录也落在同一个仓上，两处口径不会漂移。
func (s *Service) resolveWarehouse(ctx context.Context, projectID, warehouseID string) (e *inventorymodel.WarehouseEntity, err error) {
	id := strings.TrimSpace(warehouseID)
	if id != "" {
		e, err = s.m.GetWarehouse(ctx, id)
		if err != nil {
			return nil, mapWarehouseNotFound(err)
		}
		if pid := strings.TrimSpace(projectID); pid != "" && e.ProjectID != pid {
			return nil, errors.New(inventoryenums.ErrWarehouseProjectMismatch)
		}
		if e.Status != inventoryenums.StatusActive {
			return nil, errors.New(inventoryenums.ErrWarehouseDisabled)
		}
		return e, nil
	}
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	e, err = s.m.GetDefaultWarehouse(ctx, pid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(inventoryenums.ErrWarehouseDefaultMissing)
		}
		return nil, err
	}
	if e.Status != inventoryenums.StatusActive {
		return nil, errors.New(inventoryenums.ErrWarehouseDisabled)
	}
	return e, nil
}

// normalizeCode 归一并校验仓库短码：大写字母数字，1..maxWarehouseCodeLen。
//
// 短码是 SKU 编码的前缀（{短码}_{商品码}_{序号}），下划线是分隔符，
// 因此短码本身不允许出现分隔符 —— 否则 SKU 编码无法反解出仓库。
func normalizeCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrWarehouseCodeRequired)
	}
	if len(out) > maxWarehouseCodeLen {
		return "", errors.New(inventoryenums.ErrWarehouseCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return "", errors.New(inventoryenums.ErrWarehouseCodeInvalid)
	}
	return out, nil
}

// normalizeStatus 归一仓库状态：空串取默认启用，其余必须是内置取值。
func normalizeStatus(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusActive:
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusDisabled:
		return inventoryenums.StatusDisabled, nil
	default:
		return "", errors.New(inventoryenums.ErrWarehouseStatusInvalid)
	}
}

// mapWarehouseNotFound 行不存在 → 业务错误，其余原样透出。
func mapWarehouseNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrWarehouseNotFound)
	}
	return err
}

// toWarehouseResp 实体 → 响应。
func toWarehouseResp(e *inventorymodel.WarehouseEntity) *inventorydto.WarehouseResp {
	if e == nil {
		return nil
	}
	return &inventorydto.WarehouseResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code, Name: e.Name,
		Status: e.Status, IsDefault: e.IsDefault, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}
