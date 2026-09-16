// product_variant_generate.go — 变体的笛卡尔积自动生成与默认值继承（issue #8）。
//
// 本文件落地本票四条验收：
//  1. 勾选若干属性值后生成全部组合 —— 对「参与变体」的属性组做笛卡尔积；
//     已存在的规格组合（含同一次请求里重复勾选的值）直接跳过，不产生重复变体；
//  2. 维度与数量上限保护 —— 参与维度或组合总数超上限时**整体拒绝**（一条都不写），
//     错误消息带上限与实际值；
//  3. 新变体逐字段继承商品级默认值 —— 生成路径复用 newVariantFromDefaults
//     （唯一填充入口：只填空字段，「空」以 NULL 判定，0 与 false 视为已填）；
//  4. 无表单路径同样走判空继承 —— 不传勾选（批量生成 / 导入 / 接口）时按
//     「全部参与变体的属性组 × 全部启用值」生成，填充规则与表单路径一字不差。
//
// 组合的载体（商品恒有至少一个变体，见 product_service.go）：商品创建时生成的
// 首个变体没有规格（option_values = {}）。生成组合时它**就地承接第一个组合**
// （只补 option_values，已填字段一个都不覆盖），其余组合新建 —— 否则商品会多出
// 一条无规格的悬挂变体，前台规格选择器也随之多出一行。
//
// 本文件是纯业务编排：组合算法与归一在此，持久化只走 model 的具名方法。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

const (
	// MaxVariationDimensions 参与笛卡尔积的属性组数量上限（维度保护）。
	MaxVariationDimensions = 4
	// MaxVariantCombinations 单次生成的组合总数上限（数量保护）。
	MaxVariantCombinations = 200
)

// optionPair 组合里的一个「属性组 key → 属性值 key」。
type optionPair struct {
	Key   string
	Value string
}

// variationDimension 一个参与组合的维度：属性组 + 本次实际使用的值。
type variationDimension struct {
	group  *productmodel.ProductAttributeEntity
	values []productdto.AttributeValueResp
}

