package productmcp

// 属性组和标签是两回事，别混：标签是「运营打的标记」（热销 / 清仓），
// 属性组是「商品本身的规格」（颜色 / 尺码 / 容量），它决定商品在前台怎么被筛选。
// 其中一个开关尤其关键 —— IsVariation=true 的属性组是**变体的来源**：
// 它的值会被拿去笛卡尔积出 SKU。改错了会让商品的变体结构对不上。

// 与内容模块同构：一个读 + 三个写，写工具走 mcp.NewWrite（确认位 + 幂等键）。
// 只覆盖**商品主体**（products 一行）。属性 / 品牌 / 分类 / 捆绑配置各自是独立的数据形态，
// 混进同一批会让「商品」这个词在工具列表里指五样东西 —— 模型分不清该用哪个，
// 而分不清的代价是它挑一个看起来最像的调下去。
//
// **依赖断言成收窄的端口**而不是整个 ProductService：后者同时握着
// SetBundleConfig / UpdateVariantCost / CreateAttribute，工具层握着它们时
// 「顺手调个价」会从「显式加一个工具」退化成「随手就能做」。

// 为什么单独一批：它们描述的是**商品怎么被归类**，不是「某个商品」。
// 用户说「加个 Nike 品牌」「把烟具归到电子烟下面」时，改的是全站共用的字典，
// 影响面比改一个商品大得多 —— 一个分类错了，挂在它下面的商品在前台都跟着错位。
//
// 两个容易吃亏的语义，都在描述里写死：
// ① UpdateCategoryReq.ParentID 是**三态**：不传 = 不改层级；传空串 = 提升为顶级。
//    这与「把父级设成某个值」是三件事，而它们看起来都像「设父级」。
// ② 删除分类前必须没有子分类、删除品牌前该品牌下不能还挂着商品 ——
//    服务端会拒，但让模型先知道，它才会去查一遍而不是撞上去。

// 为什么和商品主体（product_tools.go）分开成一批：
// products 一行是「这个东西是什么」，product_variants 一行是「它的哪个版本、卖多少钱、
// 库存挂在哪」—— 定价、SKU、条码、库存归属都长在变体上。用户说「加个蓝色的」「改成 99 块」
// 说的都是变体，而在此之前没有任何工具能碰它们。
//
// **金额单位是本批最容易搞错的地方**：product 模块一律用「元」（float64），
// 而 order 模块用「分」（int64）。同一个仓库里两套口径，所以这里每个金额字段的描述
// 都显式写了单位 —— 模型上一次当成「分」就会把 99 元的商品定成 0.99 元。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/permission"
)

// AttributeWriter 属性组的写能力（给 AI 工具的窄门）。
//
// 刻意**不含 SetAttributeValues**：它的语义是「全量替换」——
// 请求里没有的值被删除，无 id 的新建。模型漏写一个值就等于删掉它，
// 而它可能正被某批商品的变体引用着。改属性值请去后台「商品 → 属性」，
// 那里能看到现有值、不会整批覆盖。
type AttributeWriter interface {
	CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (*productdto.AttributeResp, error)
	UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (*productdto.AttributeResp, error)
	DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) error
	ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error)
}

// AttributeTools 返回属性组工具集（1 读 + 3 写）。
func AttributeTools(w AttributeWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("productmcp: 属性写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		attributeList(w), attributeCreate(w), attributeUpdate(w), attributeDelete(w),
	}, nil
}

func attributeList(w AttributeWriter) mcp.Tool {
	return mcp.New("attribute_list", "列出商品属性组（规格）",
		"列出本站的商品属性组（如 颜色 / 尺码 / 容量）与它们的取值。\n"+
			"`用于变体=true` 的属性组是**变体的来源** —— 它的值会被拿去组合生成 SKU，"+
			"商品的规格结构就由它们决定。\n"+
			"改属性、删属性之前先用这里的 id。",
		permission.ProductAttributeList,
		mcp.Object("属性组列表参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（可选）"),
			"keyword":   mcp.String("按属性组名筛选（可选）"),
		}),
		func(ctx context.Context, args taxonomyListArgs) (mcp.Result, error) {
			list, err := w.ListAttributes(ctx, &productdto.ListAttributeReq{
				ProjectID: strings.TrimSpace(args.ProjectID), Keyword: strings.TrimSpace(args.Keyword),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有符合条件的属性组。用 attribute_create 建一个（如「颜色」，值填红/蓝）。"}, nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "共 %d 个属性组：\n", len(list))
			for _, it := range list {
				if it == nil {
					continue
				}
				mark := "仅用于展示筛选"
				if it.IsVariation {
					mark = "**用于变体**（这些值参与生成 SKU）"
				}
				fmt.Fprintf(&b, "- id=%s「%s」（key=%s，%s，%d 个取值%s）\n",
					it.ID, it.Name, emptyAsDash(it.Key), mark, len(it.Values), attrValuesText(it.Values))
			}
			return mcp.Result{Text: strings.TrimRight(b.String(), "\n")}, nil
		})
}

