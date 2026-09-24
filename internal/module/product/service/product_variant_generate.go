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
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
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
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
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

	// 容器主体 SKU（规则 A 的主体段）：与「保存清单」路径共用同一条解析
	//（预览、生成、保存三处的容器段必须逐字一致，否则同一组合会给出两种 SKU）。
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	// 属性段的固定顺序：按商品引用属性组的先后（与点选顺序无关）。
	groupOrder := attributeGroupOrder(p, attrs)

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
		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw, Sort: i,
		}, refCode(ref), skus)
		if verr != nil {
			return nil, verr
		}
		// 规则 A：变体 SKU = <容器主体 SKU>_<属性值段…>_V。
		// 属性段顺序固定（按属性组的既定顺序），因此同一组合无论怎么点选都是同一个 SKU。
		v.SKUCode = uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), skus)
		skus[v.SKUCode] = true
		created = append(created, v)
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
	// 变体清单、各仓库存行、变更记录三处在**同一个事务**里：一次「生成组合」要么整批落库、
	// 要么一行都不落 —— 半截状态会让前台的规格选择器少行（AGENTS.md「写操作的事务与回滚」）。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}
		// 验收 2：每个新建变体都在归属仓有一条库存记录（初始 0）——
		// 商品创建时的首个变体已在此路径外生成过，这里补的是本批新组合。
		// 数量不填（nil = 不跟踪 = 无限）：这条路径没有数量输入。
		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, p.ID, v.ID, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}
		return s.recordChangesTx(ctx, tx, changeInputs...)
	}); err != nil {
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
			pairs[i] = optionPair{Key: d.group.Key, Value: attributeValueCode(d.values[idx[i]])}
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

// —— SKU 编码新规则：变体 SKU 的拼接（规则 A，2026-09-19）——

// attributeGroupOrder 属性组的固定顺序（属性组 key → 序号）。
//
// 顺序取**商品引用属性组的先后**（products.attribute_ids），这就是「属性组的既定顺序」：
// 与运营点选的先后、与 option_values 的写入顺序都无关。SKU 属性段按它排序，保证
// 「顺序不同但组合相同」得到同一个 SKU。
func attributeGroupOrder(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity) map[string]int {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	order := make(map[string]int, len(attrs))
	next := 0
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := order[a.Key]; dup {
			continue
		}
		order[a.Key] = next
		next++
	}
	// 商品没显式引用的组（理论上不会出现在 option_values 里）排到最后，避免漏项被当成「第 0 位」。
	for _, a := range attrs {
		if _, ok := order[a.Key]; !ok {
			order[a.Key] = next
			next++
		}
	}
	return order
}

// variantSKUCode 变体 SKU = <容器主体 SKU>_<属性值段…>_V（规则 A）。
//
// 属性段用**属性值的标识 key**（不是属性组的 key，也不用中文显示名）；顺序固定为属性组的
// 既定顺序（groupOrder），组内按属性值排序字段 —— 点选顺序不参与，因此同一组合恒得同一 SKU。
// 无规格（option_values 为空）时返回容器主体本身。
func variantSKUCode(container string, optionValues json.RawMessage, groupOrder map[string]int) string {
	pairs := decodeOptionPairs(optionValues)
	if len(pairs) == 0 {
		return container
	}
	pairs = sortedOptionPairs(pairs, groupOrder)
	segments := make([]string, 0, len(pairs))
	for _, p := range pairs {
		segments = append(segments, skuSegment(p.Value))
	}
	return container + skuSeparator + strings.Join(segments, skuSeparator) + VariantSKUSuffix
}

// uniqueVariantSKU 保证变体 SKU 在**商品内**唯一（UNIQUE (product_id, sku_code)）。
//
// 组合去重已保证不同组合得到不同属性段，这里只兜底极端情况（两个值 key 归一化后同段）：
// 在 _V 后缀前插入 _2 / _3 …（不引入随机段，保序且可读）。
func uniqueVariantSKU(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	body := strings.TrimSuffix(base, VariantSKUSuffix)
	for i := 2; i < maxSKUProbe; i++ {
		candidate := fmt.Sprintf("%s%s%d%s", body, skuSeparator, i, VariantSKUSuffix)
		if !taken[candidate] {
			return candidate
		}
	}
	return base
}