// GenerateVariants 按勾选的属性值生成全部变体组合（幂等：已存在的组合跳过）。
func (s *Service) GenerateVariants(ctx context.Context, req *productdto.GenerateVariantsReq) (res *productdto.GenerateVariantsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs))
	if aerr != nil {
		return nil, aerr
	}
	dims, derr := buildVariationDimensions(p, attrs, req.Selections)
	if derr != nil {
		return nil, derr
	}
	total := combinationCount(dims)
	if total > MaxVariantCombinations {
		return nil, fmt.Errorf("%s：%d 个组合超过上限 %d（请减少勾选的属性值或属性维度）",
			productenums.ErrVariationCountLimit, total, MaxVariantCombinations)
	}

	// 归属仓（issue #15）：本批变体统一落在该仓（不选则默认仓），短码参与 SKU 编码，
	// 落库后在同一个仓为每个新建变体生成初始 0 的库存记录。解析失败即整体拒绝。
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	now := time.Now().UTC()
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	// 已有的规格组合（option_values 非空）与服务层已占用的 SKU 编码：
	// 组合去重与 SKU 唯一都在内存里判定，落库时一次性写。
	taken := map[string]bool{}
	skus := map[string]bool{}
	var carrier *productmodel.VariantEntity
	for _, v := range existing {
		if v.SKUCode != "" {
			skus[v.SKUCode] = true
		}
		pairs := decodeOptionPairs(v.OptionValues)
		if len(pairs) == 0 {
			// 无规格变体（商品创建时的首个变体）：取最早的一条作为组合载体。
			if carrier == nil || v.CreatedAt.Before(carrier.CreatedAt) {
				carrier = v
			}
			continue
		}
		taken[optionKey(pairs)] = true
	}

	res = &productdto.GenerateVariantsResp{
		ProductID: p.ID, Total: total, Variants: []*productdto.VariantResp{},
	}
	var updated, created []*productmodel.VariantEntity
	// issue #19：组合载体（无规格变体被首个组合承接）改的是同一个变体的 option_values，
	// 改前快照必须先取 —— 它随后会被就地改写。载体至多一个，故一份快照即可。
	var carrierBefore masterdatacontract.FieldSnapshot
	if carrier != nil {
		carrierBefore = variantChangeSnapshot(carrier, nil)
	}
	for i, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if taken[key] {
			// 重复勾选 / 重复提交：组合已存在，原样跳过。
			res.Skipped++
			continue
		}
		taken[key] = true
		raw := encodeOptionPairs(pairs)
		if carrier != nil {
			carrier.OptionValues = raw
			carrier.UpdatedAt = now
			updated = append(updated, carrier)
			// 载体只承接一个组合，后续组合一律新建。
			carrier = nil
			res.Adopted++
			continue
		}
		// 本批的 SKU 编码逐个探测：taken 传入已占用的编码集合，
		// 否则同一批里的新变体都会取到同一个「下一个序号」而互相撞号。
		v := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw, Sort: i,
		}, refCode(ref), skus)
		skus[v.SKUCode] = true
		created = append(created, v)
	}
	if err = s.m.SaveVariants(ctx, updated, created); err != nil {
		return nil, err
	}
	// 验收 2：每个新建变体都在归属仓有一条库存记录（初始 0）——
	// 商品创建时的首个变体已在此路径外生成过，这里补的是本批新组合。
	for _, v := range created {
		if err = s.ensureVariantStock(ctx, ref, p.ID, v.ID, v.SKUCode); err != nil {
			return nil, err
		}
	}
	// issue #19：本批新增的变体逐条留痕（含默认发货仓与 SKU 编码）；
	// 被承接的载体记一条修改记录（规格组合由空变为具体组合）。
	changeInputs := make([]*masterdatacontract.ChangeInput, 0, len(created)+len(updated))
	for _, v := range updated {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, carrierBefore, variantChangeSnapshot(v, nil)))
	}
	for _, v := range created {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
	}
	if err = s.recordChanges(ctx, changeInputs...); err != nil {
		return nil, err
	}
	res.Created = len(created)
	// 重算时机之一：变体写操作后 —— 组合生成会新建一整批变体，价格整体变化。
	if err = s.recalcProjectAutoTags(ctx, p.ID, p.ProjectID); err != nil {
		return nil, err
	}

	after, rerr := s.m.ListVariants(ctx, p.ID)
	if rerr != nil {
		return nil, rerr
	}
	for _, v := range after {
		res.Variants = append(res.Variants, toVariantResp(v))
	}
	// 库存展示值查询期投影（issue #32）。
	s.fillVariantStock(ctx, p.ProjectID, res.Variants)
	return res, nil
}

// buildVariationDimensions 组装本次参与组合的维度（顺序 = 商品引用属性组的顺序）。
//
// 规则见 GenerateVariantsReq 注释。勾选里出现非法引用一律报错而不是静默忽略 ——
// 静默忽略会让调用方以为「已经生成」，实际什么都没生成。
func buildVariationDimensions(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity, selections []productdto.VariantSelectionReq) (dims []variationDimension, err error) {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	picked := map[string][]string{}
	seenSel := map[string]bool{}
	for _, sel := range selections {
		id := strings.TrimSpace(sel.AttributeID)
		if id == "" {
			continue
		}
		picked[id] = append(picked[id], sel.ValueIDs...)
		if seenSel[id] {
			continue
		}
		seenSel[id] = true
		if a, ok := byID[id]; !ok || !a.IsVariation {
			return nil, fmt.Errorf("%s：属性组 %s 未被该商品引用，或未标记为参与变体",
				productenums.ErrVariationAttributeInvalid, id)
		}
	}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		enabled := enabledAttributeValues(a)
		if len(enabled) == 0 {
			// 组内没有启用的值：参与不了组合，跳过（不是错误）。
			continue
		}
		wanted, hit := picked[id]
		if !hit {
			// 这次没勾这个组 = 该维度取全部启用值（不缩小范围）。
			// 语义上等价于「无表单路径」，但即使其它组被勾选也保持一致 ——
			// 生成的组合恒覆盖全部参与变体的维度，不会产出只有部分维度的
			// 「半截组合」（那种组合在前台规格选择器里根本选不到）。
			dims = append(dims, variationDimension{group: a, values: enabled})
			continue
		}
		chosen, cerr := pickAttributeValues(a, enabled, wanted)
		if cerr != nil {
			return nil, cerr
		}
		if len(chosen) == 0 {
			continue
		}
		dims = append(dims, variationDimension{group: a, values: chosen})
	}
	if len(dims) == 0 {
		return nil, errors.New(productenums.ErrVariationNoDimension)
	}
	if len(dims) > MaxVariationDimensions {
		return nil, fmt.Errorf("%s：%d 个维度超过上限 %d（请减少参与变体的属性组）",
			productenums.ErrVariationDimensionLimit, len(dims), MaxVariationDimensions)
	}
	return dims, nil
}

