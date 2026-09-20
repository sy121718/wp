// product_tag.go — 商品标签：手工标签与自动标签（issue #11）。
//
// 语义（本票四条验收的落点）：
//  1. 手工标签（kind=manual）可建、可手工挂到商品（products.tag_ids，写入前校验
//     同工程 + 必须存在 + 必须是手工标签）；
//  2. 自动标签（kind=rule）只接受内置规则类型与参数（见 product_tag_rule.go），
//     规则类型未知、参数带未知键 / 类型不符 / 越界一律拒绝，**不接受自由表达式**；
//  3. 自动标签归属的**重算时机**是明确的四条，别处不重算：
//     · 商品写操作后（建商品 / 改商品 / 改状态到 published）；
//     · 变体写操作后（价格与对比价参与规则判定：单个新增 / 修改 / 删除 / 组合生成）；
//     · 标签自身定义变更后（新建即算一次；改规则类型或参数后立刻按新规则重算）；
//     · 显式调用（接口 / 后台「重算」按钮，可按标签或按工程）。
//     重算只替换**自己那一个 tag id** 的商品归属（model.ReplaceTagProductsTx 按元素
//     摘挂），因此永远不会覆盖手工标签，也不会动其它自动标签；
//  4. 后台可查看某标签命中哪些商品（GetTag / ListTagProducts 接口 + 页面按需片段：
//     首屏只给数量，展开某个标签才按页取，见 ListTagProductsPage）。
//
// 边界：标签只描述「商品有什么标记」，不生成 URL、不写产物；静态化在发布管线里。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
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

const (
	// defaultTagProducts 后台「某标签命中哪些商品」的默认展示条数（接口形态）。
	defaultTagProducts = 100
	// maxTagProducts 该列表的条数上限（防止一个标签挂了几千商品把页面拖死）。
	maxTagProducts = 500
	// tagProductsPageSize 后台展开区「命中商品」每页条数（审计 PERF-02）。
	// 比接口默认值小：展开区是页面里的一小块，一页 50 行已经要滚动才能看完。
	tagProductsPageSize = 50
	// maxTagProductsPageSize 每页条数上限（调用方给得再大也只给这么多）。
	maxTagProductsPageSize = 200
)

// CreateTag 新建标签（默认手工类型；规则型建好即按规则重算一次）。
func (s *Service) CreateTag(ctx context.Context, req *productdto.CreateTagReq) (res *productdto.TagResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(productenums.ErrTagNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	kind, ruleType, ruleParams, err := normalizeTagShapeOnCreate(req.Kind, req.RuleType, req.RuleParams)
	if err != nil {
		return nil, err
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = deriveSlug(req.Name)
	}
	if slug == "" {
		slug = "t-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, serr := s.m.TagSlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrTagSlugTaken)
	}
	now := time.Now().UTC()
	e := &productmodel.ProductTagEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Slug: slug,
		Kind: kind, RuleType: ruleType, RuleParams: ruleParams,
		Sort: req.Sort, Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	// 规则命中集合**先在事务外求值**：规则求值内部走 rls.InProjectScope（自带事务、另取连接），
	// 塞进已开的事务里既看不到未提交数据又可能自锁（见 pkg/rls.ScopeTx 的说明）。
	var hitIDs []string
	if e.Kind == productenums.TagKindRule {
		if hitIDs, err = s.ruleHitProductIDs(ctx, projectID, e.RuleType, e.RuleParams); err != nil {
			return nil, err
		}
	}
	// 标签行与它的首次归属重算**同事务**：规则型标签建好即算一次（避免「刚建出来一个都没命中」
	// 的假象），而「有标签、没归属」或反过来的半截状态只能靠人工对账发现（AGENTS.md「写操作的事务与回滚」）。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateTagTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		// 静态产物失效（审计 ARCH-01）：标签定义与归属变化会改列表页 / 详情页的字节。
		if xerr := s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: e.ID}); xerr != nil {
			return xerr
		}
		if e.Kind != productenums.TagKindRule {
			return nil
		}
		return s.recalcTagTx(ctx, tx, e, hitIDs, time.Now().UTC())
	}); err != nil {
		return nil, err
	}
	return s.tagDetail(ctx, e)
}