// containerSKUOfVariants 存量商品的主体 SKU 回落：取最早创建的那条变体
// （商品创建时的无规格变体，其 SKU 就是当时的容器主体）。
//
// 存量编码**不重写** —— 新生成的变体以它为主体拼接，这是「存量 SKU 一律不重写」的
// 必然结果：主体段保持老编码，只有新增的变体套用新格式。
func containerSKUOfVariants(variants []*productmodel.VariantEntity) string {
	var earliest *productmodel.VariantEntity
	for _, v := range variants {
		if strings.TrimSpace(v.SKUCode) == "" {
			continue
		}
		if earliest == nil || v.CreatedAt.Before(earliest.CreatedAt) ||
			(v.CreatedAt.Equal(earliest.CreatedAt) && v.ID < earliest.ID) {
			earliest = v
		}
	}
	if earliest == nil {
		return ""
	}
	return earliest.SKUCode
}

// attributeValueCode 属性值在规格组合里的编码（写进 option_values 的值）。
//
// 优先用属性值的**标识 key**（key 是标识不是展示文本，筛选按它匹配）；key 为空时用 id 短码
// 兜底 —— 否则空 key 的多条值会在 option_values 里互相覆盖，组合去重也会把它们当成同一个。
// 这里**不做 ASCII 归一化**：option_values 的取值语义必须与筛选口径逐字一致，
// ASCII 化只发生在拼 SKU 段时（skuSegment）。
func attributeValueCode(v productdto.AttributeValueResp) string {
	if code := strings.TrimSpace(v.Key); code != "" {
		return code
	}
	return "v" + shortHash(v.ID)
}

// skuSegment 属性值的 SKU 段：标识 key 是纯 ASCII 时原样用（可读、可与仓库对齐）；
// 否则用确定性短码兜底，保证生成的 SKU **全 ASCII**（中文显示名不进编码）。
func skuSegment(value string) string {
	v := strings.TrimSpace(value)
	if v != "" && isSKUASCII(v) {
		return v
	}
	return "v" + shortHash(v)
}

// isSKUASCII 段是否只含 SKU 允许的 ASCII 字符（字母数字与 . _ -）。
func isSKUASCII(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// shortHash FNV-1a 32 位哈希的 8 位十六进制（跨进程稳定的确定性短码）。
func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// —— 变体清单：预览—保存模型（docs/14 §8，2026-09-19 用户拍板）——
//
// 用户口径：「变体 sku 也是一样系统生产 + 可编辑，生产只是显示，并不会存入数据库，
// 必须保存才行，所以删除不是删除，是相当于清除前端显示，不存入数据库」。
//
// 本段是那句话的落地：
//   · 生成（PreviewVariantCombinations）—— 只算不写，把笛卡尔积按组合去重后交回前端清单；
//   · 删除（前端行为）—— 只是从清单里移出，服务端没有「删一行」的入口；
//   · 保存（SaveVariantList）—— 以清单为准：新增缺失的组合、更新改过的 SKU、
//     删除清单外的既有变体；清单外要删的行若**有非零库存或被 BOM 引用**则跳过，
//     逐条回带原因（不整批失败、不静默）。
//
// 与 GenerateVariants 的关系：后者是「直接落库」的接口路径（导入 / 接口 / 批量生成），
// 语义一字未改；本段服务的是后台抽屉的预览—保存交互。

// PreviewVariantCombinations 组合生成的预览：把勾选值的笛卡尔积与「库里已有的组合 +
// 前端清单已有的组合」去重后返回待追加的行（**一个字节都不写库**）。
//
// 与 GenerateVariants 共用同一套维度归一（buildVariationDimensions）、上限保护、
// 容器主体解析（resolveContainerSKU）与 SKU 拼接（variantSKUCode / uniqueVariantSKU）——
// 预览里看到的 SKU 就是保存后落库的那个 SKU，两处各写一套必然分叉。
func (s *Service) PreviewVariantCombinations(ctx context.Context, req *productdto.PreviewVariantReq) (res *productdto.PreviewVariantResp, err error) {
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
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
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
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)
	// 已占用的 SKU 编码（库里的全部 + 本批已算出的）与已出现过的组合：
	// 前者保证预览里的 SKU 不会撞号，后者保证「重复点生成不重复追加」。
	taken := make(map[string]bool, len(existing))
	seen := map[string]bool{}
	for _, v := range existing {
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			taken[code] = true
		}
		seen[optionKey(decodeOptionPairs(v.OptionValues))] = true
	}
	for _, raw := range req.ExistingOptionValues {
		if len(raw) == 0 {
			continue
		}
		seen[optionKey(decodeOptionPairs(raw))] = true
	}
	res = &productdto.PreviewVariantResp{ProductID: p.ID, Total: total, Rows: []*productdto.VariantPreviewRow{}}
	for _, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if seen[key] {
			// 库里已有该组合（抽屉打开时的初始行就是它）或清单里已有：不重复追加。
			res.Skipped++
			continue
		}
		seen[key] = true
		raw := encodeOptionPairs(pairs)
		code := uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), taken)
		taken[code] = true
		res.Rows = append(res.Rows, &productdto.VariantPreviewRow{
			OptionValues: raw, OptionKey: key, SKUCode: code,
		})
	}
	return res, nil
}