func attrValuesText(values []productdto.AttributeValueResp) string {
	if len(values) == 0 {
		return ""
	}
	labels := make([]string, 0, len(values))
	for _, v := range values {
		labels = append(labels, v.Label)
	}
	return "：" + strings.Join(labels, "、")
}

func attributeCreate(w AttributeWriter) mcp.Tool {
	return mcp.NewWrite("attribute_create", "新建商品属性组（规格）",
		"新建一个属性组（如「颜色」）并给出它的取值（红 / 蓝）。\n"+
			"**isVariation 要慎重**：\n"+
			"· `true` = 这个属性参与生成商品变体（SKU）—— 颜色=true 时，商品会按颜色组合出多个变体。\n"+
			"· `false`（默认）= 只作为筛选条件展示，不影响变体结构。\n"+
			"把不该参与变体的属性设成 true，会让商品凭空多出一批 SKU；"+
			"改回来比一开始就设对麻烦得多。\n"+
			"属性组的 key 要和其它工程内已有的区分开，重复 key 会建失败。",
		permission.ProductAttributeCreate,
		mcp.Object("新建属性组参数", map[string]mcp.Schema{
			"projectId":   mcp.String("工程 id（可选）"),
			"key":         mcp.String("属性标识（可选；不传由后端生成，如 color）"),
			"name":        mcp.String("属性组名（如 颜色）"),
			"isVariation": mcp.Boolean("是否参与生成变体（可选，默认 false；**true 会让商品按这个属性组合出多个 SKU**）"),
			"sort":        mcp.Integer("排序值（可选，越小越靠前）"),
			"values": mcp.Array("取值列表（可选；如 [\"红\",\"蓝\"]）",
				mcp.String("一个取值，如 红")),
		}, "name"),
		nil,
		func(ctx context.Context, args attributeCreateArgs) (mcp.Result, error) {
			req := &productdto.CreateAttributeReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Key:       strings.TrimSpace(args.Key),
				Name:      strings.TrimSpace(args.Name),
				Sort:      args.Sort,
			}
			if args.IsVariation != nil {
				b := *args.IsVariation
				req.IsVariation = &b
			}
			// Sort 用**过滤后的相对序号**，不是原数组下标 ——
			// 输入里夹了空串时，用下标会留下空洞（0、2），
			// 而空洞会让后续的上下移动对不上位置。
			for _, label := range args.Values {
				if strings.TrimSpace(label) == "" {
					continue
				}
				req.Values = append(req.Values, productdto.AttributeValueReq{
					Label: strings.TrimSpace(label), Sort: len(req.Values),
				})
			}
			res, err := w.CreateAttribute(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: attributeCreatedText(res, req)}, nil
		})
}

type attributeCreateArgs struct {
	ProjectID   string   `json:"projectId"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	IsVariation *bool    `json:"isVariation"`
	Sort        int      `json:"sort"`
	Values      []string `json:"values"`
}

func attributeCreatedText(res *productdto.AttributeResp, req *productdto.CreateAttributeReq) string {
	if res == nil {
		return "属性组已新建。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "属性组「%s」已新建（id=%s，%d 个取值%s）。",
		res.Name, res.ID, len(res.Values), attrValuesText(res.Values))
	if res.IsVariation {
		b.WriteString("\n**这个属性参与生成变体**：用到它的商品会按这些取值组合出多个 SKU。" +
			"如果只是想在前台做个筛选条件，应该把 isVariation 设成 false。")
	} else {
		b.WriteString("\n它只作为筛选条件，不影响商品的变体结构。")
	}
	if len(req.Values) == 0 {
		b.WriteString("\n**它现在是空属性组**（没有取值）。空属性组在商品编辑页里没法选，记得补上值。")
	}
	return b.String()
}

type attributeUpdateArgs struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"projectId"`
	Key         *string `json:"key"`
	Name        *string `json:"name"`
	IsVariation *bool   `json:"isVariation"`
	Sort        *int64  `json:"sort"`
}