// enabledAttributeValues 组内启用的值（按组定义顺序；历史纯字符串数组由归一兜底）。
func enabledAttributeValues(a *productmodel.ProductAttributeEntity) (out []productdto.AttributeValueResp) {
	out = []productdto.AttributeValueResp{}
	for _, v := range normalizeValuesFromRaw(a.Values) {
		if v.Enabled {
			out = append(out, v)
		}
	}
	return out
}

// pickAttributeValues 从启用值里挑出被勾选的那些（去重 + 保持组内定义顺序）。
//
// 勾选到组内不存在或已停用的值时报错（不静默丢弃：调用方需要知道哪一条没生效）。
func pickAttributeValues(a *productmodel.ProductAttributeEntity, enabled []productdto.AttributeValueResp, wanted []string) (out []productdto.AttributeValueResp, err error) {
	enabledSet := make(map[string]bool, len(enabled))
	for _, v := range enabled {
		enabledSet[v.ID] = true
	}
	want := map[string]bool{}
	for _, id := range wanted {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !enabledSet[id] {
			return nil, fmt.Errorf("%s：属性组 %s 下没有启用中的属性值 %s",
				productenums.ErrVariationValueInvalid, a.Key, id)
		}
		want[id] = true
	}
	out = []productdto.AttributeValueResp{}
	for _, v := range enabled {
		if want[v.ID] {
			out = append(out, v)
		}
	}
	return out, nil
}

// combinationCount 组合总数（各维度取值数之积）。
func combinationCount(dims []variationDimension) int {
	total := 1
	for _, d := range dims {
		total *= len(d.values)
	}
	return total
}

// expandCombinations 展开笛卡尔积：最后一维变化最快（与常见规格表的阅读顺序一致）。
func expandCombinations(dims []variationDimension) (out [][]optionPair) {
	if len(dims) == 0 {
		return nil
	}
	idx := make([]int, len(dims))
	for {
		pairs := make([]optionPair, len(dims))
		for i, d := range dims {
			pairs[i] = optionPair{Key: d.group.Key, Value: d.values[idx[i]].Key}
		}
		out = append(out, pairs)
		i := len(dims) - 1
		for i >= 0 {
			idx[i]++
			if idx[i] < len(dims[i].values) {
				break
			}
			idx[i] = 0
			i--
		}
		if i < 0 {
			break
		}
	}
	return out
}

// decodeOptionPairs 读变体的 option_values（jsonb 对象）为组合对。
//
// 只接受「字符串 → 字符串」的对象形态；空对象、数组、字符串等历史形态一律视为
// 「无规格」（返回空），由生成逻辑决定是否把它当作组合载体。
func decodeOptionPairs(raw json.RawMessage) []optionPair {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	out := make([]optionPair, 0, len(m))
	for k, v := range m {
		out = append(out, optionPair{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// optionKey 组合的规范化键（按属性组 key 排序）。
//
// 排序保证「写入顺序不同的同一条组合」判定为同一个键 —— 这是幂等去重的依据。
func optionKey(pairs []optionPair) string {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	var b strings.Builder
	for i, p := range sorted {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	return b.String()
}

// encodeOptionPairs 组合 → jsonb 原始字节（保持维度顺序：落库与展示都确定）。
func encodeOptionPairs(pairs []optionPair) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.Key)
		v, _ := json.Marshal(p.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}
