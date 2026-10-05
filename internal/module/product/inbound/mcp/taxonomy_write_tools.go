package productmcp

// taxonomy_write_tools.go — 商品的三个「分类维度」：品牌、分类、标签（含属性）。
//
// 为什么单独一批：它们描述的是**商品怎么被归类**，不是「某个商品」。
// 用户说「加个 Nike 品牌」「把烟具归到电子烟下面」时，改的是全站共用的字典，
// 影响面比改一个商品大得多 —— 一个分类错了，挂在它下面的商品在前台都跟着错位。
//
// 两个容易吃亏的语义，都在描述里写死：
// ① UpdateCategoryReq.ParentID 是**三态**：不传 = 不改层级；传空串 = 提升为顶级。
//    这与「把父级设成某个值」是三件事，而它们看起来都像「设父级」。
// ② 删除分类前必须没有子分类、删除品牌前该品牌下不能还挂着商品 ——
//    服务端会拒，但让模型先知道，它才会去查一遍而不是撞上去。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/permission"
)

// TaxonomyWriter 品牌 / 分类 / 标签的写能力（给 AI 工具的窄门）。
//
// 刻意不含 RecalcTags（按规则批量重算全站标签，是一次全局写）与
// SetAttributeValues（属性值的写入挂在商品上，属于商品编辑的一部分）。
type TaxonomyWriter interface {
	CreateBrand(ctx context.Context, req *productdto.CreateBrandReq) (*productdto.BrandResp, error)
	UpdateBrand(ctx context.Context, req *productdto.UpdateBrandReq) (*productdto.BrandResp, error)
	DeleteBrand(ctx context.Context, req *productdto.DeleteBrandReq) error

	CreateCategory(ctx context.Context, req *productdto.CreateCategoryReq) (*productdto.CategoryResp, error)
	UpdateCategory(ctx context.Context, req *productdto.UpdateCategoryReq) (*productdto.CategoryResp, error)
	DeleteCategory(ctx context.Context, req *productdto.DeleteCategoryReq) error

	CreateTag(ctx context.Context, req *productdto.CreateTagReq) (*productdto.TagResp, error)
	UpdateTag(ctx context.Context, req *productdto.UpdateTagReq) (*productdto.TagResp, error)
	DeleteTag(ctx context.Context, req *productdto.DeleteTagReq) error
}

// TaxonomyWriteTools 返回品牌 / 分类 / 标签的写工具集。
func TaxonomyWriteTools(w TaxonomyWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("productmcp: 分类维度写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		brandCreate(w), brandUpdate(w), brandDelete(w),
		categoryCreate(w), categoryUpdate(w), categoryDelete(w),
		tagCreate(w), tagUpdate(w), tagDelete(w),
	}, nil
}

func brandCreate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("brand_create", "新建品牌",
		"新建一个品牌（全站共用的字典项，不是挂在某个商品上）。\n"+
			"建完还要在商品里把 brandId 指过来，商品才会带上这个品牌。\n"+
			"slug 是前台 URL 用的短名；不传由后端按名称生成，重名时会自动加后缀。",
		permission.ProductBrandCreate,
		mcp.Object("新建品牌参数", map[string]mcp.Schema{
			"projectId":      mcp.String("工程 id（可选）"),
			"name":           mcp.String("品牌名（如 Nike）"),
			"slug":           mcp.String("URL 短名（可选；不传自动生成）"),
			"logo":           mcp.String("Logo 图 URL（可选）"),
			"description":    mcp.String("品牌介绍（可选）"),
			"seoTitle":       mcp.String("SEO 标题（可选）"),
			"seoDescription": mcp.String("SEO 描述（可选）"),
			"sort":           mcp.Integer("排序值（可选，越小越靠前）"),
		}, "name"),
		nil,
		func(ctx context.Context, args brandCreateArgs) (mcp.Result, error) {
			res, err := w.CreateBrand(ctx, &productdto.CreateBrandReq{
				ProjectID:      strings.TrimSpace(args.ProjectID),
				Name:           strings.TrimSpace(args.Name),
				Slug:           strings.TrimSpace(args.Slug),
				Logo:           strings.TrimSpace(args.Logo),
				Description:    args.Description,
				SEOTitle:       args.SEOTitle,
				SEODescription: args.SEODescription,
				Sort:           args.Sort,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "品牌已新建。"}, nil
			}
			return mcp.Result{Text: fmt.Sprintf(
				"品牌「%s」已新建（id=%s，slug=%s）。要让它挂在商品上，还得把商品的 brandId 改成这个 id。",
				res.Name, res.ID, emptyAsDash(res.Slug))}, nil
		})
}