func attributeUpdate(w AttributeWriter) mcp.Tool {
	return mcp.NewWrite("attribute_update", "修改商品属性组",
		"改属性组的名字、标识、排序，或切换「是否参与变体」。**只改你传的字段**。\n"+
			"**改不了取值** —— 增删取值走后台「商品 → 属性」（那里的语义是全量替换，"+
			"用工具做的话漏写一个值就等于删掉它，而它可能正被变体引用着）。\n"+
			"把 isVariation 从 false 改成 true 会让用到它的商品开始按这个属性组合变体；"+
			"从 true 改成 false 则会让已有的变体结构失去依据 —— 两个方向都要先跟用户确认。",
		permission.ProductAttributeUpdate,
		mcp.Object("修改属性组参数", map[string]mcp.Schema{
			"id":          mcp.String("属性组 id（用 attribute_list 拿）"),
			"projectId":   mcp.String("工程 id（可选）"),
			"key":         mcp.String("新标识（可选；改动影响面大）"),
			"name":        mcp.String("新属性组名（可选）"),
			"isVariation": mcp.Boolean("是否参与生成变体（可选；**两个方向都会改变商品的 SKU 结构，先确认**）"),
			"sort":        mcp.Integer("新排序值（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args attributeUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateAttributeReq{
				ID:          strings.TrimSpace(args.ID),
				ProjectID:   strings.TrimSpace(args.ProjectID),
				Key:         args.Key,
				Name:        args.Name,
				IsVariation: args.IsVariation,
			}
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.Key == nil && req.Name == nil && req.IsVariation == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateAttribute(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "属性组已修改。"}, nil
			}
			mark := "仅用于展示筛选"
			if res.IsVariation {
				mark = "**用于变体**"
			}
			return mcp.Result{Text: fmt.Sprintf("属性组 %s 已更新，现在叫「%s」（%s，%d 个取值）。",
				res.ID, res.Name, mark, len(res.Values))}, nil
		})
}

type attributeDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func attributeDelete(w AttributeWriter) mcp.Tool {
	return mcp.NewWrite("attribute_delete", "删除商品属性组",
		"删除一个属性组（连同它的全部取值）。\n"+
			"**如果这个属性组参与生成变体，删掉它会让用到它的商品的规格结构失去依据** ——"+
			"那些变体不会自动消失，但前台的规格选择器就没东西可显示了。\n"+
			"删之前先用 attribute_list 确认它有没有被商品用、是不是用于变体。",
		permission.ProductAttributeDelete,
		mcp.Object("删除属性组参数", map[string]mcp.Schema{
			"id":        mcp.String("属性组 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args attributeDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{
				ID: strings.TrimSpace(args.ID), ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"属性组 %s 及其取值已删除。用到它的商品不会消失，"+
					"但如果它是用于变体的属性，那些商品的规格选择器就没依据了 —— "+
					"去商品编辑页确认一下前台还正不正常。", args.ID)}, nil
		})
}

// ProductReader 读商品（单条 + 列表）。
type ProductReader interface {
	Get(ctx context.Context, req *productdto.GetReq) (*productdto.ProductResp, error)
	List(ctx context.Context, req *productdto.ListReq) ([]*productdto.ProductResp, error)
}

// ProductWriter 商品主体上的三个写操作。
type ProductWriter interface {
	Create(ctx context.Context, req *productdto.CreateReq) (*productdto.ProductResp, error)
	Update(ctx context.Context, req *productdto.UpdateReq) (*productdto.ProductResp, error)
	Delete(ctx context.Context, req *productdto.DeleteReq) error
}

// Tools 返回商品模块的工具集。
func Tools(r ProductReader, w ProductWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if r == nil || w == nil {
		return nil, errors.New("productmcp: 商品读写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		productFind(r),
		productGet(r),
		productCreate(w, store),
		productUpdate(w, store),
		productDelete(w, store),
	}, nil
}

// projectIDArg 商品全部工具共用的作用域参数。
//
// 商品是**多站点隔离**的数据（project_id），所以每个工具都必须带上它 ——
// 不带时 service 会去猜「唯一工程」，多站点下那个兜底会直接报错，
// 而报错文案指向的是 service 内部，看不出是调用方漏了参数。
func projectIDArg() mcp.Schema {
	return mcp.String("站点工程 id（uuid）。商品按工程隔离，必填。")
}

// productFindArgs product_find 的入参。
type productFindArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword"`
	// Status 可选状态过滤（空串 = 全部状态）。取值域与 schema 的 Enum 同源，
	// 两处都从 productenums 取 —— 写死字面量时枚举一改，工具就静默接受一个
	// 底层不认识的值，而 List 的 status 条件会把它当成「没有这个状态」查出 0 条。
	Status string `json:"status"`
}

