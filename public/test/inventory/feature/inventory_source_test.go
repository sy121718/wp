// Package feature inventory 模块 feature 测试 —— 货源管理（issue #17）。
//
// 覆盖本票四条验收（真实 PostgreSQL + 生产迁移与 seed + 真实 service + 真实 Jet 渲染）：
//  1. 可建货源，类型区分内部与外部，可标记关联方；
//  2. 货源可配置异构的对接扩展信息（JSON 对象，不同来源形状不同）；
//  3. 后台可管理货源（建 / 改 / 停启用 / 删 / 筛选，原生表单 + csrf_token）；
//  4. 关联方标志可用于报表区分（类型 × 关联方交叉统计 + 可组合筛选维度）。
//
// 另覆盖三组容易踩的边界：
//
//	· 内部货源恒为关联方 —— 显式取消被拒绝，数据库 CHECK 也兜得住（内部交易必须能进
//	  关联方报表，否则「内部交易单独出报表」这条需求在数据层就不成立）；
//	· 结算价只属于内部货源 —— 外部供应商的成本口径是采购单价，两套口径不能并存；
//	  改类型为外部时必须显式清空，不静默丢值也不静默留下；
//	· 对接配置必须是 JSON 对象 —— 标量 / 数组没有键可供适配器读取，写进去等于读不出来。
package feature

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	projectdto "go_wp/internal/module/project/dto"

	"go_wp/internal/templates"
	"go_wp/internal/web/shell"

	"go_wp/public/migrations"
)

// —— 小工具 ——

// createSource 建货源小工具（返回响应）。
func createSource(t *testing.T, f *invFixture, req *inventorydto.CreateSourceReq) *inventorydto.SourceResp {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	res, err := f.inventory.CreateSource(context.Background(), req)
	if err != nil {
		t.Fatalf("建货源 %s 失败: %v", req.Code, err)
	}
	return res
}

// mustCreateSource 建货源并断言成功（用于铺报表矩阵）。
func mustCreateSource(t *testing.T, f *invFixture, code, name, sourceType string, related *bool, settle *float64) *inventorydto.SourceResp {
	t.Helper()
	return createSource(t, f, &inventorydto.CreateSourceReq{
		Code: code, Name: name, Type: sourceType, RelatedParty: related, SettlePrice: settle,
	})
}

// boolPtr 取布尔指针（可空入参）。
func boolPtr(v bool) *bool { return &v }

// floatPtr 取浮点指针（可空入参）。
func floatPtr(v float64) *float64 { return &v }

// sourceErr 断言业务错误（service 层用 enums 消息键当错误文本）。
func sourceErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误 %s，实际成功", want)
	}
	if err.Error() != want {
		t.Fatalf("应返回错误 %s，实际 %v", want, err)
	}
}

// configJSON 解出 config 的通用形态（比较 JSON 语义而不是字节）。
func configJSON(t *testing.T, raw json.RawMessage) interface{} {
	t.Helper()
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("config 不是合法 JSON: %v (%s)", err, string(raw))
	}
	return out
}

// listSources 按条件列货源（失败即 fail）。
func listSources(t *testing.T, f *invFixture, req *inventorydto.ListSourceReq) []*inventorydto.SourceResp {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	list, err := f.inventory.ListSources(context.Background(), req)
	if err != nil {
		t.Fatalf("货源列表失败: %v", err)
	}
	return list
}

// sourceSummary 取关联方统计（失败即 fail）。
func sourceSummary(t *testing.T, f *invFixture, projectID string) *inventorydto.SourceSummaryResp {
	t.Helper()
	if projectID == "" {
		projectID = f.projectID
	}
	res, err := f.inventory.SourceSummary(context.Background(), &inventorydto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("货源统计失败: %v", err)
	}
	return res
}