// UpdateTag 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
//
// 类型切换语义见 productdto.UpdateTagReq 注释：manual → rule 必须给出规则定义并立即重算；
// rule → manual 清掉规则定义、保留当前命中为手工归属。
func (s *Service) UpdateTag(ctx context.Context, req *productdto.UpdateTagReq) (res *productdto.TagResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetTag(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapTagNotFound(err)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(productenums.ErrTagNameRequired)
		}
		e.Name = name
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.TagSlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrTagSlugTaken)
		}
		e.Slug = slug
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	if req.Kind != nil || req.RuleType != nil || req.RuleParams != nil {
		if err = applyTagShapeUpdate(e, req); err != nil {
			return nil, err
		}
	}
	e.UpdatedAt = time.Now().UTC()
	// 命中集合先在事务外求值（同 CreateTag：规则求值自带事务，不能嵌进已开的事务）。
	var hitIDs []string
	if e.Kind == productenums.TagKindRule {
		if hitIDs, err = s.ruleHitProductIDs(ctx, e.ProjectID, e.RuleType, e.RuleParams); err != nil {
			return nil, err
		}
	}
	// 标签定义变更与按新规则重算**同事务**：分开提交时「改了规则、归属还是旧的」会让
	// 后台的命中列表与新规则不一致，而这正是运营用来核对规则写对没有的那一屏。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateTagTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		// 静态产物失效（审计 ARCH-01）。
		if xerr := s.enqueueInvalidationTx(ctx, tx, e.ProjectID,
			invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: e.ID}); xerr != nil {
			return xerr
		}
		// 重算时机之一：规则定义变更后立刻按新规则重算（手工标签不动任何归属）。
		if e.Kind != productenums.TagKindRule {
			return nil
		}
		return s.recalcTagTx(ctx, tx, e, hitIDs, time.Now().UTC())
	}); err != nil {
		return nil, err
	}
	return s.tagDetail(ctx, e)
}

// applyTagShapeUpdate 计算类型切换后的 kind / ruleType / ruleParams（就地写入实体）。
func applyTagShapeUpdate(e *productmodel.ProductTagEntity, req *productdto.UpdateTagReq) (err error) {
	kind := e.Kind
	if req.Kind != nil {
		kind = strings.TrimSpace(*req.Kind)
	}
	switch kind {
	case productenums.TagKindManual:
		// rule → manual（或本来就是 manual 又提交了一次）：规则定义整体清掉。
		// 当前命中结果保留下来作为手工归属 —— 自动维护到此为止，后续由人工调整。
		e.Kind, e.RuleType, e.RuleParams, e.RecalcAt = productenums.TagKindManual, "", json.RawMessage("{}"), nil
		return nil
	case productenums.TagKindRule:
		ruleType := e.RuleType
		if req.RuleType != nil {
			ruleType = strings.TrimSpace(*req.RuleType)
		}
		params := e.RuleParams
		if req.RuleParams != nil {
			params = req.RuleParams
		}
		norm, nerr := normalizeTagRuleParams(ruleType, params)
		if nerr != nil {
			return nerr
		}
		e.Kind, e.RuleType, e.RuleParams = productenums.TagKindRule, ruleType, norm
		return nil
	default:
		return errors.New(productenums.ErrTagKindInvalid)
	}
}

// normalizeTagShapeOnCreate 新建时的形状校验（手工标签不允许带规则定义）。
func normalizeTagShapeOnCreate(kind, ruleType string, params json.RawMessage) (outKind, outRuleType string, outParams json.RawMessage, err error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		// 缺省按手工标签：最保守的选择，不会因为一条规则自动改动商品归属。
		kind = productenums.TagKindManual
	}
	switch kind {
	case productenums.TagKindManual:
		if strings.TrimSpace(ruleType) != "" || hasRuleParams(params) {
			return "", "", nil, errors.New(productenums.ErrTagRuleNotAllowed)
		}
		return kind, "", json.RawMessage("{}"), nil
	case productenums.TagKindRule:
		norm, nerr := normalizeTagRuleParams(ruleType, params)
		if nerr != nil {
			return "", "", nil, nerr
		}
		return kind, strings.TrimSpace(ruleType), norm, nil
	default:
		return "", "", nil, errors.New(productenums.ErrTagKindInvalid)
	}
}

