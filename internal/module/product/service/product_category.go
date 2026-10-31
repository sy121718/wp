package productservice

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
)

const maxCategoryDepth = 64

func (s *Service) CreateCategory(ctx context.Context, req *productdto.CreateCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, productenums.Err(productenums.ErrCategoryNameRequired)
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
		slug = "c-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, serr := s.m.CategorySlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, productenums.Err(productenums.ErrCategorySlugTaken)
	}
	parentID, err := s.resolveCategoryParent(ctx, projectID, strings.TrimSpace(req.ParentID), "")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &productmodel.ProductCategoryEntity{
		ID: uuid.NewString(), ProjectID: projectID, ParentID: parentID,
		Name: strings.TrimSpace(req.Name), Slug: slug,
		Description: req.Description, Image: mediaURL(req.Image),
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Sort: req.Sort, Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		parentID, perr := scoped.resolveCategoryParent(ctx, projectID, strings.TrimSpace(req.ParentID), "")
		if perr != nil {
			return perr
		}
		e.ParentID = parentID
		if cerr := s.m.CreateCategoryTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeCategory, EntityID: e.ID})
	}); err != nil {
		return nil, err
	}

	s.syncCategoryArchive(ctx, e.ProjectID, e.ID, e.Slug)
	return toCategoryResp(e), nil
}

func (s *Service) syncCategoryArchive(ctx context.Context, projectID, categoryID, slug string) {
	if s.archiveEnsurer == nil {
		return
	}
	resp, err := s.archiveEnsurer.EnsureArchiveInstance(ctx, &presentationcontract.EnsureArchiveReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeCategory, EntityID: categoryID, Slug: slug,
	})
	if err != nil {

		logger.Scene("product").
			With("projectId", projectID).
			With("categoryId", categoryID).
			With("slug", slug).
			With("reason", err.Error()).
			Warn("分类归档页同步失败（分类已保存，归档页稍后可重试）")
		return
	}
	if resp != nil && resp.Skipped != "" {
		logger.Scene("product").
			With("projectId", projectID).
			With("categoryId", categoryID).
			With("reason", resp.Skipped).
			Debug("归档页跳过")
	}
}

func (s *Service) UpdateCategory(ctx context.Context, req *productdto.UpdateCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetCategory(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.ParentID != nil {
		if _, perr := s.resolveCategoryParent(ctx, projectID, strings.TrimSpace(*req.ParentID), req.ID); perr != nil {
			return nil, perr
		}
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		locked, gerr := scoped.m.GetCategory(ctx, req.ID, projectID)
		if gerr != nil {
			return mapNotFound(gerr)
		}
		if !strings.EqualFold(locked.ProjectID, projectID) {
			return productenums.Err(productenums.ErrNotFound)
		}
		e = locked
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return productenums.Err(productenums.ErrCategoryNameRequired)
			}
			e.Name = name
		}
		if req.Slug != nil {
			slug := normalizeSlug(*req.Slug)
			if slug == "" {
				return productenums.Err(productenums.ErrInvalidParam)
			}
			if taken, serr := scoped.m.CategorySlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
				return serr
			} else if taken {
				return productenums.Err(productenums.ErrCategorySlugTaken)
			}
			e.Slug = slug
		}
		if req.ParentID != nil {
			parentID, perr := scoped.resolveCategoryParent(ctx, e.ProjectID, strings.TrimSpace(*req.ParentID), e.ID)
			if perr != nil {
				return perr
			}
			e.ParentID = parentID
		}
		if req.Description != nil {
			e.Description = *req.Description
		}
		if req.Image != nil {
			e.Image = mediaURL(*req.Image)
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
		if uerr := s.m.UpdateCategoryTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		refIDs, rerr := s.m.ProductIDsByCategoryTx(ctx, tx, e.ProjectID, e.ID, maxRenameFanoutProducts+1)
		if rerr != nil {
			return rerr
		}
		return s.enqueueEntityRenameFanout(ctx, tx, e.ProjectID, productcontract.EntityTypeCategory, e.ID, refIDs)
	}); err != nil {
		return nil, err
	}

	s.syncCategoryArchive(ctx, e.ProjectID, e.ID, e.Slug)
	return toCategoryResp(e), nil
}

func (s *Service) GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetCategory(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return toCategoryResp(e), nil
}

func (s *Service) ListCategories(ctx context.Context, req *productdto.ListCategoryReq) (list []*productdto.CategoryResp, err error) {
	projectID, keyword := categoryFilter(req)
	rows, err := s.m.ListCategories(ctx, projectID, keyword)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(rows), nil
}