// TestSourceCreateTypesAndRelatedParty 验收 1：
// 可建货源，类型区分内部与外部，可标记关联方。
func TestSourceCreateTypesAndRelatedParty(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 外部供应商：默认非关联方；编码归一为大写。
	ext := mustCreateSource(t, f, "sz_supplier", "苏州通达电子", "external", nil, nil)
	if ext.Code != "SZ_SUPPLIER" {
		t.Fatalf("货源编码应归一为大写，实际 %q", ext.Code)
	}
	if ext.Type != inventoryenums.SourceTypeExternal {
		t.Fatalf("类型应为外部供应商，实际 %q", ext.Type)
	}
	if ext.RelatedParty {
		t.Fatalf("未指定时外部供应商不应是关联方")
	}
	if ext.Status != inventoryenums.SourceStatusActive {
		t.Fatalf("未指定时状态应为启用中，实际 %q", ext.Status)
	}

	// 外部供应商也可以被标成关联方（同一实控人下的另一家公司）。
	extRelated := mustCreateSource(t, f, "REL_CO", "关联贸易公司", "external", boolPtr(true), nil)
	if !extRelated.RelatedParty {
		t.Fatalf("外部供应商应可被标记为关联方")
	}

	// 集团内关联公司：不显式标记也是关联方（内部交易必须能进关联方报表）。
	internal := mustCreateSource(t, f, "GROUP_CO", "集团内采购中心", "internal", nil, floatPtr(12.5))
	if !internal.RelatedParty {
		t.Fatalf("内部货源应自动成为关联方")
	}
	if internal.SettlePrice == nil || *internal.SettlePrice != 12.5 {
		t.Fatalf("内部货源的结算价应落库，实际 %+v", internal.SettlePrice)
	}

	// 自家工厂同样走 internal 类型（同一张表、同一套校验，不特殊化）。
	factory := mustCreateSource(t, f, "OWN_FACTORY", "自家杭州工厂", "internal", boolPtr(true), floatPtr(0))
	if factory.Type != inventoryenums.SourceTypeInternal || !factory.RelatedParty {
		t.Fatalf("自家工厂应为内部 + 关联方，实际 %+v", factory)
	}

	// 类型：空串取默认外部；未知类型拒绝。
	blank := createSource(t, f, &inventorydto.CreateSourceReq{Code: "BLANK_TYPE", Name: "未填类型"})
	if blank.Type != inventoryenums.SourceTypeExternal {
		t.Fatalf("类型留空应默认外部供应商，实际 %q", blank.Type)
	}
	_, err := f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		Code: "BAD_TYPE", Name: "类型非法", Type: "partner",
	})
	sourceErr(t, err, inventoryenums.ErrSourceTypeInvalid)

	// 内部货源显式取消关联方标记：拒绝（而不是静默改回去）。
	_, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		Code: "INT_UNRELATED", Name: "内部但非关联", Type: inventoryenums.SourceTypeInternal,
		RelatedParty: boolPtr(false),
	})
	sourceErr(t, err, inventoryenums.ErrSourceInternalNotRelated)

	// 名称 / 编码校验。
	_, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{Code: "NO_NAME"})
	sourceErr(t, err, inventoryenums.ErrSourceNameRequired)
	_, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{Name: "缺编码"})
	sourceErr(t, err, inventoryenums.ErrSourceCodeRequired)
	for _, bad := range []string{"S-1", "供应商", "TOO_LONG_SOURCE_CODE_1"} {
		_, cerr := f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{Code: bad, Name: "非法编码"})
		sourceErr(t, cerr, inventoryenums.ErrSourceCodeInvalid)
	}
	// 编码工程内唯一（大小写不敏感）。
	_, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{Code: "sz_supplier", Name: "重复编码"})
	sourceErr(t, err, inventoryenums.ErrSourceCodeTaken)

	// 另一个工程可以用同一个编码（唯一性是工程内的）。
	other, oerr := f.projects.Create(ctx, &projectdto.CreateReq{Name: "第二个站点工程"})
	if oerr != nil {
		t.Fatalf("建第二工程失败: %v", oerr)
	}
	if _, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		ProjectID: other.ID, Code: "SZ_SUPPLIER", Name: "另一个工程的同名货源",
	}); err != nil {
		t.Fatalf("编码唯一性是工程内的，跨工程同名应允许：%v", err)
	}

	// 详情 + 删除。
	//
	// DB-009：库里有多个工程时，按 id 单查必须显式给出工程作用域（resolveProjectID 的
	// 「唯一工程」兜底不再成立）—— 这正是隔离本身要求的调用形态。
	got, err := f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: internal.ID, ProjectID: f.projectID})
	if err != nil || got.Code != "GROUP_CO" {
		t.Fatalf("货源详情异常：%v %+v", err, got)
	}
	if err = f.inventory.DeleteSource(ctx, &inventorydto.DeleteSourceReq{ID: factory.ID, ProjectID: f.projectID}); err != nil {
		t.Fatalf("删除货源失败: %v", err)
	}
	if _, err = f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: factory.ID, ProjectID: f.projectID}); err == nil ||
		err.Error() != inventoryenums.ErrSourceNotFound {
		t.Fatalf("删除后应按 ErrSourceNotFound 报错，实际 %v", err)
	}

	// 数据库 CHECK 兜底：绕过服务层直接写「内部但非关联」必须被拒绝。
	if err = f.db.Exec("INSERT INTO inventory_sources (project_id, code, name, type, related_party) VALUES (?,?,?,?,?)",
		f.projectID, "RAW_INTERNAL", "直写内部非关联", inventoryenums.SourceTypeInternal, false).Error; err == nil {
		t.Fatalf("(内部 + 非关联) 直写应被 CHECK 约束拒绝")
	}
	// 类型 / 状态的取值约束同样在数据库一层。
	if err = f.db.Exec("INSERT INTO inventory_sources (project_id, code, name, type) VALUES (?,?,?,?)",
		f.projectID, "RAW_TYPE", "直写非法类型", "partner").Error; err == nil {
		t.Fatalf("非法类型直写应被 CHECK 约束拒绝")
	}
}

