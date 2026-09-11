// product_attribute_values.go — 属性值的序列化与「稳定标识 + 排序」（issue #7）。
//
// 语义（spec §属性组）：
//
//	· 值放在 product_attributes.values 的 JSONB 数组里，不做独立表；
//	· 每个值有 ID（关联引用）与 Key（渲染 / 规格组合用）两个稳定标识，
//	  改名（Label）不动它们 —— 否则「改了显示名，已生成的 SKU 组合就全对不上」；
//	· 排序由数组顺序承担：写入前按 Sort 稳定排序（同值保持原有相对顺序），
//	  保证「同一次提交产生确定的数组顺序」，进而保证构建期输出确定（不变量 5）。
package productservice

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
)

// valueSortBase 未显式给 Sort 时的排序基数（第 n 个值 → n*10，留出插入空隙）。
const valueSortBase = 10

// sanitizeAttributeValues 归一 + 去重 + 排序，得到可落库的稳定值列表。
//
// 规则：
//  1. 丢弃空 label 的条目（表单空行）；
//  2. 无 ID 的补 uuid，无 Key 的由 label 派生 / 兜底生成；
//  3. 同一组内 ID 与 Key 各自唯一 —— 撞了则重新生成而不是报错（幂等收敛）；
//  4. 未显式给 Sort 的按下标补位；
//  5. 按 Sort 稳定排序后重排（同 Sort 保持调用方给的相对顺序）。
func sanitizeAttributeValues(reqs []productdto.AttributeValueReq) []productdto.AttributeValueResp {
	out := make([]productdto.AttributeValueResp, 0, len(reqs))
	seenID := map[string]bool{}
	seenKey := map[string]bool{}
	for i, r := range reqs {
		label := strings.TrimSpace(r.Label)
		if label == "" {
			continue
		}
		id := strings.TrimSpace(r.ID)
		if id == "" || seenID[id] {
			id = uuid.NewString()
		}
		seenID[id] = true

		key := normalizeAttrToken(r.Key)
		if key == "" {
			key = normalizeAttrToken(label)
		}
		// 纯中文 label 派生不出 key 时兜底为 v1/v2…（用位置而非随机，
		// 让「同一次提交两次执行得到同一结果」成立）。
		if key == "" {
			key = "v" + itoa(i+1)
		}
		if seenKey[key] {
			key = key + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:4]
		}
		seenKey[key] = true

		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		sortV := r.Sort
		if sortV == 0 {
			sortV = (i + 1) * valueSortBase
		}
		out = append(out, productdto.AttributeValueResp{
			ID: id, Key: key, Label: label, Sort: sortV, Enabled: enabled,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out
}

// normalizeValuesFromRaw 读取 sides 的 values 列 → 结构化的值列表。
//
// 兜底两条历史形态（不报错，读出即可用）：
//   - 旧版纯字符串数组 ["红","蓝"] → 逐个补 id/key/sort/enabled；
//   - null / 非法 JSON → 空列表。
func normalizeValuesFromRaw(raw json.RawMessage) []productdto.AttributeValueResp {
	if len(raw) == 0 {
		return []productdto.AttributeValueResp{}
	}
	var objs []productdto.AttributeValueResp
	if err := json.Unmarshal(raw, &objs); err == nil {
		// 结构对得上：仍然走一次 sanitize，保证「库里怎么存都不影响读出来的一致形态」。
		reqs := make([]productdto.AttributeValueReq, 0, len(objs))
		for _, o := range objs {
			enabled := o.Enabled
			reqs = append(reqs, productdto.AttributeValueReq{
				ID: o.ID, Key: o.Key, Label: o.Label, Sort: o.Sort, Enabled: &enabled,
			})
		}
		return sanitizeAttributeValues(reqs)
	}
	var labels []string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return []productdto.AttributeValueResp{}
	}
	reqs := make([]productdto.AttributeValueReq, 0, len(labels))
	for _, l := range labels {
		reqs = append(reqs, productdto.AttributeValueReq{Label: l})
	}
	return sanitizeAttributeValues(reqs)
}

// encodeAttributeValues 值列表 → jsonb 原始字节（落库用）。
func encodeAttributeValues(values []productdto.AttributeValueResp) json.RawMessage {
	if values == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// normalizeAttrToken 把任意文本归一为「稳定标识片段」：小写字母数字 + 连字符。
//
// 复用商品 slug 的派生规则（deriveSlug，product_service.go）：全仓只有一份
// 「作者文本 → URL/标识片段」的实现，避免属性 key 与商品 slug 出现两套规则。
func normalizeAttrToken(s string) string {
	return deriveSlug(strings.TrimSpace(s))
}

// itoa 小整数转字符串（避免为一个位置编号引入 strconv 依赖）。
func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