func (s *Service) ListCategoryPage(ctx context.Context, req *productdto.ListCategoryPageReq) (res *productdto.CategoryListResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(req.Keyword)
	paging := utils.NormalizePaging(req.Page, req.Size, 20, 100)
	if keyword == "" {
		rows, total, lerr := s.m.ListCategoryRootsPage(ctx, projectID, paging.Size, paging.Offset)
		if lerr != nil {
			return nil, lerr
		}
		return &productdto.CategoryListResp{Items: buildCategoryPageTree(rows), Total: total}, nil
	}
	rows, matchTotal, lerr := s.m.ListCategorySearchForest(ctx, projectID, keyword)
	if lerr != nil {
		return nil, lerr
	}
	roots := buildCategoryPageTree(rows)
	res = &productdto.CategoryListResp{Total: int64(len(roots)), MatchTotal: matchTotal}
	res.Items, _ = pagination.Slice(roots, paging.Page, paging.Size)
	return res, nil
}

func (s *Service) CountCategories(ctx context.Context, req *productdto.ListCategoryReq) (n int64, err error) {
	projectID, keyword := categoryFilter(req)
	return s.m.CountCategories(ctx, projectID, keyword)
}

func categoryFilter(req *productdto.ListCategoryReq) (projectID, keyword string) {
	if req == nil {
		return "", ""
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword)
}

func (s *Service) DeleteCategory(ctx context.Context, req *productdto.DeleteCategoryReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return err
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
			return err
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		row, err := scoped.m.GetCategory(ctx, req.ID, projectID)
		if err != nil {
			return mapNotFound(err)
		}
		if !strings.EqualFold(row.ProjectID, projectID) {
			return productenums.Err(productenums.ErrNotFound)
		}
		if n, err := scoped.m.CountCategoryChildren(ctx, req.ID, projectID); err != nil {
			return err
		} else if n > 0 {
			return productenums.Err(productenums.ErrCategoryHasChildren)
		}
		ref, err := scoped.m.ProductRefsByCategory(ctx, req.ID, scope)
		if err != nil {
			return err
		}
		if ref.Referenced() {
			return crossProjectRefBlocked(productenums.ErrCategoryInUse, ref)
		}

		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		if err := s.m.DeleteCategoryTx(ctx, tx, req.ID); err != nil {
			return err
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, invalidationTarget{EntityType: productcontract.EntityTypeCategory, EntityID: req.ID})
	})
}

func (s *Service) resolveCategoryParent(ctx context.Context, projectID, parentID, selfID string) (out *string, err error) {
	if parentID == "" {
		return nil, nil
	}
	parent, err := s.m.GetCategory(ctx, parentID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, productenums.Err(productenums.ErrCategoryNotFound)
		}
		return nil, err
	}
	if projectID != "" && parent.ProjectID != projectID {
		return nil, productenums.Err(productenums.ErrCategoryParentMismatch)
	}
	if selfID != "" {
		cur := parent
		for depth := 0; depth < maxCategoryDepth && cur != nil; depth++ {
			if cur.ID == selfID {
				return nil, productenums.Err(productenums.ErrCategoryCycle)
			}
			if cur.ParentID == nil || *cur.ParentID == "" {
				break
			}
			next, gerr := s.m.GetCategory(ctx, *cur.ParentID, projectID)
			if gerr != nil {
				if errors.Is(gerr, gorm.ErrRecordNotFound) {
					break
				}
				return nil, gerr
			}
			cur = next
		}
	}
	return &parent.ID, nil
}