// SaveVariantList 以清单为准保存变体（本流程**唯一的落库动作**）。
//
// 服务端不信任前端提交的清单形状，逐条重算：
//   - 新增行的规格组合必须由服务端校验（属性组属于该商品、取值在组内启用值集合里、
//     键序按属性组固定顺序归一）；既有行则以**库里的 option_values** 为准
//     （历史组合可能引用了后来被删的组 / 值，用前端传的组合判定会让老数据卡住保存）；
//   - SKU 逐段规范化（normalizeVariantSKU），为空或规范化后为空一律报
//     ErrVariantSKUEmpty；唯一性按 product_id + sku_code（前端提交的值不作数）；
//   - 删除清单外的既有变体前先查引用面（四个：非零库存 / BOM 引用 / 捆绑成员引用 /
//     有过库存流水），命中任一即跳过该行并逐条回带原因，不整批失败。
func (s *Service) SaveVariantList(ctx context.Context, req *productdto.SaveVariantListReq) (res *productdto.SaveVariantListResp, err error) {
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
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)
	allowed := variationValueIndex(attrs)

	byID := make(map[string]*productmodel.VariantEntity, len(existing))
	// owner 是 SKU 编码的占用者（变体 id）：清单里两行不能落到同一个编码上。
	owner := make(map[string]string, len(existing))
	for _, v := range existing {
		byID[v.ID] = v
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			owner[code] = v.ID
		}
	}
	now := time.Now().UTC()
	res = &productdto.SaveVariantListResp{ProductID: p.ID, Skipped: []productdto.VariantSaveSkip{}}
	kept := make(map[string]bool, len(req.Rows))
	seenKeys := make(map[string]bool, len(req.Rows))
	var updated, created []*productmodel.VariantEntity
	changes := make([]*masterdatacontract.ChangeInput, 0, len(req.Rows))
	for i, row := range req.Rows {
		if vid := strings.TrimSpace(row.VariantID); vid != "" {
			v := byID[vid]
			if v == nil {
				// 清单里的既有变体已经被别处删掉（并发删除）：跳过并说明，不当成新增行。
				res.Skipped = append(res.Skipped, productdto.VariantSaveSkip{
					VariantID: vid, Reason: productenums.VariantSkipVariantMissing,
				})
				continue
			}
			key := optionKey(decodeOptionPairs(v.OptionValues))
			if seenKeys[key] {
				res.Skipped = append(res.Skipped, variantSkipOf(v, productenums.VariantSkipDuplicated, ""))
				continue
			}
			seenKeys[key] = true
			kept[v.ID] = true
			code := normalizeVariantSKU(row.SKUCode)
			if code == "" {
				return nil, errors.New(productenums.ErrVariantSKUEmpty)
			}
			if code == v.SKUCode {
				// 没改 SKU：这一行一个字都不写（也不留痕）。
				continue
			}
			if id, taken := owner[code]; taken && id != v.ID {
				return nil, errors.New(productenums.ErrSkuTaken)
			}
			if old := strings.TrimSpace(v.SKUCode); old != "" {
				delete(owner, old)
			}
			owner[code] = v.ID
			before := variantChangeSnapshot(v, nil)
			v.SKUCode = code
			v.UpdatedAt = now
			updated = append(updated, v)
			changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
				masterdataenums.OriginVariantGenerate, req.OperatorID, before, variantChangeSnapshot(v, nil)))
			res.Updated++
			continue
		}
		pairs, perr := normalizeListOptionPairs(row.OptionValues, allowed, groupOrder)
		if perr != nil {
			return nil, perr
		}
		raw := encodeOptionPairs(pairs)
		key := optionKey(pairs)
		if seenKeys[key] {
			res.Skipped = append(res.Skipped, productdto.VariantSaveSkip{
				SKUCode: strings.TrimSpace(row.SKUCode), OptionValues: raw,
				Reason: productenums.VariantSkipDuplicated,
			})
			continue
		}
		seenKeys[key] = true
		code := normalizeVariantSKU(row.SKUCode)
		if code == "" {
			// 清单里没给（或给成了空）：按变体 SKU 规则由系统生成。
			code = variantSKUCode(container, raw, groupOrder)
		}
		if strings.TrimSpace(code) == "" {
			return nil, errors.New(productenums.ErrVariantSKUEmpty)
		}
		if _, taken := owner[code]; taken {
			return nil, errors.New(productenums.ErrSkuTaken)
		}
		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw,
		}, refCode(ref), nil)
		if verr != nil {
			return nil, verr
		}
		v.SKUCode = code
		v.Sort = i
		owner[code] = v.ID
		created = append(created, v)
		changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
		res.Created++
	}

	// 清单外要删的既有变体：先查引用面（库存 / BOM），跳过的行逐条记原因。
	type pendingDelete struct {
		v      *productmodel.VariantEntity
		before masterdatacontract.FieldSnapshot
	}
	var deletions []pendingDelete
	for _, v := range existing {
		if kept[v.ID] {
			continue
		}
		reason, detail, rerr := s.variantDeleteBlockReason(ctx, p.ProjectID, v)
		if rerr != nil {
			return nil, rerr
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, variantSkipOf(v, reason, detail))
			continue
		}
		deletions = append(deletions, pendingDelete{v: v, before: variantChangeSnapshot(v, nil)})
	}

	// 写：新增 / 更新 / 库存行 / 逐条删除 / 留痕全部在**同一个事务**里 ——
	// 「清单外的行要删、清单里的行要建」是一个整体动作，中途失败留下「删了没建」或
	// 「建了没删」的半截清单，正是这个模型要避免的（AGENTS.md「写操作的事务与回滚」）。
	// 被引用 / 有库存的行在进入这里之前已经逐条跳过（res.Skipped），不构成中途失败。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}
		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, p.ID, v.ID, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}
		for _, item := range deletions {
			if xerr := s.m.DeleteVariantTx(ctx, tx, item.v.ID); xerr != nil {
				return xerr
			}
			changes = append(changes, variantChangeInput(p.ProjectID, item.v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariantGenerate, req.OperatorID, item.before, nil))
			res.Deleted++
		}
		return s.recordChangesTx(ctx, tx, changes...)
	}); err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后（价格与规格集合都可能变了）。
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
	s.fillVariantStock(ctx, p.ProjectID, res.Variants)
	return res, nil
}