// TestSourceHeterogeneousConfig 验收 2：
// 货源可配置异构的对接扩展信息（JSON 对象，不同来源形状不同）。
func TestSourceHeterogeneousConfig(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	configs := map[string]string{
		"SAP_ERP":  `{"erp":"sap","endpoint":"https://ERP_HOST/api","warehouseCode":"SZ01","fields":{"sku":"MATNR","qty":"MENGE"}}`,
		"MAIL_SUP": `{"contact":"buyer@example.com","leadTimeDays":7,"priceList":["A","B"],"autoPo":false}`,
		"MANUAL":   `{"note":"手工对账，无接口"}`,
	}
	ids := map[string]string{}
	for code, cfg := range configs {
		res := createSource(t, f, &inventorydto.CreateSourceReq{
			Code: code, Name: code, Config: json.RawMessage(cfg),
		})
		ids[code] = res.ID
		// 形状原样保留（比较 JSON 语义而不是字节顺序）。
		if got, want := configJSON(t, res.Config), configJSON(t, json.RawMessage(cfg)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s 的对接配置应原样保留：want=%v got=%v", code, want, got)
		}
	}

	// 逐条回读：三种完全不同的形状都能原样取回（异构扩展信息的实际含义）。
	for code, cfg := range configs {
		got, err := f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: ids[code]})
		if err != nil {
			t.Fatalf("读货源 %s 失败: %v", code, err)
		}
		if !reflect.DeepEqual(configJSON(t, got.Config), configJSON(t, json.RawMessage(cfg))) {
			t.Fatalf("%s 的对接配置回读不一致：%s", code, string(got.Config))
		}
	}

	// 对接配置是扩展信息，核心结构必须是独立的结构化列 ——
	// 报表按类型 / 关联方分组，采购单按结算价校验，它们都不能藏在 JSON 里。
	wantColumns := map[string]bool{
		"type": false, "related_party": false, "settle_price": false,
		"status": false, "config": false,
	}
	rows, err := f.db.Raw("SELECT column_name, data_type FROM information_schema.columns " +
		"WHERE table_schema = current_schema() AND table_name = 'inventory_sources'").Rows()
	if err != nil {
		t.Fatalf("查询 inventory_sources 列失败: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, dataType string
		if err = rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("扫描列失败: %v", err)
		}
		if _, ok := wantColumns[name]; ok {
			wantColumns[name] = true
		}
		if name == "config" && dataType != "jsonb" {
			t.Fatalf("config 应为 jsonb，实际 %s", dataType)
		}
		if name == "type" && dataType != "text" {
			t.Fatalf("type 应为结构化 text 列，实际 %s", dataType)
		}
	}
	for name, found := range wantColumns {
		if !found {
			t.Fatalf("inventory_sources 缺少列 %s", name)
		}
	}

	// 非对象一律拒绝：标量 / 数组没有键可供适配器读取。
	for i, bad := range []string{`[]`, `123`, `"text"`, `null`, `{bad json}`} {
		_, cerr := f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
			Code: fmt.Sprintf("BAD_CFG_%d", i), Name: "非法配置",
			Config: json.RawMessage(bad),
		})
		sourceErr(t, cerr, inventoryenums.ErrSourceConfigInvalid)
	}

	// 留空即空对象；更新可整体替换。
	blank := createSource(t, f, &inventorydto.CreateSourceReq{Code: "NO_CFG", Name: "无对接配置"})
	if string(blank.Config) != "{}" {
		t.Fatalf("未填对接配置应落成 {}，实际 %s", string(blank.Config))
	}
	replaced := `{"erp":"kingdee","endpoint":"https://ERP_HOST/new"}`
	updated, err := f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: ids["SAP_ERP"], Config: json.RawMessage(replaced),
	})
	if err != nil {
		t.Fatalf("更新对接配置失败: %v", err)
	}
	if !reflect.DeepEqual(configJSON(t, updated.Config), configJSON(t, json.RawMessage(replaced))) {
		t.Fatalf("对接配置应被整体替换，实际 %s", string(updated.Config))
	}
	// 更新路径同样拒绝非对象配置。
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: ids["SAP_ERP"], Config: json.RawMessage("[]"),
	}); err == nil || err.Error() != inventoryenums.ErrSourceConfigInvalid {
		t.Fatalf("更新非法配置应返回 ErrSourceConfigInvalid，实际 %v", err)
	}
}