func (s *Service) applyCategoryRefs(
	ctx context.Context,
	projectID string,
	requestedIDs []string,
	currentIDs []string,
	primary *string,
	currentPrimary *string,
) (ids []string, primaryID *string, err error) {
	ids = []string{}
	if requestedIDs == nil {
		for _, id := range currentIDs {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	} else {
		ids, err = s.validateCategoryIDs(ctx, projectID, requestedIDs)
		if err != nil {
			return nil, nil, err
		}
	}

	explicit := primary != nil
	value := ""
	switch {
	case explicit:
		value = strings.TrimSpace(*primary)
	case currentPrimary != nil:
		value = strings.TrimSpace(*currentPrimary)
	}
	if value != "" {
		row, gerr := s.m.GetCategory(ctx, value, projectID)
		if gerr != nil {
			if errors.Is(gerr, gorm.ErrRecordNotFound) {
				return nil, nil, productenums.Err(productenums.ErrCategoryNotFound)
			}
			return nil, nil, gerr
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, nil, productenums.Err(productenums.ErrCategoryProjectMismatch)
		}
		if !containsID(ids, value) {
			if explicit {
				ids = append(ids, value)
			} else {
				value = ""
			}
		}
	}
	if value == "" {
		return ids, nil, nil
	}
	return ids, &value, nil
}

func (s *Service) validateCategoryIDs(ctx context.Context, projectID string, in []string) (out []string, err error) {
	out = []string{}
	if len(in) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	dedup := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
	}
	if len(dedup) == 0 {
		return out, nil
	}
	rows, lerr := s.m.ListCategoriesByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductCategoryEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range dedup {
		row, ok := byID[id]
		if !ok {
			return nil, productenums.Err(productenums.ErrCategoryNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, productenums.Err(productenums.ErrCategoryProjectMismatch)
		}
		out = append(out, id)
	}
	return out, nil
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func buildCategoryPageTree(rows []*productmodel.CategoryPageRow) []*productdto.CategoryResp {
	slices.SortStableFunc(rows, func(a, b *productmodel.CategoryPageRow) int {
		return cmp.Or(
			cmp.Compare(a.Sort, b.Sort),
			a.CreatedAt.Compare(b.CreatedAt),
			cmp.Compare(a.ID, b.ID),
		)
	})
	byID := make(map[string]*productdto.CategoryResp, len(rows))
	for _, row := range rows {
		item := toCategoryResp(&row.ProductCategoryEntity)
		item.HasChildren, item.Matched = row.HasChildren, row.Matched
		byID[item.ID] = item
	}
	roots := make([]*productdto.CategoryResp, 0, len(rows))
	for _, row := range rows {
		node := byID[row.ID]
		if row.ParentID != nil && *row.ParentID != "" {
			if parent, ok := byID[*row.ParentID]; ok && parent != node {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	for _, root := range roots {
		setCategoryDepth(root, 0)
	}
	return roots
}

func buildCategoryTree(rows []*productmodel.ProductCategoryEntity) []*productdto.CategoryResp {
	byID := make(map[string]*productdto.CategoryResp, len(rows))
	for _, r := range rows {
		byID[r.ID] = toCategoryResp(r)
	}
	roots := make([]*productdto.CategoryResp, 0, len(rows))
	for _, r := range rows {
		node := byID[r.ID]
		if r.ParentID != nil && *r.ParentID != "" {
			if parent, ok := byID[*r.ParentID]; ok && parent != node {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	for _, root := range roots {
		setCategoryDepth(root, 0)
	}
	return roots
}

func setCategoryDepth(node *productdto.CategoryResp, depth int) {
	node.Depth = depth
	for _, child := range node.Children {
		setCategoryDepth(child, depth+1)
	}
}

func toCategoryResp(e *productmodel.ProductCategoryEntity) *productdto.CategoryResp {
	resp := &productdto.CategoryResp{
		ID: e.ID, ProjectID: e.ProjectID,
		Name: e.Name, Slug: e.Slug, Description: e.Description, Image: e.Image,
		SEOTitle: e.SEOTitle, SEODescription: e.SEODescription, Sort: e.Sort,
		CreatedAt: utils.NewJSONTime(e.CreatedAt),
		UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
	if e.ParentID != nil {
		resp.ParentID = *e.ParentID
	}
	return resp
}

func (s *Service) CreateBrand(ctx context.Context, req *productdto.CreateBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, productenums.Err(productenums.ErrBrandNameRequired)
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
		return nil, productenums.Err(productenums.ErrBrandSlugTaken)
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

func (s *Service) UpdateBrand(ctx context.Context, req *productdto.UpdateBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
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
			return nil, productenums.Err(productenums.ErrBrandNameRequired)
		}
		e.Name = name
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.BrandSlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, productenums.Err(productenums.ErrBrandSlugTaken)
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

func (s *Service) GetBrand(ctx context.Context, req *productdto.GetBrandReq) (res *productdto.BrandResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
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

func (s *Service) CountBrands(ctx context.Context, req *productdto.ListBrandReq) (n int64, err error) {
	projectID, keyword := brandFilter(req)
	return s.m.CountBrands(ctx, projectID, keyword)
}

func brandFilter(req *productdto.ListBrandReq) (projectID, keyword string) {
	if req == nil {
		return "", ""
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword)
}

func (s *Service) DeleteBrand(ctx context.Context, req *productdto.DeleteBrandReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	if _, gerr := s.m.GetBrand(ctx, req.ID, projectID); gerr != nil {
		return mapNotFound(gerr)
	}

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

func (s *Service) resolveBrandID(ctx context.Context, projectID, brandID string) (out *string, err error) {
	brandID = strings.TrimSpace(brandID)
	if brandID == "" {
		return nil, nil
	}
	row, err := s.m.GetBrand(ctx, brandID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, productenums.Err(productenums.ErrBrandNotFound)
		}
		return nil, err
	}
	if projectID != "" && row.ProjectID != projectID {
		return nil, productenums.Err(productenums.ErrBrandProjectMismatch)
	}
	return &row.ID, nil
}

func toBrandResp(e *productmodel.ProductBrandEntity) *productdto.BrandResp {
	return &productdto.BrandResp{
		ID: e.ID, ProjectID: e.ProjectID,
		Name: e.Name, Slug: e.Slug, Logo: mediaURL(e.Logo), Description: e.Description,
		SEOTitle: e.SEOTitle, SEODescription: e.SEODescription, Sort: e.Sort,
		CreatedAt: utils.NewJSONTime(e.CreatedAt),
		UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}