// variantSkipOf 组装「跳过一行」的回带信息（原因取 enums 常量 = i18n key）。
//
// detail 是**可定位明细**（引用面 / 工程 / 商品 id），与 Reason 分开两个字段：
// Reason 会经 ?done= 回带到页面，读侧 productVariantNoticeMatches 要求每个原因都逐字
// 等于受控文案（带自由文本的形态会被判成伪造而整条回执消失），所以明细只能走
// 响应体里的 Detail，不能拼进 Reason。反之单条删除路径（DeleteVariant）返回的是错误，
// 那里按 key：detail 的既有形态拼接，明细照常可见。
func variantSkipOf(v *productmodel.VariantEntity, reason, detail string) productdto.VariantSaveSkip {
	if v == nil {
		return productdto.VariantSaveSkip{Reason: reason, Detail: detail}
	}
	return productdto.VariantSaveSkip{
		VariantID: v.ID, SKUCode: v.SKUCode,
		OptionValues: orJSON(v.OptionValues, "{}"), Reason: reason, Detail: detail,
	}
}

// variantDeleteBlockReason 变体删除的守卫：命中**四个引用面**之一时返回原因
// （enums 常量 = i18n key）+ 可定位明细（detail 为空表示该原因没有额外定位信息）。
//
//	① 库存非零（inventory_stocks；已有）—— 该 SKU 还有货，先处理库存或改为停用；
//	② BOM 引用（inventory_bom_items 的 component；已有）—— 先解除引用；
//	③ 捆绑成员引用（products.bundle_items.options[].variantId）——
//	   删掉它会让那些捆绑套餐的成员指向一个不存在的变体；**必须跨工程可发现**：
//	   别的工程把本工程的变体列为捆绑成员时，只在本工程里查会命中 0 行 ⇒ 删除放行 ⇒
//	   那些捆绑的成员清单永久悬空（审计 DB-03 §1.2 的守卫盲区）；
//	④ 有过任何库存流水（inventory_stock_movements）—— 订单一旦建单就必然产生
//	   扣减流水，所以「有流水」等价于「被订单用过」；历史单据按 variant_id 追溯，不允许硬删。
//
// ①②在 inventory 侧（同模块直调 model 具名方法，未注入时不拦 —— 纯商品单测路径）；
// ③在 product 侧（本 module 的表，恒可查，且跨工程可见性由逐工程作用域枚举取得，
// 见 model/product_ref_scan.go）；④经库存用例（契约方法，未注入时不拦）。
//
// ①②④仍按**单个工程作用域**查（这些表都在迁移 215 名单里，缺作用域会静默返回
// 「没被引用」）。它们的跨工程面属库存域，不在本票范围（DB-03 §6 第 7 条已记录）。
func (s *Service) variantDeleteBlockReason(ctx context.Context, projectID string, v *productmodel.VariantEntity) (reason, detail string, err error) {
	if v == nil {
		return "", "", nil
	}
	if s.inv != nil {
		n, cerr := s.inv.CountNonZeroStocksByVariant(ctx, v.ID, projectID)
		if cerr != nil {
			return "", "", cerr
		}
		if n > 0 {
			return productenums.VariantSkipHasStock, "", nil
		}
		hasParent, berr := s.inv.HasBOMParent(ctx, v.ID, projectID)
		if berr != nil {
			return "", "", berr
		}
		if hasParent {
			return productenums.VariantSkipReferenced, "", nil
		}
	}
	// ③ 捆绑成员引用：跨工程扫描（jsonb 包含判断下推，见 model.ProductRefsByBundleVariant 的注释）。
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByBundleVariant(ctx, v.ID, ids)
	})
	if rerr != nil {
		return "", "", rerr
	}
	if ref.Referenced() {
		return productenums.VariantSkipBundleReferenced, refGuardDetail(ref), nil
	}
	// ④ 库存流水（被订单用过）：订单域与库存域的口径都在这一条上。
	if s.invSvc != nil {
		moved, merr := s.invSvc.VariantHasStockMovement(ctx, projectID, v.ID)
		if merr != nil {
			return "", "", merr
		}
		if moved {
			return productenums.VariantSkipHasMovement, "", nil
		}
	}
	return "", "", nil
}

