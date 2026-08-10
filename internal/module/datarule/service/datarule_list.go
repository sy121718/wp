// Package dataruleservice datarule 模块业务逻辑实现。
package dataruleservice

import (
	"context"

	dataruledto "go_wp/internal/module/datarule/dto"
	"go_wp/pkg/datarule"
)

// List 规则分页查询。
func (s *Service) List(ctx context.Context, req *dataruledto.ListReq) (res *dataruledto.ListResp, err error) {
	// 构建基础查询，支持按 domain 和 status 筛选
	query := s.rm.DB(ctx)

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
	var items []dataruledto.RuleItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 保证返回空切片而非 nil
	if items == nil {
		items = []dataruledto.RuleItem{}
	}

	return &dataruledto.ListResp{Total: total, List: items}, nil
}

// SchemaList 返回所有已注册 domain。
// 数据来自 pkg/datarule 注册中心，非数据库。
func (s *Service) SchemaList(ctx context.Context) (res []dataruledto.DomainItem, err error) {
	// 从注册中心获取全部已注册 domain 列表
	domains := datarule.GetRegisteredDomains()
	res = make([]dataruledto.DomainItem, 0, len(domains))
	for _, d := range domains {
		res = append(res, dataruledto.DomainItem{
			Domain:      d.Domain,
			DomainLabel: d.DomainLabel,
			TableName:   d.TableName,
		})
	}
	return res, nil
}

// SchemaDetail 返回 domain 的字段白名单。
func (s *Service) SchemaDetail(ctx context.Context, req *dataruledto.SchemaDetailReq) (res *dataruledto.DomainDetail, err error) {
	// 遍历注册中心查找指定 domain
	domains := datarule.GetRegisteredDomains()
	for _, d := range domains {
		if d.Domain == req.Domain {
			// 将注册中心的字段白名单转换为 DTO
			fields := make([]dataruledto.FieldDef, 0, len(d.WhiteList))
			for _, f := range d.WhiteList {
				fields = append(fields, dataruledto.FieldDef{
					Field:     f.Field,
					Label:     f.Label,
					DataType:  f.DataType,
					Operators: f.Operators,
				})
			}
			return &dataruledto.DomainDetail{
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
