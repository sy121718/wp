// product_category.go — 商品分类（issue #10）。
//
// 分类是树形自引用实体：父子层级 + 工程内唯一 slug + 排序 + SEO 字段。
// 本文件同时落地「商品 → 分类」的引用规则：
//
//	· 附属分类可挂多个（products.category_ids）；
//	· 主分类唯一（products.primary_category_id），且必然是附属分类之一 ——
//	  接口显式指定主分类而它不在附属列表里时自动纳入，未指定而已不在列表里时解绑，
//	  不变量始终由服务端维持，调用方不需要自己拼；
//	· 引用必须在同一工程内且真实存在（JSONB 数组没有数据库级外键兜底）。
//
// 边界：分类只描述「商品属于哪里」，不生成 URL、不写产物；静态化在发布管线里。
package productservice

import (
	"context"
	"errors"
	"sort"
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
	"go_wp/pkg/rls"
)

// maxCategoryDepth 分类层级兜底上限：正常数据不会到这个深度，
// 存在的意义是「历史脏数据里已有环」时不必靠递归撞栈。
const maxCategoryDepth = 64

// CreateCategory 新建分类（ParentID 为空即顶级）。
func (s *Service) CreateCategory(ctx context.Context, req *productdto.CreateCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(productenums.ErrCategoryNameRequired)
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
		return nil, errors.New(productenums.ErrCategorySlugTaken)
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
	// 分类行与静态产物失效事件同事务（审计 ARCH-01）：分类改名 / 增删会改归档页与
	// 商品列表页的字节（列表项内嵌分类展示名），事件必须与这次写一起生效或一起回滚。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateCategoryTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeCategory, EntityID: e.ID})
	}); err != nil {
		return nil, err
	}
	// 归档页同步（审计 EDT-004）：新建分类 → 补建它的归档页。
	s.syncCategoryArchive(ctx, e.ProjectID, e.ID, e.Slug)
	return toCategoryResp(e), nil
}

// syncCategoryArchive 让归档页跟上分类变化（审计 EDT-004）。
//
// 失败只记日志，**不阻断分类保存**：归档页是派生视图，不是分类的一部分。
// 反过来（因为归档页建不出来而不让保存分类）会让一个次要问题挡住主流程，
// 而且分类已经写进库里了，此时返回错误反而让调用方以为没保存成功。
//
// 未配置归档模板时 presentation 返回 Skipped —— 这里是正常路径，不打错误日志。
//
// 实体类型必须是**注册表口径** product_category（审计 EDT-004 收口）：presentation 按
// 实体类型取归档模板（contenttemplate 的注册表校验 + 迁移 160 已把存量行改名），
// 写成短名 "category" 时 ResolveTemplateByRoleScoped 直接判类型非法 ——
// 归档实例必然建不出来，而这一条只会在下面那行日志里露头。
func (s *Service) syncCategoryArchive(ctx context.Context, projectID, categoryID, slug string) {
	if s.archiveEnsurer == nil {
		return
	}
	resp, err := s.archiveEnsurer.EnsureArchiveInstance(ctx, &presentationcontract.EnsureArchiveReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeCategory, EntityID: categoryID, Slug: slug,
	})
	if err != nil {
		// 派生视图失败不阻断保存，但**必须能被人发现**：带上工程 / 分类 / slug 与原因，
		// 否则现场只剩一句没有定位信息的日志，谁也答不出「哪个分类的归档页没建起来、为什么」。
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

// UpdateCategory 修改分类（含改名 / 换父级 / 排序 / SEO 字段）。
//
// 换父级走 resolveCategoryParent：跨工程、挂到自身或自己的后代下一律拒绝
// （否则树上会出现自环或环，列表接口再也列不出这些节点）。
func (s *Service) UpdateCategory(ctx context.Context, req *productdto.UpdateCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetCategory(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(productenums.ErrCategoryNameRequired)
		}
		e.Name = name
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.CategorySlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrCategorySlugTaken)
		}
		e.Slug = slug
	}
	if req.ParentID != nil {
		parentID, perr := s.resolveCategoryParent(ctx, e.ProjectID, strings.TrimSpace(*req.ParentID), e.ID)
		if perr != nil {
			return nil, perr
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
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateCategoryTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 改名类写入口（审计 ARCH-01 收口票）：除实体键外**逐引用商品**发 direct_content ——
		// 商品详情页登记的是 product:{id}，只发分类键命中不到它（改分类名后页面仍是旧名）。
		refIDs, rerr := s.m.ProductIDsByCategoryTx(ctx, tx, e.ProjectID, e.ID, maxRenameFanoutProducts+1)
		if rerr != nil {
			return rerr
		}
		return s.enqueueEntityRenameFanout(ctx, tx, e.ProjectID, productcontract.EntityTypeCategory, e.ID, refIDs)
	}); err != nil {
		return nil, err
	}
	// 改名 / 换 slug 后归档页路径要跟着走（审计 EDT-004）：同步入口内部会比对路径，
	// 不同则按新路径重建并给旧路径留 301。
	s.syncCategoryArchive(ctx, e.ProjectID, e.ID, e.Slug)
	return toCategoryResp(e), nil
}