// productFind 按关键词找商品。
//
// **这个工具是必需的，不是锦上添花**：用户说的是「把那个商品的名字改一下」，
// 他不会念 uuid —— 而 uuid 是其余工具的唯一入口。没有这一环时模型只有两条路：
// 向用户索要 uuid（用户给不出），或者瞎猜一个（猜出来的 id 不存在，报错归到「商品不存在」，
// 看起来像是数据问题而不是工具集缺了一环）。
//
// 实测正是如此：给它一个 uuid 它会正确要求补 projectId，但**没人会那样说话**。
func productFind(r ProductReader) mcp.Tool {
	return mcp.New("product_find", "搜索商品",
		"按关键词搜索商品，返回匹配项的 id、名称、SKU、类型与状态。"+
			"**用户说的可能是商品名，也可能是 SKU**（「SKU 是 W1_CUP001 那个」），"+
			"两种情况都用这一个工具：关键词同时匹配商品名称与 SKU。"+
			"先用它把「用户说的那个」换成 id，再去调 product_get / product_update / product_delete。"+
			"要按状态缩小范围就传 status；都不传则按排序值列出前若干个（不是按创建时间）。",
		permission.ProductGet,
		mcp.Object("搜索参数", map[string]mcp.Schema{
			"projectId": projectIDArg(),
			"keyword":   mcp.String("商品名的一部分或 SKU（可为空，空则按排序值列出前若干个）"),
			"status": mcp.Enum("只看某个状态的商品（可选；不传则全部状态）",
				string(productenums.StatusDraft), string(productenums.StatusPublished), string(productenums.StatusArchived)),
		}, "projectId"),
		func(ctx context.Context, args productFindArgs) (mcp.Result, error) {
			list, err := r.List(ctx, &productdto.ListReq{
				ProjectID: args.ProjectID, Keyword: args.Keyword, Status: args.Status,
				Page: 1, Size: productFindLimit,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有匹配的商品（关键词：" + strings.TrimSpace(args.Keyword) + "）。" +
					"换个词试试，或者先问用户商品名的准确写法。"}, nil
			}
			var b strings.Builder
			b.WriteString("匹配到 " + strconv.Itoa(len(list)) + " 个商品：\n")
			for _, p := range list {
				b.WriteString("- " + p.Name + "（id=" + p.ID + "，SKU=" + p.SKUCode +
					"，类型=" + p.Type + "，状态=" + p.Status + "）\n")
			}
			if len(list) >= productFindLimit {
				b.WriteString("（只列了前 " + strconv.Itoa(productFindLimit) + " 条；" +
					"需要更准的结果就再说一个更具体的关键词。）\n")
			}
			return mcp.Result{Text: b.String()}, nil
		},
	)
}

// productFindLimit 搜索返回的条数上限。
//
// 20 是「够用来挑」与「不把上下文撑满」之间的取值：商品列表项带名称与 SKU，
// 再多就会挤掉后面对话需要的空间，而用户想找的那一个通常在前几条。
const productFindLimit = 20

// productGetArgs product_get 的入参。
type productGetArgs struct {
	ProjectID string `json:"projectId"`
	ID        string `json:"id"`
}