type brandCreateArgs struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Logo           string `json:"logo"`
	Description    string `json:"description"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Sort           int    `json:"sort"`
}

type brandUpdateArgs struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	Name           *string `json:"name"`
	Slug           *string `json:"slug"`
	Logo           *string `json:"logo"`
	Description    *string `json:"description"`
	SEOTitle       *string `json:"seoTitle"`
	SEODescription *string `json:"seoDescription"`
	Sort           *int64  `json:"sort"`
}

func brandUpdate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("brand_update", "修改品牌",
		"改品牌的名字、slug、Logo、介绍或排序。**只改你传的字段**，没传的保持原样。\n"+
			"改 name 或 slug 会连带影响前台已经用旧短名收录的链接（slug 变了旧链接就 404），"+
			"除非用户明确要改，否则别动 slug。",
		permission.ProductBrandUpdate,
		mcp.Object("修改品牌参数", map[string]mcp.Schema{
			"id":             mcp.String("品牌 id"),
			"projectId":      mcp.String("工程 id（可选）"),
			"name":           mcp.String("新品牌名（可选）"),
			"slug":           mcp.String("新 URL 短名（可选；**改了旧链接会 404**）"),
			"logo":           mcp.String("新 Logo URL（可选）"),
			"description":    mcp.String("新品牌介绍（可选）"),
			"seoTitle":       mcp.String("新 SEO 标题（可选）"),
			"seoDescription": mcp.String("新 SEO 描述（可选）"),
			"sort":           mcp.Integer("新排序值（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args brandUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateBrandReq{
				ID:             strings.TrimSpace(args.ID),
				ProjectID:      strings.TrimSpace(args.ProjectID),
				Name:           args.Name,
				Slug:           args.Slug,
				Logo:           args.Logo,
				Description:    args.Description,
				SEOTitle:       args.SEOTitle,
				SEODescription: args.SEODescription,
			}
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.Name == nil && req.Slug == nil && req.Logo == nil && req.Description == nil &&
				req.SEOTitle == nil && req.SEODescription == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateBrand(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "品牌已修改。"}, nil
			}
			return mcp.Result{Text: fmt.Sprintf("品牌 %s 已更新，现在叫「%s」（slug=%s）。",
				res.ID, res.Name, emptyAsDash(res.Slug))}, nil
		})
}

type brandDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func brandDelete(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("brand_delete", "删除品牌",
		"删除一个品牌。**品牌下还挂着商品时删不掉** —— 服务端会拒，先确认清楚。\n"+
			"删除品牌不会动商品本身，但那批商品的品牌就变成空的（前台可能显示成「未分类」）。\n"+
			"用户说的是「这个牌子不做了」而不是「删掉这个品牌」时，先问一句是不是要把商品改到别的品牌。",
		permission.ProductBrandDelete,
		mcp.Object("删除品牌参数", map[string]mcp.Schema{
			"id":        mcp.String("品牌 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args brandDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteBrand(ctx, &productdto.DeleteBrandReq{
				ID: strings.TrimSpace(args.ID), ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"品牌 %s 已删除。原本挂在这个品牌下的商品不会消失，但它们的品牌变成空了。", args.ID)}, nil
		})
}

func categoryCreate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("category_create", "新建商品分类",
		"新建一个商品分类，可以是顶级分类，也可以是某个分类的子分类（传 parentId）。\n"+
			"分类是**树形**的：前台导航、筛选都按这棵树走，所以 parentId 传错会把分类挂到错误的层级，"+
			"商品在前台就跟着错位。parentId 用 category_list 拿。\n"+
			"slug 是前台 URL 用的短名；不传由后端按名称生成。",
		permission.ProductCategoryCreate,
		mcp.Object("新建分类参数", map[string]mcp.Schema{
			"projectId":      mcp.String("工程 id（可选）"),
			"parentId":       mcp.String("父分类 id（可选；不传 = 顶级分类）"),
			"name":           mcp.String("分类名（如 电子烟）"),
			"slug":           mcp.String("URL 短名（可选；不传自动生成）"),
			"description":    mcp.String("分类描述（可选）"),
			"image":          mcp.String("分类图 URL（可选）"),
			"seoTitle":       mcp.String("SEO 标题（可选）"),
			"seoDescription": mcp.String("SEO 描述（可选）"),
			"sort":           mcp.Integer("排序值（可选，越小越靠前）"),
		}, "name"),
		nil,
		func(ctx context.Context, args categoryCreateArgs) (mcp.Result, error) {
			res, err := w.CreateCategory(ctx, &productdto.CreateCategoryReq{
				ProjectID:      strings.TrimSpace(args.ProjectID),
				ParentID:       strings.TrimSpace(args.ParentID),
				Name:           strings.TrimSpace(args.Name),
				Slug:           strings.TrimSpace(args.Slug),
				Description:    args.Description,
				Image:          strings.TrimSpace(args.Image),
				SEOTitle:       args.SEOTitle,
				SEODescription: args.SEODescription,
				Sort:           args.Sort,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "分类已新建。"}, nil
			}
			level := "顶级分类"
			if strings.TrimSpace(res.ParentID) != "" {
				level = "挂在父分类 " + res.ParentID + " 下"
			}
			return mcp.Result{Text: fmt.Sprintf(
				"分类「%s」已新建（id=%s，%s，slug=%s）。", res.Name, res.ID, level, emptyAsDash(res.Slug))}, nil
		})
}

type categoryCreateArgs struct {
	ProjectID      string `json:"projectId"`
	ParentID       string `json:"parentId"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	Image          string `json:"image"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Sort           int    `json:"sort"`
}

type categoryUpdateArgs struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	ParentID       *string `json:"parentId"`
	Name           *string `json:"name"`
	Slug           *string `json:"slug"`
	Description    *string `json:"description"`
	Image          *string `json:"image"`
	SEOTitle       *string `json:"seoTitle"`
	SEODescription *string `json:"seoDescription"`
	Sort           *int64  `json:"sort"`
}

func categoryUpdate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("category_update", "修改商品分类",
		"改分类的名字、层级、图、描述或排序。**只改你传的字段**。\n"+
			"**parentId 是三态，别搞混**：\n"+
			"· 不传 parentId → 层级不动（只改名之类）；\n"+
			"· 传 parentId=\"\"（空串）→ **提升为顶级分类**；\n"+
			"· 传 parentId=\"某 id\" → 挂到那个分类下面。\n"+
			"把分类挂到自己或自己的子孙下面会形成环，服务端会拒。",
		permission.ProductCategoryUpdate,
		mcp.Object("修改分类参数", map[string]mcp.Schema{
			"id":             mcp.String("分类 id"),
			"projectId":      mcp.String("工程 id（可选）"),
			"parentId":       mcp.String("父分类 id（**三态**：不传=不改层级；传空串=\"\"=提升为顶级；传 id=挂到该分类下）"),
			"name":           mcp.String("新分类名（可选）"),
			"slug":           mcp.String("新 URL 短名（可选；改了旧链接会 404）"),
			"description":    mcp.String("新描述（可选）"),
			"image":          mcp.String("新分类图 URL（可选）"),
			"seoTitle":       mcp.String("新 SEO 标题（可选）"),
			"seoDescription": mcp.String("新 SEO 描述（可选）"),
			"sort":           mcp.Integer("新排序值（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args categoryUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateCategoryReq{
				ID:             strings.TrimSpace(args.ID),
				ProjectID:      strings.TrimSpace(args.ProjectID),
				ParentID:       args.ParentID,
				Name:           args.Name,
				Slug:           args.Slug,
				Description:    args.Description,
				Image:          args.Image,
				SEOTitle:       args.SEOTitle,
				SEODescription: args.SEODescription,
			}
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.ParentID == nil && req.Name == nil && req.Slug == nil && req.Description == nil &&
				req.Image == nil && req.SEOTitle == nil && req.SEODescription == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateCategory(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "分类已修改。"}, nil
			}
			level := "顶级分类"
			if strings.TrimSpace(res.ParentID) != "" {
				level = "父分类 " + res.ParentID
			}
			return mcp.Result{Text: fmt.Sprintf("分类 %s 已更新（现在叫「%s」，层级：%s）。",
				res.ID, res.Name, level)}, nil
		})
}

type categoryDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func categoryDelete(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("category_delete", "删除商品分类",
		"删除一个分类。**它下面还有子分类时删不掉**（服务端会拒）—— "+
			"要删整棵树得从叶子往上删；也可以先上删除掉父分类。\n"+
			"分类下挂着的商品不会消失，但那批商品就不在这个分类里了（前台筛不出来）。\n"+
			"用户说「这个分类先不要了」时，先确认是不是只想去掉前台展示，而不是删数据。",
		permission.ProductCategoryDelete,
		mcp.Object("删除分类参数", map[string]mcp.Schema{
			"id":        mcp.String("分类 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args categoryDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteCategory(ctx, &productdto.DeleteCategoryReq{
				ID: strings.TrimSpace(args.ID), ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"分类 %s 已删除。原本挂在这个分类下的商品还在，但已经不属于这个分类了。", args.ID)}, nil
		})
}

func tagCreate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("tag_create", "新建商品标签",
		"新建一个商品标签。标签与分类不同：分类是一棵树（一个商品只在一个位置），"+
			"标签是**多个平铺的标记**（一个商品可以同时有「热销」「新品」「清仓」）。\n"+
			"**本工具只建手工标签**（由人/流程显式打上去）。另一种是「规则标签」—— "+
			"按条件自动命中商品（如「30 天未售出」），那种标签会自己增减商品归属，"+
			"属于批量写，不在这个工具的能力范围内，要用请去后台「商品 → 标签」。\n"+
			"新建前先用 tag_list 看一眼现有标签，别造出「热销」和「热卖」这种同义重复的 —— "+
			"两个标签各自挂一半商品，之后按标签做活动就会漏人。",
		permission.ProductTagCreate,
		mcp.Object("新建标签参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（可选）"),
			"name":      mcp.String("标签名（如 热销）"),
			"slug":      mcp.String("URL 短名（可选；不传自动生成）"),
			"sort":      mcp.Integer("排序值（可选，越小越靠前）"),
		}, "name"),
		nil,
		func(ctx context.Context, args tagCreateArgs) (mcp.Result, error) {
			res, err := w.CreateTag(ctx, &productdto.CreateTagReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Name:      strings.TrimSpace(args.Name),
				Slug:      strings.TrimSpace(args.Slug),
				Sort:      args.Sort,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "标签已新建。"}, nil
			}
			return mcp.Result{Text: fmt.Sprintf(
				"标签「%s」已新建（id=%s，%s）。要把它加到商品上还得在商品里改标签，"+
					"新建本身不会影响任何商品。", res.Name, res.ID, tagKindText(res.Kind))}, nil
		})
}

type tagCreateArgs struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Sort      int    `json:"sort"`
}

type tagUpdateArgs struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"projectId"`
	Name      *string `json:"name"`
	Slug      *string `json:"slug"`
	Sort      *int64  `json:"sort"`
}