// GetCategory 分类详情。
func (s *Service) GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (res *productdto.CategoryResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
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

// ListCategories 分类列表 —— 返回**树**（顶级在数组里，子级挂在 Children）。
//
// 排序在 SQL 层已定（sort ASC, create_time ASC, id ASC），这里只做父子挂接，
// 因此同一份数据每次输出同样的顺序（构建期确定性同一条理由）。
// 父级不在结果集里（被删/跨工程/环数据）的节点按顶级处理，保证节点不丢。
func (s *Service) ListCategories(ctx context.Context, req *productdto.ListCategoryReq) (list []*productdto.CategoryResp, err error) {
	projectID, keyword := categoryFilter(req)
	rows, err := s.m.ListCategories(ctx, projectID, keyword)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(rows), nil
}

// ListCategoryPage 后台分类树的分页读，返回**树**（顶级分类带 Children 嵌套）。
//
// 分页单位一律是树根：浏览态 = 顶级分类，搜索态 = 命中所属的根分类。
// 逐层点进去的导航（parentId）与子级懒加载已退役 —— 列表本身就是整棵树，
// 逐层导航与树并存只会让人在两套「在哪一层」的心智模型之间来回切。
// 旧的 ListCategories（全树契约）留给构建期与其它调用方，本方法管后台分页。
func (s *Service) ListCategoryPage(ctx context.Context, req *productdto.ListCategoryPageReq) (res *productdto.CategoryPageResp, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(req.Keyword)
	page, size := req.Page, req.Size
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	if keyword == "" {
		rows, total, lerr := s.m.ListCategoryRootsPage(ctx, projectID, size, (page-1)*size)
		if lerr != nil {
			return nil, lerr
		}
		return &productdto.CategoryPageResp{Items: buildCategoryPageTree(rows), Total: total}, nil
	}
	rows, matchTotal, lerr := s.m.ListCategorySearchForest(ctx, projectID, keyword)
	if lerr != nil {
		return nil, lerr
	}
	roots := buildCategoryPageTree(rows)
	res = &productdto.CategoryPageResp{Total: int64(len(roots)), MatchTotal: matchTotal}
	start := (page - 1) * size
	if start >= len(roots) {
		// 页码越界：分页条由调用方按 Total 夹住，这里只保证不切出 panic。
		return res, nil
	}
	end := start + size
	if end > len(roots) {
		end = len(roots)
	}
	res.Items = roots[start:end]
	return res, nil
}

// CountCategories 分类总数（后台分类页的「共 N 条」与总页数）。
//
// 数是**行**：后台把树按 DFS 前序摊平成表格行再分页，「共 N 条」说的就是这些行。
//
// **与 ListCategories 共用同一个 categoryFilter**（工程 + 关键词归一）——两处口径分叉时，
// 分页条给的页数会与实际能翻出来的行数对不上。
func (s *Service) CountCategories(ctx context.Context, req *productdto.ListCategoryReq) (n int64, err error) {
	projectID, keyword := categoryFilter(req)
	return s.m.CountCategories(ctx, projectID, keyword)
}

// categoryFilter 归一分类列表的过滤条件（ListCategories / CountCategories 共用）。
func categoryFilter(req *productdto.ListCategoryReq) (projectID, keyword string) {
	if req == nil {
		return "", ""
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword)
}

// DeleteCategory 删除分类。
//
// 两条前置规则（都是「删了会留下坏数据」的场景）：
//  1. 仍有子级 → 拒绝（外键是 SET NULL，会把子级静默提升为顶级，层级信息就丢了）；
//  2. 仍被商品引用（附属或主分类）→ 拒绝，调用方需先解绑。
func (s *Service) DeleteCategory(ctx context.Context, req *productdto.DeleteCategoryReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	if _, gerr := s.m.GetCategory(ctx, req.ID, projectID); gerr != nil {
		return mapNotFound(gerr)
	}
	if n, cerr := s.m.CountCategoryChildren(ctx, req.ID, projectID); cerr != nil {
		return cerr
	} else if n > 0 {
		return errors.New(productenums.ErrCategoryHasChildren)
	}
	// 引用检查必须**跨工程**（审计 DB-03 §1.2 / §5.1 第 2 条）：此前的 ProductUsingCategory 把
	// 作用域收在本工程，别的工程仍引用这条分类时命中 0 行 ⇒ 删除放行 ⇒ products.category_ids
	// 里留下永久悬空 id。扫描按工程逐个设置作用域取并集，与连接角色无关（见 model 的文件头）。
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByCategory(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if ref.Referenced() {
		// 命中即拒绝，明细里给出引用面 / 工程 / 商品，由人决定处置（不自动清理）。
		return crossProjectRefBlocked(productenums.ErrCategoryInUse, ref)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteCategoryTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeCategory, EntityID: req.ID})
	})
}