// resolveContainerSKU 容器主体 SKU（规则 A 的主体段）的**唯一**解析入口。
//
//	① 商品的主体编码（products.sku_code，迁移 246）—— 新建商品的权威来源；
//	② 存量商品该列为空 → 回落到最早创建的那条变体（商品创建时的无规格变体，
//	   它的 SKU 就是当时的容器主体）。**不重写它**，新变体以它为主体拼接；
//	③ 都拿不到 → 按商品 URL 段派生（确定性，不退回随机码）。
//
// 预览、生成、保存三条路径共用它：容器段一旦分叉，同一个组合会得到两种 SKU。
func resolveContainerSKU(p *productmodel.ProductEntity, existing []*productmodel.VariantEntity, warehouseCode string) (string, error) {
	if p == nil {
		return "", errors.New(productenums.ErrInvalidParam)
	}
	if container := strings.TrimSpace(p.SKUCode); container != "" {
		return container, nil
	}
	if container := containerSKUOfVariants(existing); container != "" {
		return container, nil
	}
	// 兜底派生也走容器主体 SKU 的**唯一**入口（本批合并）：三处路径共用一条分支逻辑，
	// 否则「新建时加仓码前缀、生成时忘了」这类差异只会在某个组合上暴露。
	return buildProductContainerSKU(containerSKUInput{
		ProductType: p.Type, Slug: p.Slug, WarehouseCode: warehouseCode,
	})
}

