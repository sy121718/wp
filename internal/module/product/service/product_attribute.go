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

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
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
	// 属性行与静态产物失效事件同事务（审计 ARCH-01 尾巴）：属性组是商品详情页
	// 规格维度（product.options）与列表页筛选维度（集合元数据）的来源，两处字节都会变。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateAttributeTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.enqueueAttributeInvalidation(ctx, tx, projectID, e.ID)
	}); err != nil {
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
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateAttributeTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 定义变更（组名）：属性组名进 products.options（规格维度），逐引用商品发 direct_content。
		return s.enqueueAttributeDefinitionChange(ctx, tx, e.ProjectID, e.ID)
	}); err != nil {
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
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateAttributeTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 定义变更（属性值）：值标签同样进 products.options，逐引用商品发 direct_content ——
		// 与改组名同一条路径（两者都是「引用该属性组的商品页字节变了」）。
		return s.enqueueAttributeDefinitionChange(ctx, tx, e.ProjectID, e.ID)
	}); err != nil {
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
	projectID, keyword, variation := attributeFilter(req)
	page, size := attributePageArgs(req)
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

// CountAttributes 属性组总数（后台属性页的「共 N 条」与总页数）。
//
// **与 ListAttributes 共用同一个 attributeFilter**：Variation 的三态取值（"" / "1" / "0"）
// 与关键词归一各抄一遍时，抄错的那一侧不报错 —— 只表现为总数与列表条数静默对不上，
// 而且只在用了那个筛选维度时才看得出来。
func (s *Service) CountAttributes(ctx context.Context, req *productdto.ListAttributeReq) (n int64, err error) {
	projectID, keyword, variation := attributeFilter(req)
	return s.m.CountAttributes(ctx, projectID, keyword, variation)
}

// attributeFilter 归一属性组列表的过滤条件（ListAttributes / CountAttributes 共用）。
func attributeFilter(req *productdto.ListAttributeReq) (projectID, keyword string, variation *bool) {
	if req == nil {
		return "", "", nil
	}
	// 三态字符串 → *bool：只有明确的 "1" / "0" 才过滤，其余（含 ""）不过滤。
	switch strings.TrimSpace(req.Variation) {
	case "1":
		v := true
		variation = &v
	case "0":
		v := false
		variation = &v
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword), variation
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
	// 引用检查必须**跨工程**（审计 DB-03 §1.2 / §5.1 第 2 条）：attribute_ids 是 JSON 数组、
	// 没有任何数据库级外键，守卫看不见别的工程的引用时删除会留下永久悬空 id。
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByAttribute(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if ref.Referenced() {
		return crossProjectRefBlocked(productenums.ErrAttrInUse, ref)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteAttributeTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		return s.enqueueAttributeInvalidation(ctx, tx, projectID, req.ID)
	})
}

// attributeInvalidationEntityKey 属性组实体键的失效目标（两条路径共用）。
func attributeInvalidationEntityKey(attributeID string) invalidationTarget {
	return invalidationTarget{EntityType: productcontract.EntityTypeAttribute, EntityID: attributeID}
}

// enqueueAttributeInvalidation **只有实体键**的失效入队（审计 ARCH-01 尾巴）。
//
// 用在「建 / 删属性组」上 —— 这两个动作没有「引用它的商品」这一面需要逐个发：
// 新建时还没有商品引用；删除被引用检查挡住（跨工程检查见 crossProjectRefs）。
// 改定义（组名 / 值）走 enqueueAttributeDefinitionChange，那里才需要逐引用商品发键。
//
// 与分类 / 品牌同形：实体键 + 商品集合键（enqueueInvalidationTx 内部按批补集合键）。
func (s *Service) enqueueAttributeInvalidation(ctx context.Context, tx *gorm.DB, projectID, attributeID string) error {
	return s.enqueueInvalidationTx(ctx, tx, projectID, attributeInvalidationEntityKey(attributeID))
}

// enqueueAttributeDefinitionChange 属性**定义**变更（组名 / 值）的失效入队。
//
// 两条路径为什么必须分开（审计 ARCH-01 最后一批）：
//   - 实体键只命中「绑定该属性组本身」的产物；
//   - 而组名与值都进 products.options（规格维度），**引用该属性组的商品详情页**字节
//     同样会变 —— 详情页登记的是 direct_content:product:{id}，只发实体键命中不到它，
//     站点上表现为「改了规格名 / 规格值，商品页还是旧的」且没有任何日志。
//
// 引用商品的全量 id 由 ProductIDsByAttributeTx（只投影 id，不是删除守卫那份采样）在
// **同一事务内**取出；上限与截断日志在 enqueueEntityRenameFanout 里（宁可多、不可漏）。
func (s *Service) enqueueAttributeDefinitionChange(ctx context.Context, tx *gorm.DB, projectID, attributeID string) error {
	refIDs, err := s.m.ProductIDsByAttributeTx(ctx, tx, projectID, attributeID, maxRenameFanoutProducts+1)
	if err != nil {
		return err
	}
	return s.enqueueEntityRenameFanout(ctx, tx, projectID, productcontract.EntityTypeAttribute, attributeID, refIDs)
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
	rows, lerr := s.m.ListAttributesByIDs(ctx, dedup, projectID)
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