func tagUpdate(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("tag_update", "修改商品标签",
		"改标签的名字或排序。**只改你传的字段**。\n"+
			"改名不影响已打了这个标签的商品 —— 它们跟着的是标签 id，不是名字。\n"+
			"规则标签（按条件自动命中商品的那种）的规则本身改不了，本工具只能改名与排序。",
		permission.ProductTagUpdate,
		mcp.Object("修改标签参数", map[string]mcp.Schema{
			"id":        mcp.String("标签 id"),
			"projectId": mcp.String("工程 id（可选）"),
			"name":      mcp.String("新标签名（可选）"),
			"slug":      mcp.String("新 URL 短名（可选）"),
			"sort":      mcp.Integer("新排序值（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args tagUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateTagReq{
				ID: strings.TrimSpace(args.ID), ProjectID: strings.TrimSpace(args.ProjectID),
				Name: args.Name, Slug: args.Slug,
			}
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.Name == nil && req.Slug == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateTag(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "标签已修改。"}, nil
			}
			return mcp.Result{Text: fmt.Sprintf("标签 %s 已更新，现在叫「%s」（%s）。",
				res.ID, res.Name, tagKindText(res.Kind))}, nil
		})
}

type tagDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func tagDelete(w TaxonomyWriter) mcp.Tool {
	return mcp.NewWrite("tag_delete", "删除商品标签",
		"删除一个标签。**已打在商品上的这个标签会一并消失**（商品本身不动）。\n"+
			"如果有自动化规则按这个标签触发，删掉之后那些规则就不再命中任何商品了 —— "+
			"规则不会消失，只是永远不会被触发，这种「静默失效」比报错更难发现。"+
			"删之前用 tag_list 或商品详情确认一下这个标签有没有在用。",
		permission.ProductTagDelete,
		mcp.Object("删除标签参数", map[string]mcp.Schema{
			"id":        mcp.String("标签 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args tagDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteTag(ctx, &productdto.DeleteTagReq{
				ID: strings.TrimSpace(args.ID), ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"标签 %s 已删除，打在商品上的这个标签也一并消失了；"+
					"如果有按这个标签触发的自动化规则，它们从此不会再命中任何商品。", args.ID)}, nil
		})
}

