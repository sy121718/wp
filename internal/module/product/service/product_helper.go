package productservice

// product_helper.go — 分页、slug 归一化与 JSON 投影小工具。

import (
	"encoding/json"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

// pageArgs 归一化分页参数。
func pageArgs(req *productdto.ListReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
	return paging.Page, paging.Size
}

// optionalPaging 归一「可选分页」参数，返回 model 层接受的 (limit, offset)。
//
// 与 pageArgs 的关键差别：**两个入参都是零值时返回 (0, 0) = 不分页**。
// 品牌 / 标签的 Page、Size 是后加的可选字段，同一个请求类型上并存两种调用形态：
//
//	· 显式分页 —— 后台列表页给出 Page/Size，要的是「当页 + 总数」；
//	· 全量取数 —— 集合源筛选选项、内容翻译候选、规则重算的取数来源都传零值，
//	  它们要的是全部行。这里若按 pageArgs 那样兜底成「第 1 页 20 条」，
//	  表现为品牌筛选下拉少了一批选项、重算回执少了一批标签 —— 不报错、只是少了。
//
// 所以「分页」必须是调用方的显式意图，而不是模型层的默认行为。
func optionalPaging(page, size int) (limit, offset int) {
	if page <= 0 && size <= 0 {
		return 0, 0
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return size, (page - 1) * size
}

// normalizeSlug 规范化传入 slug（小写 + 去首尾空白）。
func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "-")
	return strings.ToLower(s)
}

// deriveSlug 由商品名派生 slug：保留字母数字、其余转连字符；中文名派生为空，由调用方兜底。
func deriveSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

// orJSONList 字符串数组转 jsonb（空数组写 []）。
func orJSONList(items []string) json.RawMessage {
	if items == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// orIDList id 数组转 jsonb。
func orIDList(items []string) json.RawMessage { return orJSONList(items) }

// decodeStrings jsonb 数组 → 字符串切片（失败返回空切片，不阻断读取）。
// mediaURL 媒体地址 → 对外可用的完整链接。
//
// 商品域里凡是「取自媒体库」的字段（images / defaultImage / 变体 image /
// 分类 image / 品牌 logo）都要过它：媒体库给的是 upload.base_url 前缀下的地址，
// 未配置时是 "/storage/<id>.<ext>"。写入口归一（新数据天然完整），读出口再归一
// （存量相对值也显示对）—— 两侧共用 pkg/upload 的同一个实现，避免各写一份前缀拼接。
//
// 非媒体地址（外链 CDN / data: URI）由 StorageURL 原样返回，不会被改写。
func mediaURL(u string) string { return upload.StorageURL(u) }

// mediaURLs 逐个归一图片数组；空数组返回空数组（不是 nil），
// 免得调用方在 JSON 里拿到 null 又要各自兜一次。
func mediaURLs(list []string) []string {
	out := make([]string, 0, len(list))
	for _, u := range list {
		if s := mediaURL(u); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func decodeStrings(raw json.RawMessage) (out []string) {
	out = []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}
