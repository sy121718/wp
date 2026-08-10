// Package dataruleservice datarule 模块业务逻辑实现。
package dataruleservice

import (
	"context"
	"encoding/json"
	"errors"

	dataruledto "go_wp/internal/module/datarule/dto"
	dataruleenums "go_wp/internal/module/datarule/enums"
	datamodel "go_wp/internal/module/datarule/model"
	"go_wp/pkg/datarule"
)

// Detail 规则详情。
func (s *Service) Detail(ctx context.Context, req *dataruledto.DetailReq) (res *dataruledto.DetailResp, err error) {
	// 根据 ID 查询规则实体
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(dataruleenums.ErrRuleNotFound)
	}

	// 处理可选字段和格式化时间
	remark := ""
	if entity.Remark != nil {
		remark = *entity.Remark
	}
	createTime := ""
	if entity.CreateTime != nil {
		createTime = entity.CreateTime.Format("2006-01-02 15:04:05")
	}
	updateTime := ""
	if entity.UpdateTime != nil {
		updateTime = entity.UpdateTime.Format("2006-01-02 15:04:05")
	}

	config, err := decodeRuleConfig(entity.Config)
	if err != nil {
		return nil, err
	}

	return &dataruledto.DetailResp{
		ID:         entity.ID,
		RuleName:   entity.RuleName,
		Domain:     entity.Domain,
		Config:     config,
		Status:     entity.Status,
		Remark:     remark,
		CreateBy:   entity.CreateBy,
		CreateTime: createTime,
		UpdateBy:   entity.UpdateBy,
		UpdateTime: updateTime,
	}, nil
}

// Create 新建规则。
func (s *Service) Create(ctx context.Context, req *dataruledto.CreateReq) error {
	config, err := encodeRuleConfig(req.Config)
	if err != nil {
		return err
	}

	entity := &datamodel.SysRuleEntity{
		RuleName: req.RuleName,
		Domain:   req.Domain,
		Config:   config,
		Status:   req.Status,
	}
	// 可选备注
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	return s.rm.Create(ctx, entity)
}

// Update 更新规则。
func (s *Service) Update(ctx context.Context, req *dataruledto.UpdateReq) error {
	// 校验规则是否存在
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(dataruleenums.ErrRuleNotFound)
	}

	config, err := encodeRuleConfig(req.Config)
	if err != nil {
		return err
	}

	// 更新字段
	entity.RuleName = req.RuleName
	entity.Domain = req.Domain
	entity.Config = config
	entity.Status = req.Status
	// 若 remark 为空则置 nil，否则更新
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	return s.rm.Update(ctx, entity)
}

// Delete 批量删除规则。
func (s *Service) Delete(ctx context.Context, req *dataruledto.DeleteReq) error {
	// 先清理该规则的分配记录，避免外键残留
	for _, id := range req.IDs {
		if err := s.ram.DeleteByRuleID(ctx, id); err != nil {
			return err
		}
	}
	// 再批量删除规则本身
	_, err := s.rm.DeleteByIDs(ctx, req.IDs)
	return err
}

func encodeRuleConfig(config datarule.RuleConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRuleConfig(config string) (datarule.RuleConfig, error) {
	var result datarule.RuleConfig
	if err := json.Unmarshal([]byte(config), &result); err != nil {
		return datarule.RuleConfig{}, err
	}
	return result, nil
}
