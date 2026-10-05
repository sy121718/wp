package productmcp

// attribute_write_tools.go — 商品属性组（规格）的读写。
//
// 属性组和标签是两回事，别混：标签是「运营打的标记」（热销 / 清仓），
// 属性组是「商品本身的规格」（颜色 / 尺码 / 容量），它决定商品在前台怎么被筛选。
// 其中一个开关尤其关键 —— IsVariation=true 的属性组是**变体的来源**：
// 它的值会被拿去笛卡尔积出 SKU。改错了会让商品的变体结构对不上。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
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