// TestSourceSettlePriceRules 验收 1/2 的边界：
// 结算价只属于内部货源，且改类型时必须显式清空（不静默丢值、不静默留下）。
func TestSourceSettlePriceRules(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 外部供应商带结算价：拒绝（外部成本口径是采购单价）。
	_, err := f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		Code: "EXT_SETTLE", Name: "外部带结算价", Type: inventoryenums.SourceTypeExternal,
		SettlePrice: floatPtr(9.9),
	})
	sourceErr(t, err, inventoryenums.ErrSourceSettleNotInternal)

	// 负结算价：拒绝。
	_, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		Code: "NEG_SETTLE", Name: "负结算价", Type: inventoryenums.SourceTypeInternal,
		SettlePrice: floatPtr(-1),
	})
	sourceErr(t, err, inventoryenums.ErrSourceSettleInvalid)

	// 内部货源：可设结算价，也可不设（0 是合法值，NULL 与 0 是两件事）。
	withPrice := mustCreateSource(t, f, "INT_PRICE", "集团内结算", inventoryenums.SourceTypeInternal, nil, floatPtr(8.25))
	withoutPrice := mustCreateSource(t, f, "INT_NOPRICE", "自家工厂", inventoryenums.SourceTypeInternal, nil, nil)
	if withPrice.SettlePrice == nil || *withPrice.SettlePrice != 8.25 {
		t.Fatalf("内部结算价应为 8.25，实际 %+v", withPrice.SettlePrice)
	}
	if withoutPrice.SettlePrice != nil {
		t.Fatalf("未填结算价应为 NULL，实际 %+v", withoutPrice.SettlePrice)
	}

	// 类型改外部：既有结算价必须先显式清空，否则拒绝（绝不静默丢值）。
	external := inventoryenums.SourceTypeExternal
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: withPrice.ID, Type: &external,
	}); err == nil || err.Error() != inventoryenums.ErrSourceSettleNotInternal {
		t.Fatalf("改类型为外部且留有结算价应被拒绝，实际 %v", err)
	}
	// 显式清空后可以改类型。
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: withPrice.ID, Type: &external, ClearSettlePrice: true,
	}); err != nil {
		t.Fatalf("清空结算价后改类型应成功：%v", err)
	}
	after, err := f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: withPrice.ID})
	if err != nil {
		t.Fatalf("读货源失败: %v", err)
	}
	if after.Type != inventoryenums.SourceTypeExternal || after.SettlePrice != nil {
		t.Fatalf("改类型后应为外部且无结算价，实际 %+v", after)
	}

	// 外部 → 内部：关联方标志自动变真（内部即关联方）。
	internal := inventoryenums.SourceTypeInternal
	flipped, err := f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: withPrice.ID, Type: &internal, SettlePrice: floatPtr(4.5),
	})
	if err != nil {
		t.Fatalf("改类型为内部失败: %v", err)
	}
	if !flipped.RelatedParty || flipped.SettlePrice == nil || *flipped.SettlePrice != 4.5 {
		t.Fatalf("改为内部后应自动成为关联方并接受结算价，实际 %+v", flipped)
	}

	// 内部货源取消关联方标记：拒绝。
	no := false
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: withPrice.ID, RelatedParty: &no,
	}); err == nil || err.Error() != inventoryenums.ErrSourceInternalNotRelated {
		t.Fatalf("内部货源取消关联方标记应被拒绝，实际 %v", err)
	}

	// 状态与名称：停用可写回，名称清空拒绝。
	disabled := inventoryenums.SourceStatusDisabled
	off, err := f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{ID: withPrice.ID, Status: &disabled})
	if err != nil || off.Status != inventoryenums.SourceStatusDisabled {
		t.Fatalf("停用货源失败：%v %+v", err, off)
	}
	blankName := "   "
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{ID: withPrice.ID, Name: &blankName}); err == nil ||
		err.Error() != inventoryenums.ErrSourceNameRequired {
		t.Fatalf("清空名称应返回 ErrSourceNameRequired，实际 %v", err)
	}
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{ID: "00000000-0000-0000-0000-000000000000"}); err == nil ||
		err.Error() != inventoryenums.ErrSourceNotFound {
		t.Fatalf("不存在的货源应返回 ErrSourceNotFound，实际 %v", err)
	}
}

