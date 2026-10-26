// Package purchaseservice purchase 模块业务实现（货源 / 采购单 / 入库）。
//
// 边界（2026-10 从 inventory 拆出）：
//
//	#17  货源 —— 一张表承载所有进货来源（外部供应商 / 集团内关联公司 / 自家工厂），
//	     用 type + related_party 区分，改动进主数据变更记录；
//	#18  采购单与入库 —— 采购单（来源 = 货源的货物 + 收货仓）→ 收货入库、生产入库。
//
// **本模块不写库存真源**：收货引起的数量增减一律经 StockPort 由 inventory 模块在
// 调用方事务内落账（原因字典 / 流水 / 来源引用齐全），本模块只记「进了多少、单价多少」。
// 依赖方向 purchase → inventory（契约）、purchase → product（成本回写端口）、
// purchase → masterdata（货源留痕）；三者都不反向依赖本模块。
package purchaseservice

import (
	"context"
	"strings"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
)

// Service 货源 / 采购单 / 入库的业务实现。
//
// 只持有本模块 model 与外部契约；不持有 *gorm.DB。
type Service struct {
	m       *purchasemodel.Model
	project projectcontract.ProjectService
	// stock 库存侧能力（收货落账、仓解析、仓名映射）。装配缺失时收货路径明确报错，
	// 不会静默「只记账不动库存」—— 那正是账实不符的起点。
	stock purchasecontract.StockPort
	// variantCost 商品侧成本价写回端口（inventory 时代的兼容写回，端口由 product 实现）。
	// 未注入时按回写失败记在入库单行上（cost_error），不回滚已经落地的库存。
	variantCost productcontract.VariantCostPort
	// changes 主数据变更记录端口（由 masterdata 模块实现）。
	// 货源资料的字段级变更经它留痕；未注入时静默跳过（纯单元测试路径）。
	changes masterdatacontract.MasterDataService
}

// NewService 构造。
func NewService(m *purchasemodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetStock 注入库存侧能力（装配期调用；**必须注入**）。
func (s *Service) SetStock(port purchasecontract.StockPort) { s.stock = port }

// SetVariantCost 注入商品侧成本价写回端口（装配期调用；**必须注入**）。
func (s *Service) SetVariantCost(port productcontract.VariantCostPort) { s.variantCost = port }

// SetMasterDataChanges 注入主数据变更记录端口（装配期调用；**必须注入**）。
func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) { s.changes = port }

// 编译期断言：实现本模块契约。
var _ purchasecontract.PurchaseService = (*Service)(nil)

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

// sourceChangeSnapshot 货源主数据的字段白名单快照。
//
// 只有结构化列进快照：类型 / 关联方 / 结算价 / 状态都是报表与采购单要按它们取数或
// 校验的字段，改一次必须留痕；对接配置（config）是自由形状的 JSON，
// 用压缩后的文本参与 diff（等价 JSON 不产生假记录）。
func sourceChangeSnapshot(e *purchasemodel.SourceEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"code", e.Code,
		"name", e.Name,
		"type", e.Type,
		"related_party", masterdatacontract.FormatBool(e.RelatedParty),
		"settle_price", masterdatacontract.FormatPricePtr(e.SettlePrice),
		"status", e.Status,
		"config", masterdatacontract.FormatJSON(e.Config),
		"sort", masterdatacontract.FormatInt(e.Sort),
	)
}

// sourceChangeInput 组装货源级变更输入。
func sourceChangeInput(e *purchasemodel.SourceEntity, action, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityInventorySource,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: masterdataenums.OriginSource, OperatorID: operator,
		Before: before, After: after,
	}
}

// recordChangesTx 在**调用方事务内**记录主数据变更（端口未注入时空转）。
//
// master_data_changes 是 append-only：业务行提交了而留痕没提交，等于永久缺一条审计。
// 所以只要有主实体写，留痕就必须在同一事务里。
func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}

// orJSON 归一可空 jsonb（空值落成 fallback）。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

// —— 仓库侧 SKU 编码归一 ——

// skuSeparator 仓码与前缀后编码之间的分隔符（与商品侧同一个字面量）。
const skuSeparator = "_"

// stripWarehousePrefix 剥掉仓码前缀 —— 商品侧 attachWarehousePrefix 的逆操作。
//
// 三条性质（与商品侧逐条对称）：
//
//	· **幂等** —— 本来不带前缀时原样返回，剥两次与剥一次相同；
//	· **大小写不敏感** —— 前缀按大写比对（仓短码在工程内已归一为大写，历史数据不一定）；
//	· **只剥一次** —— SZ_SZ_X 剥成 SZ_X（不会一路剥到 X）。
//
// 仓码为空（未选仓 / 端口未注入）时原样返回：没有前缀就无所谓剥离。
func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

// normalizeStockSKU 把调用方给的 SKU 编码归一成**仓库侧裸码**，空串一律拒绝。
//
// 口径与 inventory 侧同名函数逐字一致（scripts/check-inventory-sku-format.sh 按它巡检
// 存量）：仓库里的 SKU 永远不带仓码前缀，而编码来源不可控（页面手选 / 外部系统 /
// 运维脚本），带前缀是常态 —— 一律拒绝等于把「没按内部表示传参」判成业务错误。
//
// 复制而不是共享的原因：那是 inventory 模块的 service 内部函数（未导出、也不该导出）。
// 两处漂移由门禁脚本兜住（它按同一口径查 inventory_stocks 与 inventory_purchase_order_lines）。
func normalizeStockSKU(code, warehouseCode string) (bare string, err error) {
	bare = stripWarehousePrefix(code, warehouseCode)
	if bare == "" {
		return "", purchaseenums.Err(purchaseenums.ErrStockSKURequired)
	}
	return bare, nil
}
