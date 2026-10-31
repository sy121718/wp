package productservice

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/seo"
	json "go_wp/pkg/jsonx"
)

func (s *Service) SetPublicCatalogIndex(index presentationcontract.PublishedEntityIndex) {
	s.publicCatalogIndex = index
}

// 公开目录的分页边界（PRD-008）。
//
// 这三个数字是**今天的契约**，不是「顺手写死」：页码超过上限、每页超过上限都回
// 400（用户可以直接改 URL，越界不该由我们对数据库发起一次深 OFFSET）。
//
// 为什么抽成常量：它们出现在参数闸门里，而 DEC-05（公开 100 页上限、越界是空页还是
// 夹到末页）**仍待定** —— 待定期间最危险的不是数字本身，是「同一个 100 在两个地方
// 各写一遍、改口径时只改了其中一个」。口径一旦确定，改这里一处即可。
//
// 现状（未改口径，仅命名）：page 0 → 1（等于缺省），page > 100 → 400，page < 0 → 400；
// 已上线实体不足时按上限夹页，超出的页码返回空 items（不报错）。
const (
	publicCatalogMaxPage         = 100
	publicCatalogMaxPageSize     = 60
	publicCatalogDefaultPageSize = 12
)

// PublicCatalog 先约束真实上线实体，再用同一过滤条件取数与计数，隐藏项不占分页位置。
func (s *Service) PublicCatalog(ctx context.Context, req *productdto.PublicCatalogReq) (*productdto.PublicCatalogResp, error) {
	if req == nil || uuid.Validate(req.ProjectID) != nil || req.Page < 0 || req.Page > publicCatalogMaxPage || req.PageSize < 0 || req.PageSize > publicCatalogMaxPageSize {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	filter := map[string]string{}
	if req.Filter != "" {
		if err := json.Unmarshal([]byte(req.Filter), &filter); err != nil {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
	}
	if filter == nil {
		filter = map[string]string{}
	}
	filter[productcontract.CollectionFilterStatus] = productenums.StatusPublished
	f, err := parseCollectionFilter(filter)
	if err != nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	order := strings.TrimSpace(req.Sort)
	if !publicCatalogSort(order) {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	page, size := req.Page, req.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = publicCatalogDefaultPageSize
	}
	out := &productdto.PublicCatalogResp{Items: []productdto.PublicCatalogItemResp{}, Page: page, PageSize: size}
	if s.publicCatalogIndex == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	paths, err := s.publicCatalogIndex.PublishedEntityIndex(ctx, req.ProjectID, productcontract.EntityTypeProduct, req.Lang)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return out, nil
	}
	f.ProjectID, f.Order, f.Keyword = req.ProjectID, order, strings.TrimSpace(req.Query)
	f.IDs = make([]string, 0, len(paths))
	for id := range paths {
		if uuid.Validate(id) == nil {
			f.IDs = append(f.IDs, id)
		}
	}
	if len(f.IDs) == 0 {
		return out, nil
	}
	// 排序不是为了结果顺序（候选只当 = ANY(数组) 的过滤集合用，取数顺序由 ORDER BY 决定），
	// 而是为了让**同样的输入产生同样的绑定参数**：map 遍历顺序随机，不排序的话每次请求的
	// 数组字面量都不同，EXPLAIN 与预算对比就永远比不出差异（PRD-007/018 的测量前提）。
	sort.Strings(f.IDs)
	out.Total, err = s.m.CountForCollection(ctx, f)
	if err != nil {
		return nil, err
	}
	// 页内无候选就不必再发一条带深 OFFSET 的取数（越界页与空结果集都走这里）：
	// 计数已经把「这一页有没有东西」答完了 —— 省掉的正是深分页最贵的那一段。
	if out.Total == 0 || int64((page-1)*size) >= out.Total {
		return out, nil
	}
	rows, err := s.m.ListForCollection(ctx, f, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	values := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		values = append(values, map[string]string{"name": row.Name, "subtitle": row.Subtitle})
	}
	s.translateFieldsBatch(ctx, req.Lang, productcontract.EntityTypeProduct, values)
	// 只用定价事实（五列投影）：这里要的是最低价 / 最高价 / 划线价，
	// 变体全行（选项、图片、元数据…）与本屏无关。口径不变，只少搬带宽。
	variants, err := s.m.ListVariantPriceFacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagIndex(ctx, req.ProjectID, rows)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	for i, row := range rows {
		// 显式工程与状态判定不依赖数据库连接角色是否绕过RLS。
		if row.ProjectID != req.ProjectID || row.Status != productenums.StatusPublished || paths[row.ID] == "" {
			continue
		}
		item := productdto.PublicCatalogItemResp{ID: row.ID, Name: values[i]["name"], Subtitle: values[i]["subtitle"], URL: seo.AbsoluteSiteURL(paths[row.ID]), Images: imageURLs(row), Slug: row.Slug, Unit: row.Unit, Tags: []string{}}
		var alts []string
		_ = json.Unmarshal(row.ImageAlts, &alts)
		if len(alts) > 0 {
			item.ImageAlt = alts[0]
		}
		for _, tagID := range decodeStrings(row.TagIDs) {
			if tag := tags[tagID]; tag != nil {
				item.Tags = append(item.Tags, tag.Name)
			}
		}
		// 价格汇总走共享纯函数（product_variant_price.go）：启用范围与同价决胜规则只有一份。
		// 口径仍是「只看启用变体、划线价取胜出的最低价变体」（DEC-01 待定，未改）。
		summary := summarizeVariantPrices(byProduct[row.ID], true)
		if summary.MinVariant != nil {
			price := yuanToCents(summary.MinPrice)
			item.Price = &price
			if summary.MinVariant.ComparePrice != nil {
				compare := yuanToCents(*summary.MinVariant.ComparePrice)
				item.ComparePrice = &compare
			}
			maxPrice := yuanToCents(summary.MaxPrice)
			item.MaxPrice = &maxPrice
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func publicCatalogSort(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "default", "newest", "oldest", "priceAsc", "priceDesc", "ratingDesc":
		return true
	}
	return false
}
