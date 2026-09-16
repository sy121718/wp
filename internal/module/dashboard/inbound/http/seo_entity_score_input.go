package dashboardhttp

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/seo/scoring"
)

// seo_entity_score_input.go - 实体评分的输入素材提取（标题、正文纯文本、图片与分类名）。

// pageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
func pageDraftTitle(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}
	var parsed struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title)
}

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// entityPlainText 商品描述（jsonb）→ 纯文本。
//
// 两种形态与构建期一致（{"html": "..."} 富文本 / JSON 字符串纯文本），
// 去标签的理由与文章侧相同：把 <p> 当正文算进字数，一篇 300 字的描述会被算成 900。
func entityPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return stripEntityTags(s)
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return stripEntityTags(asString)
	}
	return ""
}

var entityTagRe = regexp.MustCompile("<[^>]+>")

// stripEntityTags 去 HTML 标签并压掉多余空白（只读展示与统计用，不承担清洗职责）。
func stripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

// productPageImages 商品图集 → 评分图片项（首图按 hero、其余按 content，
// alt 与 Images 逐位对应 —— 与构建期 imageAltsJSON 同一份数据）。
func productPageImages(p *productdto.ProductResp) []scoring.Image {
	if p == nil {
		return nil
	}
	out := make([]scoring.Image, 0, len(p.Images))
	for i, src := range p.Images {
		if strings.TrimSpace(src) == "" {
			continue
		}
		kind := "content"
		if i == 0 {
			kind = "hero"
		}
		alt := ""
		if i < len(p.ImageAlts) {
			alt = strings.TrimSpace(p.ImageAlts[i])
		}
		out = append(out, scoring.Image{Src: strings.TrimSpace(src), Alt: alt, Kind: kind})
	}
	return out
}

// singleImage 单图实体（分类图 / 品牌 logo）→ 评分图片项。
func singleImage(src, kind string) []scoring.Image {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	return []scoring.Image{{Src: strings.TrimSpace(src), Kind: kind}}
}

// variationSpecNames 参与变体的属性组名（详情页把它们渲染成分组小标题）。
func variationSpecNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Attributes))
	for _, a := range p.Attributes {
		if a != nil && a.IsVariation && strings.TrimSpace(a.Name) != "" {
			out = append(out, strings.TrimSpace(a.Name))
		}
	}
	return out
}

// productCategoryNames 商品挂载的分类名（详情页的关联分区标题）。
func productCategoryNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Categories))
	for _, c := range p.Categories {
		if c != nil && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// categoryChildNames 某分类的子分类名（列表页把子分类渲染成小节）。
//
// 取不到（分类不存在 / 列表失败）返回 nil：少一个小标题只会让标题结构项如实报缺，
// 不会让评分整体失败。
func categoryChildNames(ctx context.Context, h *productPageHandle, projectID, id string) []string {
	cats, err := h.flatCategories(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, c := range cats {
		if c != nil && c.ParentID == id && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}

// firstNonEmptyString 取第一个非空值。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