// seedSourceMatrix 铺一套报表矩阵：3 外部（其中 1 个关联方）+ 2 内部（各带结算价）。
//
// 返回 code → id。
func seedSourceMatrix(t *testing.T, f *invFixture) map[string]string {
	t.Helper()
	ids := map[string]string{}
	add := func(code, name, sourceType string, related *bool, settle *float64) {
		res := mustCreateSource(t, f, code, name, sourceType, related, settle)
		ids[code] = res.ID
	}
	add("EXT_A", "外部供应商甲", inventoryenums.SourceTypeExternal, nil, nil)
	add("EXT_B", "外部供应商乙", inventoryenums.SourceTypeExternal, nil, nil)
	add("EXT_REL", "外部关联供应商", inventoryenums.SourceTypeExternal, boolPtr(true), nil)
	add("INT_GROUP", "集团内采购中心", inventoryenums.SourceTypeInternal, nil, floatPtr(12.5))
	add("INT_FACTORY", "自家工厂", inventoryenums.SourceTypeInternal, nil, floatPtr(0))
	return ids
}

// TestSourceRelatedPartyReporting 验收 4：
// 关联方标志可用于报表区分（交叉统计 + 可组合筛选维度）。
func TestSourceRelatedPartyReporting(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	ids := seedSourceMatrix(t, f)

	// 交叉统计：类型（内部 / 外部）× 关联方（是 / 否）。
	sum := sourceSummary(t, f, "")
	if sum.Total != 5 || sum.Internal != 2 || sum.External != 3 {
		t.Fatalf("类型口径应为 总数 5 / 内部 2 / 外部 3，实际 %+v", sum)
	}
	if sum.RelatedParty != 3 || sum.Unrelated != 2 {
		t.Fatalf("关联方口径应为 关联方 3 / 非关联方 2，实际 %+v", sum)
	}
	if sum.SettlePriced != 2 {
		t.Fatalf("已设内部结算价的货源应为 2，实际 %d", sum.SettlePriced)
	}
	group := map[string]int64{}
	for _, g := range sum.Groups {
		group[fmt.Sprintf("%s/%v", g.Type, g.RelatedParty)] = g.Count
	}
	want := map[string]int64{"external/false": 2, "external/true": 1, "internal/true": 2}
	if !reflect.DeepEqual(group, want) {
		t.Fatalf("交叉分组应为 %v，实际 %v", want, group)
	}

	// 筛选维度：关联方三态 + 类型。
	if got := listSources(t, f, &inventorydto.ListSourceReq{RelatedParty: "true"}); len(got) != 3 {
		t.Fatalf("仅关联方应有 3 条，实际 %d", len(got))
	}
	for _, s := range listSources(t, f, &inventorydto.ListSourceReq{RelatedParty: "false"}) {
		if s.RelatedParty {
			t.Fatalf("「仅非关联方」不应出现关联方 %s", s.Code)
		}
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{RelatedParty: "false"}); len(got) != 2 {
		t.Fatalf("仅非关联方应有 2 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{Type: inventoryenums.SourceTypeInternal}); len(got) != 2 {
		t.Fatalf("内部货源应有 2 条，实际 %d", len(got))
	}
	// 组合维度：外部 + 关联方 = 1。
	if got := listSources(t, f, &inventorydto.ListSourceReq{
		Type: inventoryenums.SourceTypeExternal, RelatedParty: "true",
	}); len(got) != 1 {
		t.Fatalf("外部 + 关联方应有 1 条，实际 %d", len(got))
	}
	// 关键词维度。
	if got := listSources(t, f, &inventorydto.ListSourceReq{Keyword: "集团"}); len(got) != 1 {
		t.Fatalf("按关键词「集团」应有 1 条，实际 %d", len(got))
	}

	// 拼错的筛选值一律拒绝 —— 静默当成「不过滤」会让报表悄悄给出全量数字。
	_, err := f.inventory.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: f.projectID, RelatedParty: "yes",
	})
	sourceErr(t, err, inventoryenums.ErrSourceFilterInvalid)
	_, err = f.inventory.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: f.projectID, Type: "partner",
	})
	sourceErr(t, err, inventoryenums.ErrSourceTypeInvalid)

	// 停用：列表默认不再列出，但统计口径不变（停用只是「不再选用」，历史口径不动）。
	disabled := inventoryenums.SourceStatusDisabled
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{ID: ids["EXT_B"], Status: &disabled}); err != nil {
		t.Fatalf("停用货源失败: %v", err)
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{}); len(got) != 4 {
		t.Fatalf("默认应只列启用中的 4 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{IncludeDisabled: true}); len(got) != 5 {
		t.Fatalf("includeDisabled 应列全部 5 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{Status: inventoryenums.SourceStatusDisabled}); len(got) != 1 {
		t.Fatalf("按「已停用」应列 1 条，实际 %d", len(got))
	}
	after := sourceSummary(t, f, "")
	if after.Total != 5 || after.External != 3 || after.Unrelated != 2 {
		t.Fatalf("停用不应改变统计口径，实际 %+v", after)
	}

	// 跨工程隔离：另一个工程的统计与列表互不串味。
	other, oerr := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if oerr != nil {
		t.Fatalf("建第二工程失败: %v", oerr)
	}
	if _, err = f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		ProjectID: other.ID, Code: "OTHER_EXT", Name: "另一个工程的外部供应商",
	}); err != nil {
		t.Fatalf("给第二工程建货源失败: %v", err)
	}
	otherSum := sourceSummary(t, f, other.ID)
	if otherSum.Total != 1 || otherSum.External != 1 || otherSum.RelatedParty != 0 {
		t.Fatalf("第二个工程的统计应独立，实际 %+v", otherSum)
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{ProjectID: other.ID}); len(got) != 1 {
		t.Fatalf("第二个工程应有 1 条货源，实际 %d", len(got))
	}
}

