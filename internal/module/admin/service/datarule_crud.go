package adminservice

import (
	"context"
	"encoding/json"
	"errors"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

// RuleDetail 数据规则详情。
func (s *Service) RuleDetail(ctx context.Context, req *admindto.RuleDetailReq) (res *admindto.RuleDetailResp, err error) {
	// 根据 ID 查询规则实体
	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrRuleNotFound)
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

	return &admindto.RuleDetailResp{
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

// RuleCreate 新建数据规则。
func (s *Service) RuleCreate(ctx context.Context, req *admindto.RuleCreateReq) error {
	// domain 必须已注册（pkg/datarule 注册中心），否则规则落库后无引擎消费。
	if !isRegisteredDomain(req.Domain) {
		return errors.New(adminenums.ErrInvalidDomain)
	}

	config, err := encodeRuleConfig(req.Config)
	if err != nil {
		return err
	}

	entity := &adminmodel.SysRuleEntity{
		RuleName: req.RuleName,
		Domain:   req.Domain,
		Config:   config,
		Status:   req.Status,
	}
	// 可选备注
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	return s.drm.Create(ctx, entity)
}

// RuleUpdate 更新数据规则。
func (s *Service) RuleUpdate(ctx context.Context, req *admindto.RuleUpdateReq) error {
	// 校验规则是否存在
	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrRuleNotFound)
	}
	// domain 必须已注册；先查存在性（规则不存在语义优先），再校验 domain。
	if !isRegisteredDomain(req.Domain) {
		return errors.New(adminenums.ErrInvalidDomain)
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

	return s.drm.Update(ctx, entity)
}

// RuleDelete 批量删除数据规则。
func (s *Service) RuleDelete(ctx context.Context, req *admindto.RuleDeleteReq) error {
	// 先清理该规则的分配记录，避免外键残留
	for _, id := range req.IDs {
		if err := s.dram.DeleteByRuleID(ctx, id); err != nil {
			return err
		}
	}
	// 再批量删除规则本身
	_, err := s.drm.DeleteByIDs(ctx, req.IDs)
	return err
}

// isRegisteredDomain 校验 domain 是否已在 pkg/datarule 注册中心注册。
// 与 RuleSchemaList/RuleSchemaDetail 同一数据源（GetRegisteredDomains）。
func isRegisteredDomain(domain string) bool {
	for _, d := range datarule.GetRegisteredDomains() {
		if d.Domain == domain {
			return true
		}
	}
	return false
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

// RuleList 数据规则分页查询。
func (s *Service) RuleList(ctx context.Context, req *admindto.RuleListReq) (res *admindto.RuleListResp, err error) {
	// 构建基础查询，支持按 domain 和 status 筛选
	query := s.drm.DB(ctx)

	if req.Domain != "" {
		query = query.Where("domain = ?", req.Domain)
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	// 查询总记录数用于分页
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	// 按 id 降序分页查询
	var items []admindto.RuleItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 保证返回空切片而非 nil
	if items == nil {
		items = []admindto.RuleItem{}
	}

	return &admindto.RuleListResp{Total: total, List: items}, nil
}

// RuleSchemaList 返回所有已注册 domain。
// 数据来自 pkg/datarule 注册中心，非数据库。
func (s *Service) RuleSchemaList(ctx context.Context) (res []admindto.RuleDomainItem, err error) {
	// 从注册中心获取全部已注册 domain 列表
	domains := datarule.GetRegisteredDomains()
	res = make([]admindto.RuleDomainItem, 0, len(domains))
	for _, d := range domains {
		res = append(res, admindto.RuleDomainItem{
			Domain:      d.Domain,
			DomainLabel: d.DomainLabel,
			TableName:   d.TableName,
		})
	}
	return res, nil
}

// RuleSchemaDetail 返回 domain 的字段白名单。
func (s *Service) RuleSchemaDetail(ctx context.Context, req *admindto.RuleSchemaDetailReq) (res *admindto.RuleDomainDetail, err error) {
	// 遍历注册中心查找指定 domain
	domains := datarule.GetRegisteredDomains()
	for _, d := range domains {
		if d.Domain == req.Domain {
			// 将注册中心的字段白名单转换为 DTO
			fields := make([]admindto.RuleFieldDef, 0, len(d.WhiteList))
			for _, f := range d.WhiteList {
				fields = append(fields, admindto.RuleFieldDef{
					Field:     f.Field,
					Label:     f.Label,
					DataType:  f.DataType,
					Operators: f.Operators,
				})
			}
			return &admindto.RuleDomainDetail{
				Domain:      d.Domain,
				DomainLabel: d.DomainLabel,
				TableName:   d.TableName,
				Fields:      fields,
			}, nil
		}
	}

	// domain 未注册时返回空
	return nil, nil
}
