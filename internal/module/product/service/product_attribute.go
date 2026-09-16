// product_attribute.go — 商品属性组与属性值（issue #7）。
//
// 本文件落地本票四条验收：
//  1. 可建属性组与属性值，各带稳定标识与排序 —— 组有 Key，值有 ID + Key，
//     排序由 Sort 字段 + 数组顺序双保险（见 product_attribute_values.go）；
//  2. 属性组可标记是否参与变体 —— IsVariation 落到 product_attributes.is_variation，
//     由 #8 的笛卡尔积消费；
//  3. 同一属性组可被多个商品复用 —— 组与值的定义只存一份（product_attributes），
//     商品侧只存引用（products.attribute_ids），不复制定义；
//  4. 后台可管理属性组与属性值 —— 走 /api/product/attribute/* 与 /admin/product-attributes。
//
// 边界：属性只描述「这个商品有什么可选的维度」，不生成任何变体、不改价格库存。
// 变体笛卡尔积是 #8 的事；这里只保证数据形态可用。
package productservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// CreateAttribute 新建属性组（可选同时带初始属性值）。
//
// 属性组归属工程（product_attributes.project_id 是 NOT NULL 外键）；
// 未显式给工程时与商品一样取唯一工程兜底。
func (s *Service) CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(productenums.ErrAttrNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	key := normalizeAttrToken(req.Key)
	if key == "" {
		key = normalizeAttrToken(req.Name)
	}
	if key == "" {
		key = "attr-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, kerr := s.m.AttributeKeyExists(ctx, projectID, key, ""); kerr != nil {
		return nil, kerr
	} else if taken {
		return nil, errors.New(productenums.ErrAttrKeyTaken)
	}

	now := time.Now().UTC()
	isVariation := true
	if req.IsVariation != nil {
		isVariation = *req.IsVariation
	}
	values := sanitizeAttributeValues(req.Values)
	e := &productmodel.ProductAttributeEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Key: key, Name: strings.TrimSpace(req.Name),
		IsVariation: isVariation, Sort: req.Sort,
		Values:    encodeAttributeValues(values),
		Metadata:  []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateAttribute(ctx, e); err != nil {
		return nil, err
	}
	return toAttributeResp(e), nil
}

// UpdateAttribute 修改属性组本身（名称 / 标识 / 参与变体标记 / 排序）。
//
// 属性值不在这里改：走 SetAttributeValues 整体保存，避免「改组顺手清空值」。
// Key 允许改（属性组尚未被变体引用时确实存在改名需求），但工程内仍须唯一。
func (s *Service) UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetAttribute(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.Key != nil {
		key := normalizeAttrToken(*req.Key)
		if key == "" {
			return nil, errors.New(productenums.ErrAttrKeyRequired)
		}
		if taken, kerr := s.m.AttributeKeyExists(ctx, e.ProjectID, key, e.ID); kerr != nil {
			return nil, kerr
		} else if taken {
			return nil, errors.New(productenums.ErrAttrKeyTaken)
		}
		e.Key = key
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(productenums.ErrAttrNameRequired)
		}
		e.Name = name
	}
	if req.IsVariation != nil {
		e.IsVariation = *req.IsVariation
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateAttribute(ctx, e); err != nil {
		return nil, err
	}
	return s.toAttributeDetail(ctx, e)
}

// SetAttributeValues 整体保存属性组的值（全量替换语义，见 dto 注释）。
func (s *Service) SetAttributeValues(ctx context.Context, req *productdto.SetAttributeValuesReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetAttribute(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	values := sanitizeAttributeValues(req.Values)
	e.Values = encodeAttributeValues(values)
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateAttribute(ctx, e); err != nil {
		return nil, err
	}
	return s.toAttributeDetail(ctx, e)
}

// GetAttribute 属性组详情。
func (s *Service) GetAttribute(ctx context.Context, req *productdto.GetAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetAttribute(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return s.toAttributeDetail(ctx, e)
}

// ListAttributes 属性组列表。
//
// 已带出每个组的属性值：后台一张表要同时显示「组 + 值」，分两次查会让
// 每个组再多一次往返（N+1）。值本身就在本表 JSONB 列里，读出来零成本。
func (s *Service) ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) (list []*productdto.AttributeResp, err error) {
	page, size := attributePageArgs(req)
	var variation *bool
	if req != nil {
		switch strings.TrimSpace(req.Variation) {
		case "1":
			v := true
			variation = &v
		case "0":
			v := false
			variation = &v
		}
	}
	var projectID, keyword string
	if req != nil {
		projectID, keyword = req.ProjectID, strings.TrimSpace(req.Keyword)
	}
	rows, err := s.m.ListAttributes(ctx, projectID, keyword, variation, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.AttributeResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toAttributeResp(r))
	}
	return list, nil
}

// DeleteAttribute 删除属性组。
//
// 被商品引用时拒绝删除：products.attribute_ids 是 JSON 数组，没有数据库级外键
// 兜底，删掉就会留下悬空引用（商品详情页读到不存在的组）。调用方需先解绑。
func (s *Service) DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	if _, gerr := s.m.GetAttribute(ctx, req.ID, projectID); gerr != nil {
		return mapNotFound(gerr)
	}
	if _, uerr := s.m.ProductUsingAttribute(ctx, req.ID); uerr == nil {
		return errors.New(productenums.ErrAttrInUse)
	} else if !errors.Is(uerr, gorm.ErrRecordNotFound) {
		return uerr
	}
	return s.m.DeleteAttribute(ctx, req.ID)
}

// attributePageArgs 归一化属性组分页参数。
func attributePageArgs(req *productdto.ListAttributeReq) (page, size int) {
	page, size = 1, defaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > maxPageSize {
			size = maxPageSize
		}
	}
	return page, size
}

// toAttributeResp 实体 → 响应（值列表在这里归一，历史字符串数组也能读出来）。
func toAttributeResp(e *productmodel.ProductAttributeEntity) *productdto.AttributeResp {
	values := normalizeValuesFromRaw(e.Values)
	resp := &productdto.AttributeResp{
		ID: e.ID, ProjectID: e.ProjectID, Key: e.Key, Name: e.Name,
		IsVariation: e.IsVariation, Sort: e.Sort,
		Values: values, ValueCount: len(values),
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	for _, v := range values {
		if v.Enabled {
			resp.VariationValueCount++
		}
	}
	return resp
}

// toAttributeDetail 与 toAttributeResp 同形；详情路径保留单独入口，
// 便于后续在详情里追加聚合信息（如已引用它的商品数）而不影响列表。
func (s *Service) toAttributeDetail(ctx context.Context, e *productmodel.ProductAttributeEntity) (*productdto.AttributeResp, error) {
	_ = ctx
	return toAttributeResp(e), nil
}

// resolveAttributeIDs 校验并归一商品引用的属性组 id。
//
// 两条规则（issue #7 验收 3）：
//  1. 引用必须在同一工程内（跨工程引用等于把别人的属性组挂到本商品上）；
//  2. 引用必须真实存在（不校验的话接口会静默写入悬空 id）。
//
// 去重后保持调用方给的顺序，保证「同一次提交产生同一份 attribute_ids」。
func (s *Service) resolveAttributeIDs(ctx context.Context, projectID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	dedup := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
	}
	rows, lerr := s.m.ListAttributesByIDs(ctx, dedup)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range dedup {
		row, ok := byID[id]
		if !ok {
			return nil, errors.New(productenums.ErrAttrNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, errors.New(productenums.ErrAttrProjectMismatch)
		}
		out = append(out, id)
	}
	return out, nil
}