// TestSourceAdminPage 验收 3：后台可管理货源（真实 Jet 渲染 + 原生表单写链路）。
func TestSourceAdminPage(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()

	// 表单建货源：内部 + 结算价 + 对接配置。
	rec := postForm(engine, "/admin/inventory/sources/create", url.Values{
		"projectId": {f.projectID}, "code": {"group_co"}, "name": {"集团内采购中心"},
		"type": {inventoryenums.SourceTypeInternal}, "relatedParty": {""},
		"settlePrice": {"12.50"}, "sort": {"1"},
		"config": {`{"erp":"sap","warehouseCode":"SZ01"}`},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建货源应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 外部供应商（表单路径，不标记关联方 —— 它同时是「关联方筛选要排除掉的那一类」）。
	rec = postForm(engine, "/admin/inventory/sources/create", url.Values{
		"projectId": {f.projectID}, "code": {"EXT_A"}, "name": {"外部供应商甲"},
		"type": {inventoryenums.SourceTypeExternal}, "relatedParty": {""},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建外部货源应 302，实际 %d", rec.Code)
	}
	// 列表落库校验（走契约读，不直查表）。
	all := listSources(t, f, &inventorydto.ListSourceReq{IncludeDisabled: true})
	if len(all) != 2 {
		t.Fatalf("应落库 2 条货源，实际 %d", len(all))
	}
	var internal *inventorydto.SourceResp
	for _, s := range all {
		if s.Code == "GROUP_CO" {
			internal = s
		}
	}
	if internal == nil || !internal.RelatedParty || internal.SettlePrice == nil || *internal.SettlePrice != 12.5 {
		t.Fatalf("表单建的内部货源字段不正确：%+v", internal)
	}

	// 页面渲染：标题 / 说明 / 两条货源 / 关联方统计 / 表单 csrf_token 隐藏域。
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("货源页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"货源管理", "集团内采购中心", "外部供应商甲", "GROUP_CO", "EXT_A",
		"内部（集团内 / 自家工厂）", "外部供应商", "关联方",
		"关联方报表区分（类型 × 关联方）", "新建货源", "对接配置",
		`name="csrf_token"`, "12.50",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("货源页缺少 %q", want)
		}
	}

	// 页面上的筛选维度直接可用：仅关联方 → 只剩内部那条。
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&relatedParty=true")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "EXT_A") {
		t.Fatalf("按关联方筛选后不应再出现非关联方 EXT_A：%d", rec.Code)
	}
	// 非法筛选值：页面仍 200，错误经徽标回显（不白屏）。
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&relatedParty=yes")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "上一次操作未完成") {
		t.Fatalf("非法筛选值应回显错误而不是白屏，实际 %d", rec.Code)
	}

	// 表单改货源：改名 + 停用 + 清空结算价（表单即最终状态：留空 = 无结算价）。
	rec = postForm(engine, "/admin/inventory/sources/update", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
		"code": {"GROUP_CO"}, "name": {"集团内采购中心（已改名）"},
		"type": {inventoryenums.SourceTypeInternal}, "relatedParty": {""},
		"status": {inventoryenums.SourceStatusDisabled}, "settlePrice": {""}, "sort": {"3"},
		"config": {`{"erp":"kingdee"}`},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 改货源应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got, err := f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: internal.ID})
	if err != nil {
		t.Fatalf("读改后的货源失败: %v", err)
	}
	if got.Name != "集团内采购中心（已改名）" || got.Status != inventoryenums.SourceStatusDisabled ||
		got.SettlePrice != nil || got.Sort != 3 {
		t.Fatalf("表单改货源未生效：%+v", got)
	}
	// jsonb 的文字表示会规整空白（`{"erp": "kingdee"}`），比较 JSON 语义而不是字节。
	if !reflect.DeepEqual(configJSON(t, got.Config), configJSON(t, json.RawMessage(`{"erp":"kingdee"}`))) {
		t.Fatalf("表单改对接配置未生效：%s", string(got.Config))
	}

	// 状态筛选三态：默认只列启用中（停用的那条不出现），选「全部（含停用）」才列出来。
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "集团内采购中心（已改名）") {
		t.Fatalf("默认视图不应列停用的货源：%d", rec.Code)
	}
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&status=all")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "集团内采购中心（已改名）") {
		t.Fatalf("「全部（含停用）」应列出停用的货源：%d", rec.Code)
	}
	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&status=disabled")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "集团内采购中心（已改名）") {
		t.Fatalf("按「已停用」筛选应列出该货源：%d", rec.Code)
	}

	// 内部货源关掉关联方：错误经 ?err= 回显，且落库值不变。
	rec = postForm(engine, "/admin/inventory/sources/update", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
		"code": {"GROUP_CO"}, "name": {"集团内采购中心（已改名）"},
		"type": {inventoryenums.SourceTypeInternal}, "relatedParty": {"false"},
		"status": {inventoryenums.SourceStatusActive}, "settlePrice": {""},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("取消内部货源关联方标记应回列表并带错误提示，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
	got, _ = f.inventory.GetSource(ctx, &inventorydto.GetSourceReq{ID: internal.ID})
	if !got.RelatedParty {
		t.Fatalf("失败的更新不应改动关联方标志")
	}

	// 表单删货源。
	rec = postForm(engine, "/admin/inventory/sources/delete", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 删货源应 302，实际 %d", rec.Code)
	}
	if got := listSources(t, f, &inventorydto.ListSourceReq{IncludeDisabled: true}); len(got) != 1 {
		t.Fatalf("删除后应剩 1 条，实际 %d", len(got))
	}
}

// TestSourceAPIChain 接口链路：JSON 入参 → service → pkg/response 出参。
func TestSourceAPIChain(t *testing.T) {
	engine, f := newSourceAPIEngine(t)
	if engine == nil {
		return
	}

	rec := postSourceJSON(engine, "/api/inventory/source/create", map[string]interface{}{
		"projectId": f.projectID, "code": "api_ext", "name": "接口建的货源",
		"type": inventoryenums.SourceTypeExternal, "relatedParty": true,
		"config": map[string]interface{}{"erp": "api"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("接口建货源应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Code    int                     `json:"code"`
		Message string                  `json:"message"`
		Data    inventorydto.SourceResp `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if created.Code != http.StatusOK || created.Data.Code != "API_EXT" || !created.Data.RelatedParty {
		t.Fatalf("接口返回不正确：%+v", created)
	}

	// 列表（带关联方筛选维度）。
	rec = httptestGet(engine, "/api/inventory/source/list?projectId="+f.projectID+"&relatedParty=true")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "API_EXT") {
		t.Fatalf("接口列表应返回关联方货源，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 非法筛选值：400 + 业务消息键（不静默当成全量）。
	rec = httptestGet(engine, "/api/inventory/source/list?projectId="+f.projectID+"&relatedParty=yes")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), inventoryenums.ErrSourceFilterInvalid) {
		t.Fatalf("非法筛选值应 400 + %s，实际 %d：%s", inventoryenums.ErrSourceFilterInvalid, rec.Code, rec.Body.String())
	}
	// 统计出口。
	rec = httptestGet(engine, "/api/inventory/source/summary?projectId="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"relatedParty":1`) {
		t.Fatalf("统计接口应返回关联方计数，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 详情 + 删除。
	rec = httptestGet(engine, "/api/inventory/source/get?id="+created.Data.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "API_EXT") {
		t.Fatalf("详情接口异常：%d %s", rec.Code, rec.Body.String())
	}
	rec = postSourceJSON(engine, "/api/inventory/source/delete", map[string]interface{}{
		"id": created.Data.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("删除接口应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	rec = httptestGet(engine, "/api/inventory/source/get?id="+created.Data.ID)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), inventoryenums.ErrSourceNotFound) {
		t.Fatalf("删除后详情应 404 + %s，实际 %d", inventoryenums.ErrSourceNotFound, rec.Code)
	}
}

// TestSourcePermissionsAndMenuSeeded 迁移 105/106/107：
// 货源权限点与后台菜单已 seed（未 seed 时 Casbin 无策略 → 含超管全员 403）。
func TestSourcePermissionsAndMenuSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	codes := []string{
		"inventory:source_list", "inventory:source_get", "inventory:source_create",
		"inventory:source_update", "inventory:source_delete", "inventory:source_summary",
	}
	for _, code := range codes {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE permission_code = ?", code).Scan(&hit).Error; err != nil {
			t.Fatalf("查询权限点 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("权限点 %s 应已 seed，实际 %d 条", code, hit)
		}
	}
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE module = 'inventory'").Scan(&n).Error; err != nil {
		t.Fatalf("查询 inventory 权限点失败: %v", err)
	}
	// 100（#15 九个）+ 104（#16 十个）+ 106（#17 六个）+ 109（#18 采购单与入库七个）。
	if n != 32 {
		t.Fatalf("inventory 模块应有 32 个权限点，实际 %d", n)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '货源管理' AND deleted_at IS NULL").Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 107 应 seed 「货源管理」后台菜单，实际 %d", n)
	}
	for _, code := range []string{"inventory:source_create", "inventory:source_update", "inventory:source_delete"} {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 3 AND permission_code = ? AND deleted_at IS NULL", code).Scan(&hit).Error; err != nil {
			t.Fatalf("查询菜单按钮 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("货源按钮 %s 应已 seed，实际 %d 条", code, hit)
		}
	}
	// 权限点已挂路径 / 方法（Casbin 按 path+method 匹配）。
	var apiPath, apiMethod string
	if err := f.db.Raw("SELECT api_path, api_method FROM sys_permission WHERE permission_code = 'inventory:source_summary'").
		Row().Scan(&apiPath, &apiMethod); err != nil {
		t.Fatalf("查询权限点路径失败: %v", err)
	}
	if apiPath != "/api/inventory/source/summary" || apiMethod != "GET" {
		t.Fatalf("权限点路径 / 方法不正确：%s %s", apiPath, apiMethod)
	}
}

// newSourcePageEngine 装配只挂货源管理页的测试引擎（真实 Jet 模板 + 真实 service）。
func newSourcePageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	// 新建/编辑入口按权限显隐（shell.Prepare 读 PermSetKey），而这条链路不走鉴权中间件：
	// 注入一份权限，让页面把所有原生表单写入口都渲染出来（多端契约断言的正是这些入口）。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"inventory:source_create": true, "inventory:source_update": true, "inventory:source_delete": true,
		})
	})
	handle := inventoryhttp.NewInventorySourcePageHandle(f.inventory, f.projects)
	engine.GET("/admin/inventory/sources", handle.InventorySourcesPage)
	engine.POST("/admin/inventory/sources/create", handle.InventorySourceCreate)
	engine.POST("/admin/inventory/sources/update", handle.InventorySourceUpdate)
	engine.POST("/admin/inventory/sources/delete", handle.InventorySourceDelete)
	return engine, f
}

// newSourceAPIEngine 装配只挂货源 JSON 接口的测试引擎（真实 handler + pkg/response）。
func newSourceAPIEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := inventoryhttp.NewHandle(f.inventory)
	engine.GET("/api/inventory/source/list", handle.ListSources)
	engine.GET("/api/inventory/source/get", handle.GetSource)
	engine.GET("/api/inventory/source/summary", handle.SourceSummary)
	engine.POST("/api/inventory/source/create", handle.CreateSource)
	engine.POST("/api/inventory/source/update", handle.UpdateSource)
	engine.POST("/api/inventory/source/delete", handle.DeleteSource)
	return engine, f
}

// postSourceJSON 发送 application/json 请求体。
func postSourceJSON(engine *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}
