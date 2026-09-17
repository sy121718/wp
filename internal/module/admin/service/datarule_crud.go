package adminservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	// domain 必须已注册（pkg/datarule 注册中心），且配置只能引用该域声明过的字段与操作符；
	// 白名单不在这里把关的话，规则会「落库成功但运行时被引擎静默丢弃」。
	validated, err := validateRuleConfig(req.Domain, req.Config.ToRuleConfig())
	if err != nil {
		return err
	}
	config, err := encodeRuleConfig(validated)
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
	// 先查规则存在性（「规则不存在」语义优先），再校验 domain 与配置。
	validated, err := validateRuleConfig(req.Domain, req.Config.ToRuleConfig())
	if err != nil {
		return err
	}
	config, err := encodeRuleConfig(validated)
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

// validateRuleConfig 校验规则配置只能引用该数据域白名单内的字段与该字段声明的操作符，
// 并返回可直接落库的配置（字段名、逻辑、操作符都按声明口径原样保留）。
//
// 为什么这层必须有：白名单的消费者只有引擎的「字段名合法性」与操作符白名单 ——
// 前者只过滤字符集（escapeField），后者只查全局 supportedOps。字段名写错、字段存在但
// 用错操作符、OmitFields 写了本表没有的列，引擎全都静默跳过或静默无效：规则看起来生效，
// 实际什么都没拦。所以引用完整性在这里按域声明逐项判定，不合法直接拒绝落库。
//
// 调用方（表单路径 adminConfigFromJSON）不经过 gin 的 binding 校验，因此各分支在这里兜底重判。
func validateRuleConfig(domain string, cfg datarule.RuleConfig) (datarule.RuleConfig, error) {
	declared, ok := datarule.GetDomain(domain)
	if !ok {
		return datarule.RuleConfig{}, errors.New(adminenums.ErrInvalidDomain)
	}

	allowed := make(map[string]datarule.FieldDef, len(declared.WhiteList))
	for _, field := range declared.WhiteList {
		allowed[field.Field] = field
	}

	omitFields := make([]string, 0, len(cfg.OmitFields))
	for _, field := range cfg.OmitFields {
		if _, ok := allowed[field]; !ok {
			return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleFieldNotAllowed, field)
		}
		omitFields = append(omitFields, field)
	}

	groups := make([]datarule.ConditionGroup, 0, len(cfg.ConditionGroups))
	for _, group := range cfg.ConditionGroups {
		// 引擎对未知 Logic 会静默降级为 AND（buildConditions），这里不接受「差不多」的写法。
		// 取值口径与 dto 的 binding 声明一致（只认大写），表单路径与 JSON API 行为才不会有微妙差异。
		logic := strings.TrimSpace(group.Logic)
		if logic != datarule.LogicAnd && logic != datarule.LogicOr {
			return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleLogicNotAllowed, group.Logic)
		}

		conditions := make([]datarule.Condition, 0, len(group.Conditions))
		for _, condition := range group.Conditions {
			field, ok := allowed[condition.Field]
			if !ok {
				return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleFieldNotAllowed, condition.Field)
			}
			// 同上：只认大写，且必须是该字段声明过的操作符（不是「引擎全局支持」就算数）。
			op := strings.TrimSpace(condition.Op)
			if strings.ToUpper(op) != op || !containsOperator(field.Operators, op) {
				return datarule.RuleConfig{}, fmt.Errorf("%s: %s.%s", adminenums.ErrRuleOpNotAllowed, field.Field, condition.Op)
			}
			conditions = append(conditions, datarule.Condition{
				Field: field.Field,
				Op:    op,
				Value: condition.Value,
			})
		}
		groups = append(groups, datarule.ConditionGroup{Logic: logic, Conditions: conditions})
	}

	return datarule.RuleConfig{OmitFields: omitFields, ConditionGroups: groups}, nil
}

// containsOperator 报告字段声明的操作符里是否包含 op（两侧都已是大写）。
func containsOperator(operators []string, op string) bool {
	for _, item := range operators {
		if strings.EqualFold(strings.TrimSpace(item), op) {
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

func decodeRuleConfig(config string) (admindto.RuleConfigDTO, error) {
	var result admindto.RuleConfigDTO
	if err := json.Unmarshal([]byte(config), &result); err != nil {
		return admindto.RuleConfigDTO{}, err
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