// normalizeVariantSKU 清单里运营可编辑的 SKU 文本的规范化。
//
// 逐段经 skuSegment 归一（ASCII 段原样保留、非 ASCII 段取确定性短码）——
// 与系统生成变体 SKU 时用的是同一个段规则（不是另写一套大小写 / 字符白名单）。
// 出现空段（含纯空白）或整串为空白时返回空串，由调用方报 ErrVariantSKUEmpty：
// 静默补一个「系统生成」的编码会让运营以为自己填的那个已经生效。
func normalizeVariantSKU(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, skuSeparator)
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
		parts[i] = skuSegment(part)
	}
	return strings.Join(parts, skuSeparator)
}

// normalizeListOptionPairs 校验并归一清单**新增行**的规格组合（服务端重算）。
//
// 属性组必须属于该商品且参与变体、取值必须是组内启用值（按 attributeValueCode 口径，
// 与 option_values 的写入口径逐字一致），键序按属性组固定顺序归一 ——
// 这三条与 buildVariationDimensions 对勾选的判定是同一份规则。
func normalizeListOptionPairs(raw json.RawMessage, allowed map[string]map[string]bool, groupOrder map[string]int) ([]optionPair, error) {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return nil, errors.New(productenums.ErrVariantOptionsInvalid)
	}
	for _, p := range pairs {
		values, ok := allowed[p.Key]
		if !ok {
			return nil, fmt.Errorf("%s：属性组 %s 未参与该商品的变体",
				productenums.ErrVariationAttributeInvalid, p.Key)
		}
		if !values[p.Value] {
			return nil, fmt.Errorf("%s：属性组 %s 下没有启用中的属性值 %s",
				productenums.ErrVariationValueInvalid, p.Key, p.Value)
		}
	}
	return sortedOptionPairs(pairs, groupOrder), nil
}

// variationValueIndex 参与变体的属性组 → 组内启用值的编码集合（清单新增行的校验依据）。
func variationValueIndex(attrs []*productmodel.ProductAttributeEntity) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(attrs))
	for _, a := range attrs {
		if a == nil || !a.IsVariation {
			continue
		}
		set := map[string]bool{}
		for _, v := range enabledAttributeValues(a) {
			set[attributeValueCode(v)] = true
		}
		if len(set) > 0 {
			out[a.Key] = set
		}
	}
	return out
}

// sortedOptionPairs 组合对按**属性组固定顺序**排序（组内按属性组 key 字典序兜底）。
//
// SKU 属性段的拼接与 option_values 的写入顺序都取它 —— 顺序不固定则同一组合会
// 生成不同 SKU（docs/14 §1.2 的第一条约束）。
func sortedOptionPairs(pairs []optionPair, groupOrder map[string]int) []optionPair {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	fallback := len(groupOrder) + 1
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, iok := groupOrder[sorted[i].Key]
		if !iok {
			ri = fallback
		}
		rj, jok := groupOrder[sorted[j].Key]
		if !jok {
			rj = fallback
		}
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Key < sorted[j].Key
	})
	return sorted
}