// hasRuleParams 参数是否给出了实质内容（空对象 / null / 空串都算没给）。
func hasRuleParams(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "{}" && s != "null"
}

// GetTag 标签详情（含命中商品列表 —— 验收 4）。
func (s *Service) GetTag(ctx context.Context, req *productdto.GetTagReq) (res *productdto.TagResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetTag(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapTagNotFound(err)
	}
	return s.tagDetail(ctx, e)
}

// ListTags 标签列表（工程内；kind 为空表示手工 + 自动都返回）。
//
// 每个标签带当前归属数量：列表页要能一眼看出自动规则命中了多少商品。
//
// 命中数是**一次批量聚合**出来的（审计 PERF-02）：此前逐个标签发一条 Count，
// 页面 SQL 条数随标签数线性增长 —— 「标签是个位数」只是当时的假设，协议没有使它成立。
// 现在无论 1 个还是 1000 个标签，这里都只多一条 SQL。
func (s *Service) ListTags(ctx context.Context, req *productdto.ListTagReq) (list []*productdto.TagResp, err error) {
	var projectID, kind, keyword string
	rawProjectID := ""
	if req != nil {
		rawProjectID, kind, keyword = req.ProjectID, strings.TrimSpace(req.Kind), strings.TrimSpace(req.Keyword)
	}
	// 工程作用域必填（DB-009）：下面的批量计数反查的是 products，
	// 没有作用域时每个标签的命中数会静默变成 0（fail closed 不报错）。
	// 这与 Products.List 同一口径：不显式指定工程时取唯一工程，多于一个工程即报错，
	// 不再有「projectID 为空 = 不限工程」这条在策略下必然退化成空结果的旧语义。
	projectID, err = s.resolveProjectID(ctx, rawProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListTags(ctx, projectID, kind, keyword)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	counts, cerr := s.m.CountProductsByTagIDs(ctx, projectID, ids)
	if cerr != nil {
		return nil, cerr
	}
	list = make([]*productdto.TagResp, 0, len(rows))
	for _, r := range rows {
		resp := toTagResp(r)
		// 未命中的标签不在聚合结果里，缺省 0（不是「没查到」）。
		resp.ProductCount = int(counts[r.ID])
		list = append(list, resp)
	}
	return list, nil
}

// ListTagProducts 某标签命中的商品（验收 4：后台可查看某标签命中哪些商品）。
func (s *Service) ListTagProducts(ctx context.Context, req *productdto.ListTagProductsReq) (list []*productdto.TagProductResp, err error) {
	if req == nil || req.TagID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if _, gerr := s.m.GetTag(ctx, req.TagID, projectID); gerr != nil {
		return nil, mapTagNotFound(gerr)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultTagProducts
	}
	if limit > maxTagProducts {
		limit = maxTagProducts
	}
	rows, err := s.m.ListProductsByTag(ctx, req.TagID, projectID, limit)
	if err != nil {
		return nil, err
	}
	return toTagProductResps(rows), nil
}

// ListTagProductsPage 某标签命中商品的一页（后台展开区按需取；审计 PERF-02）。
//
// 与 ListTagProducts 的分工：那个是接口形态（不翻页、只要一个上限），这个是后台页形态。
// 命中商品从首屏移到这里之后，标签页的 SQL 条数与标签数彻底脱钩：展开一个标签才会
// 发这一组查询（标签存在性 + 总数 + 本页行），与页面上有多少标签无关。
//
// 越界的 page 不报错：收敛到最后一页（Total 仍是真实值），不会把一个翻页越界
// 变成一条错误提示 —— 翻页本来就是可以走到头再点一下的操作。
func (s *Service) ListTagProductsPage(ctx context.Context, req *productdto.ListTagProductsPageReq) (res *productdto.TagProductsPageResp, err error) {
	if req == nil || req.TagID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tag, gerr := s.m.GetTag(ctx, req.TagID, projectID)
	if gerr != nil {
		return nil, mapTagNotFound(gerr)
	}
	size := req.Size
	if size <= 0 {
		size = tagProductsPageSize
	}
	if size > maxTagProductsPageSize {
		size = maxTagProductsPageSize
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	total, cerr := s.m.CountProductsByTag(ctx, tag.ID, projectID)
	if cerr != nil {
		return nil, cerr
	}
	// 偏移量按「最长一页也不越界」算：page 很大时 offset 会溢出 int，先按页数封顶。
	totalPage := pagesOf(int(total), size)
	if page > totalPage {
		page = totalPage
	}
	rows, lerr := s.m.ListProductsByTagPage(ctx, tag.ID, projectID, (page-1)*size, size)
	if lerr != nil {
		return nil, lerr
	}
	return &productdto.TagProductsPageResp{
		TagID: tag.ID, TagName: tag.Name,
		Total: int(total), Page: page, PageSize: size, TotalPage: totalPage,
		Items: toTagProductResps(rows),
	}, nil
}

// pagesOf 总页数（空列表也算 1 页：页面要显示「第 1 / 1 页」，而不是「第 1 / 0 页」）。
func pagesOf(total, size int) int {
	if size <= 0 || total <= 0 {
		return 1
	}
	return (total + size - 1) / size
}

// DeleteTag 删除标签：先把引用从商品上解绑，再删标签行（同一事务）。
//
// 不拒绝「**本工程**已被商品引用」的删除：标签是标记不是外键，解绑即可；反过来说，
// 留下悬空引用才是坏数据，所以两步必须原子。
//
// 但**别的工程**的引用一律打回给人（审计 DB-03 §2.4 记的正是这个缺口）：
// RemoveTagFromProductsTx 的作用域是 tag.ProjectID，只解绑标签自己工程下的商品 ——
// 别的工程仍引用着它时，删除会成功并在那些商品上留下永久悬空 tag id。
// 这里不跨工程解绑：跨工程写既会被策略的 WITH CHECK 拒绝（换非超级角色后），
// 也不该由本工程的删除动作替别的工程改数据（AGENTS.md「一律打回给人」）。
func (s *Service) DeleteTag(ctx context.Context, req *productdto.DeleteTagReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	tag, gerr := s.m.GetTag(ctx, req.ID, projectID)
	if gerr != nil {
		return mapTagNotFound(gerr)
	}
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByTag(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if others := ref.Outside(tag.ProjectID); others.Referenced() {
		return crossProjectRefBlocked(productenums.ErrTagCrossProject, others)
	}
	now := time.Now().UTC()
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if rerr := s.m.RemoveTagFromProductsTx(tx, tag.ID, tag.ProjectID, now); rerr != nil {
			return rerr
		}
		if xerr := s.enqueueInvalidationTx(ctx, tx, tag.ProjectID,
			invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: tag.ID}); xerr != nil {
			return xerr
		}
		return s.m.DeleteTagTx(tx, tag.ID)
	})
}

