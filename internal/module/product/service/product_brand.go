// product_brand.go — 商品品牌（issue #10）。
//
// 品牌是独立实体（不是商品的字段）：名称、slug、logo、描述与 SEO 字段都定义一次，
// 商品侧只存 brand_id 引用（081 已建外键）。同一品牌可被多个商品共用。
//
// 边界：品牌不生成 URL、不写产物；静态化在发布管线里。
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

// CreateBrand 新建品牌。
func (s *Service) CreateBrand(ctx context.Context, req *productdto.CreateBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(productenums.ErrBrandNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = deriveSlug(req.Name)
	}
	if slug == "" {
		slug = "b-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, serr := s.m.BrandSlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrBrandSlugTaken)
	}
	now := time.Now().UTC()
	e := &productmodel.ProductBrandEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Slug: slug,
		Logo: req.Logo, Description: req.Description,
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Sort: req.Sort, Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateBrand(ctx, e); err != nil {
		return nil, err
	}
	return toBrandResp(e), nil
}

// UpdateBrand 修改品牌（含改名 / 换 slug / 换 logo / 描述与 SEO 字段）。
func (s *Service) UpdateBrand(ctx context.Context, req *productdto.UpdateBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetBrand(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(productenums.ErrBrandNameRequired)
		}
		e.Name = name
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.BrandSlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrBrandSlugTaken)
		}
		e.Slug = slug
	}
	if req.Logo != nil {
		e.Logo = *req.Logo
	}
	if req.Description != nil {
		e.Description = *req.Description
	}
	if req.SEOTitle != nil {
		e.SEOTitle = *req.SEOTitle
	}
	if req.SEODescription != nil {
		e.SEODescription = *req.SEODescription
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateBrand(ctx, e); err != nil {
		return nil, err
	}
	return toBrandResp(e), nil
}

// GetBrand 品牌详情。
func (s *Service) GetBrand(ctx context.Context, req *productdto.GetBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetBrand(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return toBrandResp(e), nil
}

// ListBrands 品牌列表（按排序号 + 创建时间的稳定顺序）。
func (s *Service) ListBrands(ctx context.Context, req *productdto.ListBrandReq) (list []*productdto.BrandResp, err error) {
	var projectID, keyword string
	if req != nil {
		projectID, keyword = req.ProjectID, strings.TrimSpace(req.Keyword)
	}
	rows, err := s.m.ListBrands(ctx, projectID, keyword)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.BrandResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toBrandResp(r))
	}
	return list, nil
}

// DeleteBrand 删除品牌。被商品引用时拒绝：静默解绑会让商品详情页的品牌区凭空消失，
// 而调用方（后台/接口）看不到任何信号。
func (s *Service) DeleteBrand(ctx context.Context, req *productdto.DeleteBrandReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	if _, gerr := s.m.GetBrand(ctx, req.ID, projectID); gerr != nil {
		return mapNotFound(gerr)
	}
	// 引用检查必须**跨工程**（审计 DB-03 §1.2 / §5.1 第 2 条）：跨工程引用此前不可见，
	// 删除放行后 products.brand_id 的外键 ON DELETE SET NULL 会**静默解绑**别的工程那个商品的
	// 品牌 —— 没有任何信号（这正是品牌守卫与另外三个不同的后果）。
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByBrand(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if ref.Referenced() {
		return crossProjectRefBlocked(productenums.ErrBrandInUse, ref)
	}
	return s.m.DeleteBrand(ctx, req.ID)
}

// resolveBrandID 校验商品指定的品牌并归一为指针（空串 = 不指定品牌 = nil）。
//
// 两条规则：品牌必须存在、必须与商品同工程（否则等于把别人的品牌挂到本商品上）。
func (s *Service) resolveBrandID(ctx context.Context, projectID, brandID string) (out *string, err error) {
	brandID = strings.TrimSpace(brandID)
	if brandID == "" {
		return nil, nil
	}
	row, err := s.m.GetBrand(ctx, brandID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(productenums.ErrBrandNotFound)
		}
		return nil, err
	}
	if projectID != "" && row.ProjectID != projectID {
		return nil, errors.New(productenums.ErrBrandProjectMismatch)
	}
	return &row.ID, nil
}

// toBrandResp 实体 → 响应。
func toBrandResp(e *productmodel.ProductBrandEntity) *productdto.BrandResp {
	return &productdto.BrandResp{
		ID: e.ID, ProjectID: e.ProjectID,
		Name: e.Name, Slug: e.Slug, Logo: e.Logo, Description: e.Description,
		SEOTitle: e.SEOTitle, SEODescription: e.SEODescription, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