// tagKindText 说清这个标签是怎么来的 —— 手工打的，还是规则自动命中的。
//
// 用户删/改标签时这个差别决定后果：规则标签会自己把商品重新挂回来，
// 手工标签删了就真没了。
func tagKindText(kind string) string {
	if strings.TrimSpace(kind) == "rule" {
		return "**规则标签**：商品归属由规则自动算出来，不是手工打的"
	}
	return "手工标签"
}

// TaxonomyReader 列出三个分类维度的现有条目。
//
// 它存在是因为**写工具需要它**：category_create 要 parentId、
// brand_delete / tag_delete 要先看有没有在用，而这些 id 只能从这里拿。
// 「工具的输出里要带上下一步动作需要的参数」这条已经在 stock_find 漏 variantId、
// stock_reasons 漏 id、建活动缺 accountId 上吃过三回 —— 这次在写工具之前就补上。
type TaxonomyReader interface {
	ListBrands(ctx context.Context, req *productdto.ListBrandReq) ([]*productdto.BrandResp, error)
	ListCategories(ctx context.Context, req *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error)
	ListTags(ctx context.Context, req *productdto.ListTagReq) ([]*productdto.TagResp, error)
}

// TaxonomyTools 返回三个维度清单的只读工具。
func TaxonomyTools(r TaxonomyReader) ([]mcp.Tool, error) {
	if r == nil {
		return nil, errors.New("productmcp: 分类维度读依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{brandList(r), categoryList(r), tagList(r)}, nil
}

func brandList(r TaxonomyReader) mcp.Tool {
	return mcp.New("brand_list", "列出现有品牌",
		"列出本站的品牌（含 id 与 slug）。要建品牌、要把商品改到某个品牌时用这里的 id。\n"+
			"列表很长时用 keyword 过滤。",
		permission.ProductBrandList,
		mcp.Object("品牌列表参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（可选）"),
			"keyword":   mcp.String("按品牌名筛选（可选）"),
		}),
		func(ctx context.Context, args taxonomyListArgs) (mcp.Result, error) {
			list, err := r.ListBrands(ctx, &productdto.ListBrandReq{
				ProjectID: strings.TrimSpace(args.ProjectID), Keyword: strings.TrimSpace(args.Keyword),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有符合条件的品牌。用 brand_create 建一个。"}, nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "共 %d 个品牌：\n", len(list))
			for _, it := range list {
				if it == nil {
					continue
				}
				fmt.Fprintf(&b, "- id=%s「%s」（slug=%s）\n", it.ID, it.Name, emptyAsDash(it.Slug))
			}
			return mcp.Result{Text: strings.TrimRight(b.String(), "\n")}, nil
		})
}

func categoryList(r TaxonomyReader) mcp.Tool {
	return mcp.New("category_list", "列出现有商品分类",
		"列出本站的商品分类（含 id 与层级）。建子分类、把商品归类时用这里的 id 当 parentId。\n"+
			"分类是树形的，列表按父子缩进输出 —— 缩进层级就是它在树里的位置。",
		permission.ProductCategoryList,
		mcp.Object("分类列表参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（可选）"),
			"keyword":   mcp.String("按分类名筛选（可选；命中的分类会连同它所在的整条路径一起列出）"),
		}),
		func(ctx context.Context, args taxonomyListArgs) (mcp.Result, error) {
			list, err := r.ListCategories(ctx, &productdto.ListCategoryReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Keyword:   strings.TrimSpace(args.Keyword),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有符合条件的分类。用 category_create 建一个（不传 parentId 就是顶级分类）。"}, nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "共 %d 个分类：\n", len(list))
			for _, it := range list {
				if it == nil {
					continue
				}
				indent := strings.Repeat("  ", it.Depth)
				parent := ""
				if strings.TrimSpace(it.ParentID) != "" {
					parent = "，父级 " + it.ParentID
				}
				fmt.Fprintf(&b, "- %sid=%s「%s」（slug=%s%s）\n",
					indent, it.ID, it.Name, emptyAsDash(it.Slug), parent)
			}
			b.WriteString("要建子分类就把父分类的 id 传给 category_create 的 parentId。")
			return mcp.Result{Text: b.String()}, nil
		})
}

func tagList(r TaxonomyReader) mcp.Tool {
	return mcp.New("tag_list", "列出现有商品标签",
		"列出本站的商品标签（含 id、是手工标签还是规则标签）。给商品打标签、改标签时用这里的 id。\n"+
			"建新标签前先看这里：同义重复的标签（「热销」与「热卖」）会让之后按标签做的活动漏人。",
		permission.ProductTagList,
		mcp.Object("标签列表参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（可选）"),
			"keyword":   mcp.String("按标签名筛选（可选）"),
		}),
		func(ctx context.Context, args taxonomyListArgs) (mcp.Result, error) {
			list, err := r.ListTags(ctx, &productdto.ListTagReq{
				ProjectID: strings.TrimSpace(args.ProjectID), Keyword: strings.TrimSpace(args.Keyword),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有符合条件的标签。用 tag_create 建一个。"}, nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "共 %d 个标签：\n", len(list))
			for _, it := range list {
				if it == nil {
					continue
				}
				fmt.Fprintf(&b, "- id=%s「%s」（%s）\n", it.ID, it.Name, tagKindText(it.Kind))
			}
			return mcp.Result{Text: strings.TrimRight(b.String(), "\n")}, nil
		})
}

type taxonomyListArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword"`
}