// ListTagRuleTypes 内置规则类型清单（后台规则下拉与参数说明的唯一来源）。
func (s *Service) ListTagRuleTypes(ctx context.Context) (list []*productdto.TagRuleTypeResp) {
	return tagRuleTypeOptions()
}

// RecalcTags 手动触发重算（重算时机之一）。
//
// TagID 为空时重算该工程全部自动标签；TagID 非空时只重算它（工程按标签自己的取）。
// 手工标签不参与重算（命中数为 0，也不计进 Recalculated）。
func (s *Service) RecalcTags(ctx context.Context, req *productdto.RecalcTagsReq) (res *productdto.RecalcTagsResp, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	res = &productdto.RecalcTagsResp{Tags: []*productdto.TagResp{}}
	projectID := strings.TrimSpace(req.ProjectID)
	var tags []*productmodel.ProductTagEntity
	if id := strings.TrimSpace(req.TagID); id != "" {
		// 作用域：显式 projectId 优先，否则取标签自身的工程（这里先按请求工程读标签本身）。
		scopeID, serr := s.resolveProjectID(ctx, projectID)
		if serr != nil {
			return nil, serr
		}
		tag, gerr := s.m.GetTag(ctx, id, scopeID)
		if gerr != nil {
			return nil, mapTagNotFound(gerr)
		}
		tags = []*productmodel.ProductTagEntity{tag}
		if projectID == "" {
			projectID = tag.ProjectID
		}
	} else {
		if projectID == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		tags, err = s.m.ListTags(ctx, projectID, productenums.TagKindRule, "")
		if err != nil {
			return nil, err
		}
	}
	for _, tag := range tags {
		n, rerr := s.recalcTag(ctx, tag)
		if rerr != nil {
			return nil, rerr
		}
		if tag.Kind != productenums.TagKindRule {
			continue
		}
		res.Recalculated++
		res.Products += n
	}
	after, lerr := s.ListTags(ctx, &productdto.ListTagReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	res.Tags = after
	return res, nil
}

// resolveTagIDs 校验并归一商品引用的标签 id（去重 + 保序 + 同工程 + 必须存在 + 必须是手工标签）。
//
// 「必须是手工标签」是刻意的：自动标签的归属由重算维护，允许手工挂载会立刻被下一次
// 重算抹掉，调用方却以为挂上了 —— 直接拒绝比静默丢失诚实。
func (s *Service) resolveTagIDs(ctx context.Context, projectID string, in []string) (out []string, err error) {
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
	rows, lerr := s.m.ListTagsByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductTagEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range dedup {
		row, ok := byID[id]
		if !ok {
			return nil, errors.New(productenums.ErrTagNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, errors.New(productenums.ErrTagProjectMismatch)
		}
		if row.Kind != productenums.TagKindManual {
			return nil, errors.New(productenums.ErrTagNotManual)
		}
		out = append(out, id)
	}
	return out, nil
}

// recalcTag 按规则重算单个标签的商品归属（手工标签直接返回 0，一个字都不动）。
//
// 命中集合的求值（读）在事务外完成：规则求值是多表只读查询，且内部走 rls.InProjectScope
// （自带事务），放进外层事务只会拉长持锁时间并可能自锁。「替换归属 + 记重算时间」两处写
// 落在同一个事务里（recalcTagTx）。
func (s *Service) recalcTag(ctx context.Context, tag *productmodel.ProductTagEntity) (n int, err error) {
	if tag == nil || tag.Kind != productenums.TagKindRule {
		return 0, nil
	}
	ids, err := s.ruleHitProductIDs(ctx, tag.ProjectID, tag.RuleType, tag.RuleParams)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.recalcTagTx(ctx, tx, tag, ids, now)
	}); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// recalcTagTx 在调用方事务里「整体替换该标签的商品归属 + 记重算时间」——两处写必须原子。
//
// 原先这两步分属两个事务：归属已经替换、UpdateTag 失败时 recalc_at 还是上一次的值，
// 后台按它核对「重算时机」会得出错误结论（看起来最近没重算过，实际归属已经变了）。
func (s *Service) recalcTagTx(ctx context.Context, tx *gorm.DB, tag *productmodel.ProductTagEntity,
	ids []string, now time.Time) (err error) {
	if serr := rls.ScopeTx(tx, tag.ProjectID); serr != nil {
		return serr
	}
	if rerr := s.m.ReplaceTagProductsTx(tx, tag.ID, tag.ProjectID, ids, now); rerr != nil {
		return rerr
	}
	// 静态产物失效（审计 ARCH-01）：自动标签的归属重算改了商品的 tag_ids，
	// 列表页的项目标签与详情页的标签区随之变化 → 发该标签的实体键 + 商品集合键。
	if xerr := s.enqueueInvalidationTx(ctx, tx, tag.ProjectID,
		invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: tag.ID}); xerr != nil {
		return xerr
	}
	// 记录重算时间（后台可见，用来核对「重算时机」是否真的发生过）。
	tag.RecalcAt = &now
	tag.UpdatedAt = now
	return s.m.UpdateTagTx(ctx, tx, tag)
}