// resolveCategoryParent 校验父分类并归一为指针（空串 = 顶级 = nil）。
//
// 三条规则：父级必须存在、必须同工程、selfID 不能出现在父级到根的链上（判环）。
func (s *Service) resolveCategoryParent(ctx context.Context, projectID, parentID, selfID string) (out *string, err error) {
	if parentID == "" {
		return nil, nil
	}
	parent, err := s.m.GetCategory(ctx, parentID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(productenums.ErrCategoryNotFound)
		}
		return nil, err
	}
	if projectID != "" && parent.ProjectID != projectID {
		return nil, errors.New(productenums.ErrCategoryParentMismatch)
	}
	if selfID != "" {
		cur := parent
		for depth := 0; depth < maxCategoryDepth && cur != nil; depth++ {
			if cur.ID == selfID {
				return nil, errors.New(productenums.ErrCategoryCycle)
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

// applyCategoryRefs 计算商品的分类引用与主分类，维持「主分类必属于附属分类」不变量。
//
// 参数语义（update 路径的「不改」与「清空」必须能区分）：
//   - requestedIDs 为 nil → 本次不改附属分类，沿用 currentIDs（不重复校验，它们写入时已校验过）；
//   - requestedIDs 为空数组 → 解绑全部分类；
//   - primary 为 nil → 本次不改主分类；指向空串 → 显式解绑主分类。
//
// 自动纳入 / 自动解绑两条兜底规则：
//   - 显式指定主分类但它不在附属列表里 → 自动纳入（主分类必然是附属分类之一）；
//   - 未显式指定，而沿用/替换后的附属列表里已经没有原主分类 → 解绑主分类。
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
				return nil, nil, errors.New(productenums.ErrCategoryNotFound)
			}
			return nil, nil, gerr
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, nil, errors.New(productenums.ErrCategoryProjectMismatch)
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

// validateCategoryIDs 校验并归一商品引用的附属分类 id（去重 + 保序 + 同工程 + 必须存在）。
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
			return nil, errors.New(productenums.ErrCategoryNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, errors.New(productenums.ErrCategoryProjectMismatch)
		}
		out = append(out, id)
	}
	return out, nil
}

// containsID id 是否在切片里（小切片线性查找即可，避免为一次判断建 map）。
func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// buildCategoryPageTree 分页读的行 → 树（并填 Depth / HasChildren / Matched）。
//
// 排序在这里做而不是在 SQL 里：递归 CTE 的输出顺序在 UNION ALL 之后没有语义，
// 同一层的兄弟必须按 sort / create_time / id 定序才对得上用户在表单里看到的顺序。
func buildCategoryPageTree(rows []*productmodel.CategoryPageRow) []*productdto.CategoryResp {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Sort != b.Sort {
			return a.Sort < b.Sort
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
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

// buildCategoryTree 扁平行 → 树（父级缺失 / 自环 / 环数据一律按顶级处理，节点不丢）。
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

// setCategoryDepth 由根向下填层级（后台据此缩进；前端不需要第二套父子规则）。
func setCategoryDepth(node *productdto.CategoryResp, depth int) {
	node.Depth = depth
	for _, child := range node.Children {
		setCategoryDepth(child, depth+1)
	}
}

// toCategoryResp 实体 → 响应（ParentID 归一为空串，前端不必判 null）。
func toCategoryResp(e *productmodel.ProductCategoryEntity) *productdto.CategoryResp {
	resp := &productdto.CategoryResp{
		ID: e.ID, ProjectID: e.ProjectID,
		Name: e.Name, Slug: e.Slug, Description: e.Description, Image: e.Image,
		SEOTitle: e.SEOTitle, SEODescription: e.SEODescription, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ParentID != nil {
		resp.ParentID = *e.ParentID
	}
	return resp
}
