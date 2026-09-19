package contenttemplatehttp

// content_template_impact.go — 引用影响面的页面投影（列表页的「引用」列与状态提示）。
//
// 分工：数据的取回与聚合在 contenttemplate service（Impact），这里只把它翻译成
// 模板能直接渲染的形状，并把「查不出来 / 可能不完整」这两种状态说清楚 ——
// 把「没有引用」和「查不出引用」显示成同一个空白列表，是这一页最危险的误导。

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

// contentTemplatePageRef 页面引用的一行。
//
// 用具体结构体而不是 gin.H：模板按字段取值时类型是确定的 —— map[string]interface{}
// 的取值在 Jet 里要经过一层接口解包，任何一处拿不准都会表现为「那一行之后的 HTML
// 整块消失（HTTP 仍 200）」这种极难定位的现象。
type contentTemplatePageRef struct {
	Label string
	Path  string
	Slots string
	URL   string
}

// contentTemplateInstanceRef 实例引用的一行。
type contentTemplateInstanceRef struct {
	Label  string
	Entity string
	Path   string
	Slots  string
}

// contentTemplateImpactRow 一套模板的引用投影（模板渲染直接消费）。
//
// 计数单独给（RefPageCount / RefInstanceCount / RefCount）：模板里不再对切片调 len()，
// 少一层「模板运行时才知道类型对不对」的风险。
type contentTemplateImpactRow struct {
	Pages     []contentTemplatePageRef
	Instances []contentTemplateInstanceRef
	Count     int
}

// contentTemplateRefsByTemplate 把一次反查的结果按模板归集。
//
// 一次扫描服务整页：列表页每套模板都要显示「引用 N 处」，逐个模板各扫一遍页面与实例
// 文档就是 N 次全表扫描（页面一多这一页就点不动）。
func contentTemplateRefsByTemplate(impact *contenttemplatedto.ImpactResp) map[string]*contentTemplateImpactRow {
	out := map[string]*contentTemplateImpactRow{}
	if impact == nil {
		return out
	}
	for _, ref := range impact.References {
		row := out[ref.TemplateID]
		if row == nil {
			row = &contentTemplateImpactRow{Pages: []contentTemplatePageRef{}, Instances: []contentTemplateInstanceRef{}}
			out[ref.TemplateID] = row
		}
		slots := contentTemplateSlotsLabel(ref.Slots)
		switch ref.Kind {
		case contenttemplatedto.ReferenceKindPage:
			label := strings.TrimSpace(ref.PageTitle)
			if label == "" {
				label = strings.TrimSpace(ref.PagePath)
			}
			if label == "" {
				label = ref.PageID
			}
			row.Pages = append(row.Pages, contentTemplatePageRef{
				Label: label, Path: strings.TrimSpace(ref.PagePath), Slots: slots,
				// 页面给可视化编辑入口：引用明细要能「顺着点过去改」，否则它只是好看。
				URL: "/workbench?id=" + url.QueryEscape(ref.PageID),
			})
		case contenttemplatedto.ReferenceKindInstance:
			label := strings.TrimSpace(ref.URLPath)
			if label == "" {
				label = strings.TrimSpace(ref.EntityType + " " + ref.EntityID)
			}
			row.Instances = append(row.Instances, contentTemplateInstanceRef{
				Label: label, Entity: strings.TrimSpace(ref.EntityType + " " + ref.EntityID),
				Path: strings.TrimSpace(ref.URLPath), Slots: slots,
			})
		}
	}
	for _, row := range out {
		row.Count = len(row.Pages) + len(row.Instances)
	}
	return out
}

// contentTemplateSlotsLabel 槽位名 → 面向运营的说法（排序后拼接；空列表返回空串）。
//
// 判据复用 builder 的槽位常量而不是就地写 "header"/"footer"：构建期就是按这两个
// 常量合并绑定的，写死字符串的话槽位改名只会在这一页静默失效（引用列少一半）。
func contentTemplateSlotsLabel(slots []string) string {
	if len(slots) == 0 {
		return ""
	}
	labels := make([]string, 0, len(slots))
	for _, slot := range slots {
		switch slot {
		case builder.SlotHeader:
			labels = append(labels, "页眉")
		case builder.SlotFooter:
			labels = append(labels, "页脚")
		default:
			labels = append(labels, slot)
		}
	}
	sort.Strings(labels)
	return strings.Join(labels, " / ")
}

// contentTemplateTypeLabel 实体类型 → 面向运营的说法（结构模板与内容实体模板要一眼分得开）。
func contentTemplateTypeLabel(entityType string) string {
	switch entityType {
	case "header":
		return "页眉（结构）"
	case "footer":
		return "页脚（结构）"
	case "product":
		return "商品详情"
	case "article":
		return "文章详情"
	case "category":
		return "分类归档"
	case "tag":
		return "标签归档"
	case "brand":
		return "品牌归档"
	default:
		return entityType
	}
}

// contentTemplateRoleLabel 模板角色 → 面向运营的说法。
func contentTemplateRoleLabel(role string) string {
	switch role {
	case "archive":
		return "归档页"
	case "", "detail":
		return "详情页"
	default:
		return role
	}
}

// contentTemplateImpactNote 影响面状态提示（空串 = 无需提示）。
//
// 三种状态各说各话，不能合并：
//
//	· 读取失败 —— 归口文案（错误原文只进日志，见 content_template_err.go）；
//	· 端口未装配 —— 「查不出来」，不是「没有引用」；
//	· 有文档解析不了 —— 影响面可能不完整（>0 的计数必须显示出来）。
func contentTemplateImpactNote(c *gin.Context, impact *contenttemplatedto.ImpactResp, err error) string {
	if err != nil {
		return contentTemplateInternalText(c, err)
	}
	if impact == nil || !impact.Available {
		return contentTemplateImpactUnavailableText
	}
	if impact.Unparsable > 0 {
		return fmt.Sprintf(contentTemplateImpactUnparsableTemplate, impact.Unparsable)
	}
	return ""
}