// recalcAutoTags 重算某工程下的全部自动标签（商品 / 变体写操作后的隐式重算入口）。
func (s *Service) recalcAutoTags(ctx context.Context, projectID string) (err error) {
	if projectID == "" {
		return nil
	}
	tags, err := s.m.ListTags(ctx, projectID, productenums.TagKindRule, "")
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err = s.recalcTag(ctx, tag); err != nil {
			return err
		}
	}
	return nil
}

// recalcProjectAutoTags 按商品 id 反查工程后重算（商品 / 变体写操作后的统一收口）。
//
// 商品已不存在（例如刚被删除）时直接返回：归属随商品行一起消失，没有需要重算的东西。
// projectID 是工程作用域（DB-009）：products 有 RLS 策略，这次反查同样要在作用域内。
// 调用方没有工程上下文时（纯商品单测路径）按唯一工程兜底 —— 唯一的兜底入口，
// 不静默跳过作用域（那正是换角色后「静默 0 行」的来源）。
func (s *Service) recalcProjectAutoTags(ctx context.Context, productID, projectID string) (err error) {
	if productID == "" {
		return nil
	}
	if strings.TrimSpace(projectID) == "" {
		if projectID, err = s.resolveProjectID(ctx, ""); err != nil {
			return err
		}
	}
	p, gerr := s.m.Get(ctx, productID, projectID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil
		}
		return gerr
	}
	return s.recalcAutoTags(ctx, p.ProjectID)
}

