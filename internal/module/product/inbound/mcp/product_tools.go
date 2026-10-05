package productmcp

// product_tools.go — 商品模块暴露给模型的工具。
//
// 与内容模块同构：一个读 + 三个写，写工具走 mcp.NewWrite（确认位 + 幂等键）。
// 只覆盖**商品主体**（products 一行）。属性 / 品牌 / 分类 / 捆绑配置各自是独立的数据形态，
// 混进同一批会让「商品」这个词在工具列表里指五样东西 —— 模型分不清该用哪个，
// 而分不清的代价是它挑一个看起来最像的调下去。
//
// **依赖断言成收窄的端口**而不是整个 ProductService：后者同时握着
// SetBundleConfig / UpdateVariantCost / CreateAttribute，工具层握着它们时
// 「顺手调个价」会从「显式加一个工具」退化成「随手就能做」。

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/permission"
)

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
