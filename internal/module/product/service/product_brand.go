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

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
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
		Logo: mediaURL(req.Logo), Description: req.Description,
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Sort: req.Sort, Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	// 品牌行与静态产物失效事件同事务（审计 ARCH-01，理由同分类侧）。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateBrandTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeBrand, EntityID: e.ID})
	}); err != nil {
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
		e.Logo = mediaURL(*req.Logo)
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
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateBrandTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 改名类写入口（审计 ARCH-01 收口票）：逐引用商品发 direct_content（理由同分类）。
		refIDs, rerr := s.m.ProductIDsByBrandTx(ctx, tx, e.ProjectID, e.ID, maxRenameFanoutProducts+1)
		if rerr != nil {
			return rerr
		}
		return s.enqueueEntityRenameFanout(ctx, tx, e.ProjectID, productcontract.EntityTypeBrand, e.ID, refIDs)
	}); err != nil {
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
//
// 分页下推到 model 的 LIMIT/OFFSET（审计 D12 收口）：请求类型自带的 Page/Size 只在
// **显式给出**时生效，零值形态仍是全量（见 optionalPaging）—— 集合源筛选选项与内容翻译
// 候选都走那条形态，它们要的是全部品牌。
func (s *Service) ListBrands(ctx context.Context, req *productdto.ListBrandReq) (list []*productdto.BrandResp, err error) {
	projectID, keyword := brandFilter(req)
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	limit, offset := optionalPaging(inPage, inSize)
	rows, err := s.m.ListBrands(ctx, projectID, keyword, limit, offset)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.BrandResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toBrandResp(r))
	}
	return list, nil
}

// CountBrands 品牌总数（后台品牌页的「共 N 条」与总页数）。
//
// **与 ListBrands 共用同一个 brandFilter**（工程 + 关键词归一），两处口径不存在分叉余地。
func (s *Service) CountBrands(ctx context.Context, req *productdto.ListBrandReq) (n int64, err error) {
	projectID, keyword := brandFilter(req)
	return s.m.CountBrands(ctx, projectID, keyword)
}

// brandFilter 归一品牌列表的过滤条件（ListBrands / CountBrands 共用）。
func brandFilter(req *productdto.ListBrandReq) (projectID, keyword string) {
	if req == nil {
		return "", ""
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword)
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
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteBrandTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeBrand, EntityID: req.ID})
	})
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
		Name: e.Name, Slug: e.Slug, Logo: mediaURL(e.Logo), Description: e.Description,
		SEOTitle: e.SEOTitle, SEODescription: e.SEODescription, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