func productGet(r ProductReader) mcp.Tool {
	return mcp.New("product_get", "读取商品",
		"按 id 读出一个商品的字段（名称、副标题、URL 段、类型、状态、SKU 编码）。"+
			"**id 要先经 product_find 拿到**（用户通常只说商品名）；改或删之前先调它核对名称。",
		permission.ProductGet,
		mcp.Object("读取参数", map[string]mcp.Schema{
			"projectId": projectIDArg(),
			"id":        mcp.String("商品 id（uuid）"),
		}, "projectId", "id"),
		func(ctx context.Context, args productGetArgs) (mcp.Result, error) {
			res, err := r.Get(ctx, &productdto.GetReq{ProjectID: args.ProjectID, ID: args.ID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: describeProduct(res)}, nil
		},
	)
}

// productCreateArgs product_create 的业务入参。
//
// **刻意不收 SKU 编码与仓库参数**：那两条入口的规则（docs/14 §1.1）里，
// 从仓库选要同时给 WarehouseID 与 WarehouseSKU、自己创建要处理仓码前缀、
// 派生不出 ASCII 段时明确报错 —— 这套规则的输入是运营在表单上看着仓库列表做的决定，
// 不是模型能凭一句话补全的。收进来只会让它猜，猜错的形态是建出一个编码别扭的商品
// （而商品编码后面会被打印、被扫码，改起来比建错麻烦得多）。
type productCreateArgs struct {
	ProjectID string  `json:"projectId"`
	Name      string  `json:"name"`
	Slug      *string `json:"slug"`
	Subtitle  *string `json:"subtitle"`
}

func productCreate(w ProductWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("product_create", "新建商品",
		"新建一个商品主体（常规变体商品）。建好后还需要给变体（价格与库存）才能上架。"+
			"SKU 编码由系统按规则派生，本工具不接受它。",
		permission.ProductCreate,
		mcp.Object("新建商品参数", map[string]mcp.Schema{
			"projectId": projectIDArg(),
			"name":      mcp.String("商品名称"),
			"slug":      mcp.String("URL 段（可选）。留空由名称派生；同工程内唯一。"),
			"subtitle":  mcp.String("副标题（可选）"),
		}, "projectId", "name"),
		store,
		func(ctx context.Context, args productCreateArgs) (mcp.Result, error) {
			req := &productdto.CreateReq{
				ProjectID: args.ProjectID,
				Name:      args.Name,
			}
			if args.Slug != nil {
				req.Slug = *args.Slug
			}
			if args.Subtitle != nil {
				req.Subtitle = *args.Subtitle
			}
			res, err := w.Create(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已新建商品 " + res.Name + "（id=" + res.ID + "）"}, nil
		},
	)
}

// productUpdateArgs product_update 的业务入参。
//
// 指针类型与 service 的 UpdateReq 同形（nil = 本次不改）：这层多一次「空串 vs 缺省」
// 的折算，把「清空副标题」与「不改副标题」混成一个值，是这一批最容易写错的地方。
type productUpdateArgs struct {
	ProjectID string  `json:"projectId"`
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	Slug      *string `json:"slug"`
	Subtitle  *string `json:"subtitle"`
}

func productUpdate(w ProductWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("product_update", "更新商品",
		"按 id 更新商品的基本字段（名称 / URL 段 / 副标题）。只传要改的字段，"+
			"没传的保持原值；传空串就是清空该字段。**商品类型不可改**（变体 ↔ 捆绑涉及"+
			"有没有自己的 SKU 与价格放哪，要换类型只能新建）。",
		permission.ProductUpdate,
		mcp.Object("更新商品参数", map[string]mcp.Schema{
			"projectId": projectIDArg(),
			"id":        mcp.String("商品 id（uuid）"),
			"name":      mcp.String("新名称（不传 = 不改）"),
			"slug":      mcp.String("新 URL 段（不传 = 不改）"),
			"subtitle":  mcp.String("新副标题（不传 = 不改；传空串 = 清空）"),
		}, "projectId", "id"),
		store,
		func(ctx context.Context, args productUpdateArgs) (mcp.Result, error) {
			res, err := w.Update(ctx, &productdto.UpdateReq{
				ProjectID: args.ProjectID,
				ID:        args.ID,
				Name:      args.Name,
				Slug:      args.Slug,
				Subtitle:  args.Subtitle,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已更新商品 " + res.Name + "（id=" + res.ID + "）"}, nil
		},
	)
}

// productDeleteArgs product_delete 的入参。
type productDeleteArgs struct {
	ProjectID string `json:"projectId"`
	ID        string `json:"id"`
}

func productDelete(w ProductWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("product_delete", "删除商品",
		"按 id 删除一个商品（连同它的变体）。**这是不可逆操作**：删之前先 product_get "+
			"读出名称，把「要删哪个商品」说给用户听，确认后再调用本工具。",
		permission.ProductDelete,
		mcp.Object("删除商品参数", map[string]mcp.Schema{
			"projectId": projectIDArg(),
			"id":        mcp.String("要删除的商品 id"),
		}, "projectId", "id"),
		store,
		func(ctx context.Context, args productDeleteArgs) (mcp.Result, error) {
			if err := w.Delete(ctx, &productdto.DeleteReq{ProjectID: args.ProjectID, ID: args.ID}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已删除商品 " + args.ID}, nil
		},
	)
}

// describeProduct 把商品摊成便于模型读的文本。
//
// 只列**可变且模型需要知道**的字段：状态、类型、SKU 这些是它做判断的依据
// （比如「类型是 bundle，所以不能按变体商品理解」），而 id / projectId 要带上，
// 否则下一轮它无从引用这一行。
func describeProduct(res *productdto.ProductResp) string {
	if res == nil {
		return "（没有读到商品）"
	}
	var b strings.Builder
	b.WriteString("商品（id=" + res.ID + "，projectId=" + res.ProjectID + "）\n")
	writeLine(&b, "名称", res.Name)
	writeLine(&b, "副标题", res.Subtitle)
	writeLine(&b, "URL 段", res.Slug)
	writeLine(&b, "类型", res.Type)
	writeLine(&b, "状态", res.Status)
	writeLine(&b, "SKU 编码", res.SKUCode)
	// 价格与库存是用户最常追问的两件事（「多少钱」「还有货吗」），而它们都已由
	// toResp 填好 —— 之前只列了六个字段，模型答不了这两问只能让用户自己去后台看。
	//
	// 区间用「最低~最高」；只有一个价位时两侧相同，写成区间会让答案读起来啰嗦。
	if res.PriceMin == res.PriceMax {
		writeLine(&b, "价格", formatPrice(res.PriceMin))
	} else {
		writeLine(&b, "价格区间", formatPrice(res.PriceMin)+" ~ "+formatPrice(res.PriceMax))
	}
	b.WriteString("- 变体数: " + strconv.Itoa(res.VariantCount) + "\n")
	// 库存是**查询期聚合**（真源在 inventory_stocks），三态必须原样交代：
	//   infinite = 至少一个仓不跟踪库存（等价于「要多少有多少」）；
	//   tracked  = 全部仓都跟踪，后面那个数才有意义；
	//   none     = 任何仓都没有库存行，**不等于 0**（「没入库」与「入库了但没货」是两回事）。
	switch res.StockState {
	case productenums.StockStateInfinite:
		writeLine(&b, "库存", "不限（至少一个仓库不跟踪库存）")
	case productenums.StockStateTracked:
		writeLine(&b, "库存", strconv.Itoa(res.StockTotal)+"（各仓合计）")
	default:
		writeLine(&b, "库存", "未入库（任何仓库都没有该商品的库存行）")
	}
	// 上架时间：用户会问「什么时候上的架」，而「新品」的判断也靠它。
	writeLine(&b, "创建时间", res.CreatedAt)
	writeLine(&b, "上架时间", res.PublishedAt)
	return b.String()
}

// formatPrice 把概览用的金额格式化成便于阅读的形态。
//
// 不做货币符号也不做千分位：**这个工具不知道工程用哪种货币**（货币是站点/工程级配置，
// 商品本身不存），凭空写个 ¥ 会在非人民币站点上给出错误答案。保留两位小数即可，
// 需要符号时让模型去问用户或查站点配置。
func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// writeLine 写一行「字段: 值」，空值也写（标出「本字段为空」）。
//
// 不能省略空值行：模型要靠「这个字段存在但为空」与「这个字段不存在」区分
// 「清空它」和「别管它」—— 省略之后两者长得一样。
func writeLine(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		b.WriteString("- " + label + ": （空）\n")
		return
	}
	b.WriteString("- " + label + ": " + value + "\n")
}

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

// VariantWriter 变体写能力（给 AI 工具的窄门）。
//
// 刻意不含 GenerateVariants（一次按属性笛卡尔积批量造变体 —— 它会覆盖既有 SKU 的
// 价格与库存口径，是给「刚建完商品、配置规格」这一步用的批量动作，不是问答里的一次修改）
// 与 UpdateVariantCost（改成本价会连锁影响毛利报表与定价规则）。
type VariantWriter interface {
	CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (*productdto.VariantResp, error)
	UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (*productdto.VariantResp, error)
	DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) error
}

// VariantWriteTools 返回变体写工具集。
func VariantWriteTools(w VariantWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("productmcp: 变体写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{variantCreate(w), variantUpdate(w), variantDelete(w)}, nil
}

func variantCreate(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_create", "新增商品变体（SKU）",
		"给某个商品加一个变体（一个可独立售卖、独立库存的 SKU）。\n"+
			"**金额单位是元**（不是分）—— 99 元传 99，不要传 9900。\n"+
			"价格不传就继承商品主价格；库存 quantity 不传 = **不跟踪数量（可无限售出）**，"+
			"传 0 是「暂时没货」，两者不是一回事，别用 0 表示「无限」。\n"+
			"warehouseId 决定这个 SKU 的库存挂在哪个仓；不传用工程默认仓。"+
			"无论选没选，都会在归属仓生成一条库存记录（初始 0）。\n"+
			"productId 用 product_find 拿；该商品已被删除时不能加变体。",
		permission.ProductVariantCreate,
		mcp.Object("新增变体参数", map[string]mcp.Schema{
			"productId":    mcp.String("所属商品 id（用 product_find 拿）"),
			"projectId":    mcp.String("工程 id（可选；不传由后端按唯一工程兜底）"),
			"skuCode":      mcp.String("SKU 编码（可选；留空则由后端生成）"),
			"barcode":      mcp.String("条码（可选）"),
			"price":        money("售价，**单位：元**（可选；不传继承商品主价格），可带小数"),
			"comparePrice": money("划线价，**单位：元**（可选；要比售价大才有意义），可带小数"),
			"costPrice":    money("成本价，**单位：元**（可选），可带小数"),
			"image":        mcp.String("变体图 URL（可选）"),
			"enabled":      mcp.Boolean("是否可售（可选；不传按可售处理）"),
			"sort":         mcp.Integer("排序值（可选，越小越靠前）"),
			"warehouseId":  mcp.String("归属仓库 id（可选；不传用工程默认仓）"),
			"quantity":     mcp.Integer("库存数量（可选；**不传 = 不跟踪数量（无限）**，0 是「没货」）"),
		}, "productId"),
		nil,
		func(ctx context.Context, args variantCreateArgs) (mcp.Result, error) {
			req := &productdto.CreateVariantReq{
				ProductID:   strings.TrimSpace(args.ProductID),
				ProjectID:   strings.TrimSpace(args.ProjectID),
				SKUCode:     strings.TrimSpace(args.SKUCode),
				Barcode:     strings.TrimSpace(args.Barcode),
				Image:       strings.TrimSpace(args.Image),
				Sort:        args.Sort,
				WarehouseID: strings.TrimSpace(args.WarehouseID),
				Quantity:    args.Quantity,
			}
			req.Price = args.Price
			req.ComparePrice = args.ComparePrice
			req.CostPrice = args.CostPrice
			if args.Enabled != nil {
				b := *args.Enabled
				req.Enabled = &b
			}
			res, err := w.CreateVariant(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: variantCreatedText(res, args.Quantity)}, nil
		})
}

type variantCreateArgs struct {
	ProductID    string   `json:"productId"`
	ProjectID    string   `json:"projectId"`
	SKUCode      string   `json:"skuCode"`
	Barcode      string   `json:"barcode"`
	Price        *float64 `json:"price"`
	ComparePrice *float64 `json:"comparePrice"`
	CostPrice    *float64 `json:"costPrice"`
	Image        string   `json:"image"`
	Enabled      *bool    `json:"enabled"`
	Sort         int      `json:"sort"`
	WarehouseID  string   `json:"warehouseId"`
	Quantity     *int     `json:"quantity"`
}

func variantCreatedText(res *productdto.VariantResp, qty *int) string {
	if res == nil {
		return "变体已新增。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已给商品 %s 新增变体（id=%s，SKU %s，售价 %s）。",
		res.ProductID, res.ID, emptyAsDash(res.SKUCode), formatPrice(res.Price))
	if !res.Enabled {
		b.WriteString("\n**它当前是「不可售」状态** —— 前台看不到、买不了。要开卖得改成可售。")
	}
	b.WriteString("\n" + quantityLine(res, qty))
	return b.String()
}

// quantityLine 说清这个变体到底怎么管库存。
//
// create 的 quantity 是**可空**的：不给 = 不跟踪 = 无限售出，给 0 = 没货。
// 这两种在数据库里是同一个 0（库存行数量），只有在「刚新增、且没显式给数量」时
// 才能靠入参区分，所以这句提示只能在这里说 —— 事后再看库存页是看不出来的。
func quantityLine(res *productdto.VariantResp, qty *int) string {
	if qty == nil {
		return "库存：**没有跟踪数量**，可以无限售出。要按实际库存卖，去库存页把它改成跟踪数量。"
	}
	if *qty == 0 {
		return "库存：0 —— 这个 SKU 现在**卖不出去**（不是无限，是没货）。"
	}
	return fmt.Sprintf("库存：已设为 %d，可在库存页加减。", *qty)
}

type variantUpdateArgs struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"projectId"`
	SKUCode      *string  `json:"skuCode"`
	Barcode      *string  `json:"barcode"`
	Price        *float64 `json:"price"`
	ComparePrice *float64 `json:"comparePrice"`
	CostPrice    *float64 `json:"costPrice"`
	Image        *string  `json:"image"`
	Enabled      *bool    `json:"enabled"`
	Sort         *int64   `json:"sort"`
}

func variantUpdate(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_update", "修改商品变体（SKU）",
		"改一个变体的价格、SKU 编码、条码、图、可售状态或排序。\n"+
			"**只改你传的字段**：没传的保持原样。所以「把价格改成 0」要传 price=0，"+
			"而「不动价格」就整个不传 price —— 两者结果不同。\n"+
			"**金额单位是元**（不是分）—— 99 元传 99。\n"+
			"改 skuCode 会影响所有引用这个编码的地方（对账、外部同步）；"+
			"除非用户明确要改编码，否则别动它。\n"+
			"enabled=false 会让它立刻从前台下架（已经下过的单不受影响）。",
		permission.ProductVariantUpdate,
		mcp.Object("修改变体参数", map[string]mcp.Schema{
			"id":           mcp.String("变体 id（用 product_get 看商品的变体列表拿）"),
			"projectId":    mcp.String("工程 id（可选）"),
			"skuCode":      mcp.String("新 SKU 编码（可选；**改动影响面大，非必要别动**）"),
			"barcode":      mcp.String("新条码（可选）"),
			"price":        money("新售价，**单位：元**（可选；改 0 就是免费，慎重），可带小数"),
			"comparePrice": money("新划线价，**单位：元**（可选），可带小数"),
			"costPrice":    money("新成本价，**单位：元**（可选；影响毛利统计），可带小数"),
			"image":        mcp.String("新变体图 URL（可选）"),
			"enabled":      mcp.Boolean("是否可售（可选；false = 立即下架）"),
			"sort":         mcp.Integer("新排序值（可选，越小越靠前）"),
		}, "id"),
		nil,
		func(ctx context.Context, args variantUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateVariantReq{
				ID:        strings.TrimSpace(args.ID),
				ProjectID: strings.TrimSpace(args.ProjectID),
				SKUCode:   args.SKUCode,
				Barcode:   args.Barcode,
				Image:     args.Image,
				Enabled:   args.Enabled,
			}
			req.Price = args.Price
			req.ComparePrice = args.ComparePrice
			req.CostPrice = args.CostPrice
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.SKUCode == nil && req.Barcode == nil && req.Image == nil &&
				req.Enabled == nil && req.Price == nil && req.ComparePrice == nil &&
				req.CostPrice == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateVariant(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: variantUpdatedText(res)}, nil
		})
}

// variantUpdatedText 报「改完长什么样」而不是「改成功了」。
//
// 因为「只改传了的字段」这个语义下，用户最想知道的是**现在**的值：
// 说「已修改价格」但不说改成多少，用户还得再去翻一遍。
func variantUpdatedText(res *productdto.VariantResp) string {
	if res == nil {
		return "变体已修改。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "变体 %s（SKU %s）已更新。现在：售价 %s",
		res.ID, emptyAsDash(res.SKUCode), formatPrice(res.Price))
	if res.ComparePrice != nil {
		fmt.Fprintf(&b, "，划线价 %s", formatPrice(*res.ComparePrice))
	}
	if res.CostPrice != nil {
		fmt.Fprintf(&b, "，成本价 %s", formatPrice(*res.CostPrice))
	}
	if res.Enabled {
		b.WriteString("，可售。")
	} else {
		b.WriteString("，**不可售**（前台看不到）。")
	}
	return b.String()
}

type variantDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func variantDelete(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_delete", "删除商品变体（SKU）",
		"删除一个变体。**它会立刻从前台消失**，且这个 SKU 的库存行一并作废。\n"+
			"历史订单里已经卖出去的那几单不会变（订单存的是当时的快照），"+
			"但之后再想按这个 SKU 对账就找不到了。\n"+
			"**如果只是不想再卖，用 variant_update 把 enabled 设成 false 更合适** ——"+
			"下架可逆，删除不可逆。用户说「下架」「先别卖」时不要调这个工具。\n"+
			"商品只剩一个变体时删除它，会让这个商品在前台没有可买的东西。",
		permission.ProductVariantDelete,
		mcp.Object("删除变体参数", map[string]mcp.Schema{
			"id":        mcp.String("要删除的变体 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args variantDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteVariant(ctx, &productdto.DeleteVariantReq{
				ID:        strings.TrimSpace(args.ID),
				ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"变体 %s 已删除，它已从前台消失，库存行一并作废。"+
					"如果其实只是想停止售卖，下次可以用 variant_update 下架（可逆）。", args.ID)}, nil
		})
}

// money 构造一个「元」金额字段。
//
// 用 number 而不是 integer：商品价格本来就有 9.9 / 99.5 这种小数，
// 用 integer 会在校验层直接拒掉它们（模型只能取整，等于悄悄改了用户报的价）。
func money(desc string) mcp.Schema {
	return mcp.Schema{Type: "number", Description: desc}
}

// 空值展示用短横线：JSON 里 "" 与「这个字段没有」在阅读上没区别，
// 而在变体上「SKU 为空」往往是后端自动生成的，得让人看出「这里本来就没填」。
func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