// ruleHitProductIDs 单条规则的命中商品 id（已过滤到本工程、顺序确定）。
func (s *Service) ruleHitProductIDs(ctx context.Context, projectID, ruleType string, params json.RawMessage) (ids []string, err error) {
	rule := lookupTagRule(ruleType)
	if rule == nil {
		return nil, ruleTypeErr(ruleType)
	}
	ids, err = rule.Evaluate(ctx, s.m, projectID, params)
	if err != nil {
		return nil, err
	}
	return s.filterProjectProductIDs(ctx, projectID, ids)
}

// filterProjectProductIDs 只保留本工程的商品 id 并排序。
//
// 变体维度的规则（价格区间 / 促销）取数时没有工程条件（product_variants 没有 project_id），
// 这里统一兜底过滤 —— 归属绝不能落到别的工程。排序保证同一份数据每次写出同样的顺序。
func (s *Service) filterProjectProductIDs(ctx context.Context, projectID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, lerr := s.m.ListProductsByIDs(ctx, ids, projectID)
	if lerr != nil {
		return nil, lerr
	}
	for _, p := range rows {
		if p == nil {
			continue
		}
		if projectID != "" && p.ProjectID != projectID {
			continue
		}
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return out, nil
}

// tagDetail 实体 → 详情响应（含命中商品列表）。
//
// ProductCount 取**真实命中数**，不是 len(Products)：Products 有 maxTagProducts 上限
// 截断，此前两者混用会让「命中 800 个」的标签在详情里显示 500（与列表接口的数字对不上）。
func (s *Service) tagDetail(ctx context.Context, e *productmodel.ProductTagEntity) (res *productdto.TagResp, err error) {
	// 工程作用域取标签自身的工程：tagDetail 的入参就是标签实体（ProjectID 非空列），
	// 不必再由调用方多传一个参数。
	rows, lerr := s.m.ListProductsByTag(ctx, e.ID, e.ProjectID, maxTagProducts)
	if lerr != nil {
		return nil, lerr
	}
	total, cerr := s.m.CountProductsByTag(ctx, e.ID, e.ProjectID)
	if cerr != nil {
		return nil, cerr
	}
	resp := toTagResp(e)
	resp.Products = toTagProductResps(rows)
	resp.ProductCount = int(total)
	return resp, nil
}

// toTagResp 实体 → 响应（规则描述由服务端翻好，后台不解释规则参数）。
func toTagResp(e *productmodel.ProductTagEntity) *productdto.TagResp {
	resp := &productdto.TagResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Slug: e.Slug,
		Kind: e.Kind, RuleType: e.RuleType,
		RuleParams: orJSON(e.RuleParams, "{}"),
		RuleLabel:  describeTagRule(e.RuleType, e.RuleParams),
		Sort:       e.Sort,
		CreatedAt:  e.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  e.UpdatedAt.Format(time.RFC3339),
	}
	if e.RecalcAt != nil {
		resp.RecalcAt = e.RecalcAt.Format(time.RFC3339)
	}
	return resp
}

// toTagProductResps 商品行 → 标签命中商品响应。
func toTagProductResps(rows []*productmodel.ProductEntity) []*productdto.TagProductResp {
	out := make([]*productdto.TagProductResp, 0, len(rows))
	for _, p := range rows {
		if p == nil {
			continue
		}
		out = append(out, &productdto.TagProductResp{
			ID: p.ID, Name: p.Name, Slug: p.Slug, Status: p.Status,
		})
	}
	return out
}

// mapTagNotFound 把 gorm 的 not found 归一为标签不存在。
func mapTagNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(productenums.ErrTagNotFound)
	}
	return err
}
