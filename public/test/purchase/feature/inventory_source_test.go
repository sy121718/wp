package feature

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectdto "go_wp/internal/module/project/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasehttp "go_wp/internal/module/purchase/inbound/http"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	"go_wp/public/migrations"
)

func createSource(t *testing.T, f *purchaseFixture, req *purchasedto.CreateSourceReq) *purchasedto.SourceResp {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	res, err := f.purchase.CreateSource(context.Background(), req)
	if err != nil {
		t.Fatalf("建货源 %s 失败: %v", req.Code, err)
	}
	return res
}

func mustCreateSource(t *testing.T, f *purchaseFixture, code, name, sourceType string, related *bool, settle *float64) *purchasedto.SourceResp {
	t.Helper()
	return createSource(t, f, &purchasedto.CreateSourceReq{
		Code: code, Name: name, Type: sourceType, RelatedParty: related, SettlePrice: settle,
	})
}

func boolPtr(v bool) *bool { return &v }

func floatPtr(v float64) *float64 { return &v }

func sourceErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误 %s，实际成功", want)
	}
	if err.Error() != want {
		t.Fatalf("应返回错误 %s，实际 %v", want, err)
	}
}

func configJSON(t *testing.T, raw json.RawMessage) interface{} {
	t.Helper()
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("config 不是合法 JSON: %v (%s)", err, string(raw))
	}
	return out
}

func listSources(t *testing.T, f *purchaseFixture, req *purchasedto.ListSourceReq) []*purchasedto.SourceResp {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	list, err := f.purchase.ListSources(context.Background(), req)
	if err != nil {
		t.Fatalf("货源列表失败: %v", err)
	}
	return list
}

func sourceSummary(t *testing.T, f *purchaseFixture, projectID string) *purchasedto.SourceSummaryResp {
	t.Helper()
	if projectID == "" {
		projectID = f.projectID
	}
	res, err := f.purchase.SourceSummary(context.Background(), &purchasedto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("货源统计失败: %v", err)
	}
	return res
}

func TestSourceCreateTypesAndRelatedParty(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	ext := mustCreateSource(t, f, "sz_supplier", "苏州通达电子", "external", nil, nil)
	if ext.Code != "SZ_SUPPLIER" {
		t.Fatalf("货源编码应归一为大写，实际 %q", ext.Code)
	}
	if ext.Type != purchaseenums.SourceTypeExternal {
		t.Fatalf("类型应为外部供应商，实际 %q", ext.Type)
	}
	if ext.RelatedParty {
		t.Fatalf("未指定时外部供应商不应是关联方")
	}
	if ext.Status != purchaseenums.SourceStatusActive {
		t.Fatalf("未指定时状态应为启用中，实际 %q", ext.Status)
	}

	extRelated := mustCreateSource(t, f, "REL_CO", "关联贸易公司", "external", boolPtr(true), nil)
	if !extRelated.RelatedParty {
		t.Fatalf("外部供应商应可被标记为关联方")
	}

	internal := mustCreateSource(t, f, "GROUP_CO", "集团内采购中心", "internal", nil, floatPtr(12.5))
	if !internal.RelatedParty {
		t.Fatalf("内部货源应自动成为关联方")
	}
	if internal.SettlePrice == nil || *internal.SettlePrice != 12.5 {
		t.Fatalf("内部货源的结算价应落库，实际 %+v", internal.SettlePrice)
	}

	factory := mustCreateSource(t, f, "OWN_FACTORY", "自家杭州工厂", "internal", boolPtr(true), floatPtr(0))
	if factory.Type != purchaseenums.SourceTypeInternal || !factory.RelatedParty {
		t.Fatalf("自家工厂应为内部 + 关联方，实际 %+v", factory)
	}

	blank := createSource(t, f, &purchasedto.CreateSourceReq{Code: "BLANK_TYPE", Name: "未填类型"})
	if blank.Type != purchaseenums.SourceTypeExternal {
		t.Fatalf("类型留空应默认外部供应商，实际 %q", blank.Type)
	}
	_, err := f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		Code: "BAD_TYPE", Name: "类型非法", Type: "partner",
	})
	sourceErr(t, err, purchaseenums.ErrSourceTypeInvalid)

	_, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		Code: "INT_UNRELATED", Name: "内部但非关联", Type: purchaseenums.SourceTypeInternal,
		RelatedParty: boolPtr(false),
	})
	sourceErr(t, err, purchaseenums.ErrSourceInternalNotRelated)

	_, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{Code: "NO_NAME"})
	sourceErr(t, err, purchaseenums.ErrSourceNameRequired)
	_, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{Name: "缺编码"})
	sourceErr(t, err, purchaseenums.ErrSourceCodeRequired)
	for _, bad := range []string{"S-1", "供应商", "TOO_LONG_SOURCE_CODE_1"} {
		_, cerr := f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{Code: bad, Name: "非法编码"})
		sourceErr(t, cerr, purchaseenums.ErrSourceCodeInvalid)
	}

	_, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{Code: "sz_supplier", Name: "重复编码"})
	sourceErr(t, err, purchaseenums.ErrSourceCodeTaken)

	other, oerr := f.projects.Create(ctx, &projectdto.CreateReq{Name: "第二个站点工程"})
	if oerr != nil {
		t.Fatalf("建第二工程失败: %v", oerr)
	}
	if _, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		ProjectID: other.ID, Code: "SZ_SUPPLIER", Name: "另一个工程的同名货源",
	}); err != nil {
		t.Fatalf("编码唯一性是工程内的，跨工程同名应允许：%v", err)
	}

	got, err := f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: internal.ID, ProjectID: f.projectID})
	if err != nil || got.Code != "GROUP_CO" {
		t.Fatalf("货源详情异常：%v %+v", err, got)
	}
	if err = f.purchase.DeleteSource(ctx, &purchasedto.DeleteSourceReq{ID: factory.ID, ProjectID: f.projectID}); err != nil {
		t.Fatalf("删除货源失败: %v", err)
	}
	if _, err = f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: factory.ID, ProjectID: f.projectID}); err == nil ||
		err.Error() != purchaseenums.ErrSourceNotFound {
		t.Fatalf("删除后应按 ErrSourceNotFound 报错，实际 %v", err)
	}

	if err = f.db.Exec("INSERT INTO inventory_sources (project_id, code, name, type, related_party) VALUES (?,?,?,?,?)",
		f.projectID, "RAW_INTERNAL", "直写内部非关联", purchaseenums.SourceTypeInternal, false).Error; err == nil {
		t.Fatalf("(内部 + 非关联) 直写应被 CHECK 约束拒绝")
	}

	if err = f.db.Exec("INSERT INTO inventory_sources (project_id, code, name, type) VALUES (?,?,?,?)",
		f.projectID, "RAW_TYPE", "直写非法类型", "partner").Error; err == nil {
		t.Fatalf("非法类型直写应被 CHECK 约束拒绝")
	}
}

func TestSourceHeterogeneousConfig(t *testing.T) {
	f := newPurchaseFixture(t)
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
		res := createSource(t, f, &purchasedto.CreateSourceReq{
			Code: code, Name: code, Config: json.RawMessage(cfg),
		})
		ids[code] = res.ID

		if got, want := configJSON(t, res.Config), configJSON(t, json.RawMessage(cfg)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s 的对接配置应原样保留：want=%v got=%v", code, want, got)
		}
	}

	for code, cfg := range configs {
		got, err := f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: ids[code]})
		if err != nil {
			t.Fatalf("读货源 %s 失败: %v", code, err)
		}
		if !reflect.DeepEqual(configJSON(t, got.Config), configJSON(t, json.RawMessage(cfg))) {
			t.Fatalf("%s 的对接配置回读不一致：%s", code, string(got.Config))
		}
	}

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

	for i, bad := range []string{`[]`, `123`, `"text"`, `null`, `{bad json}`} {
		_, cerr := f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
			Code: fmt.Sprintf("BAD_CFG_%d", i), Name: "非法配置",
			Config: json.RawMessage(bad),
		})
		sourceErr(t, cerr, purchaseenums.ErrSourceConfigInvalid)
	}

	blank := createSource(t, f, &purchasedto.CreateSourceReq{Code: "NO_CFG", Name: "无对接配置"})
	if string(blank.Config) != "{}" {
		t.Fatalf("未填对接配置应落成 {}，实际 %s", string(blank.Config))
	}
	replaced := `{"erp":"kingdee","endpoint":"https://ERP_HOST/new"}`
	updated, err := f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: ids["SAP_ERP"], Config: json.RawMessage(replaced),
	})
	if err != nil {
		t.Fatalf("更新对接配置失败: %v", err)
	}
	if !reflect.DeepEqual(configJSON(t, updated.Config), configJSON(t, json.RawMessage(replaced))) {
		t.Fatalf("对接配置应被整体替换，实际 %s", string(updated.Config))
	}

	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: ids["SAP_ERP"], Config: json.RawMessage("[]"),
	}); err == nil || err.Error() != purchaseenums.ErrSourceConfigInvalid {
		t.Fatalf("更新非法配置应返回 ErrSourceConfigInvalid，实际 %v", err)
	}
}

func TestSourceSettlePriceRules(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	_, err := f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		Code: "EXT_SETTLE", Name: "外部带结算价", Type: purchaseenums.SourceTypeExternal,
		SettlePrice: floatPtr(9.9),
	})
	sourceErr(t, err, purchaseenums.ErrSourceSettleNotInternal)

	_, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		Code: "NEG_SETTLE", Name: "负结算价", Type: purchaseenums.SourceTypeInternal,
		SettlePrice: floatPtr(-1),
	})
	sourceErr(t, err, purchaseenums.ErrSourceSettleInvalid)

	withPrice := mustCreateSource(t, f, "INT_PRICE", "集团内结算", purchaseenums.SourceTypeInternal, nil, floatPtr(8.25))
	withoutPrice := mustCreateSource(t, f, "INT_NOPRICE", "自家工厂", purchaseenums.SourceTypeInternal, nil, nil)
	if withPrice.SettlePrice == nil || *withPrice.SettlePrice != 8.25 {
		t.Fatalf("内部结算价应为 8.25，实际 %+v", withPrice.SettlePrice)
	}
	if withoutPrice.SettlePrice != nil {
		t.Fatalf("未填结算价应为 NULL，实际 %+v", withoutPrice.SettlePrice)
	}

	external := purchaseenums.SourceTypeExternal
	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: withPrice.ID, Type: &external,
	}); err == nil || err.Error() != purchaseenums.ErrSourceSettleNotInternal {
		t.Fatalf("改类型为外部且留有结算价应被拒绝，实际 %v", err)
	}

	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: withPrice.ID, Type: &external, ClearSettlePrice: true,
	}); err != nil {
		t.Fatalf("清空结算价后改类型应成功：%v", err)
	}
	after, err := f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: withPrice.ID})
	if err != nil {
		t.Fatalf("读货源失败: %v", err)
	}
	if after.Type != purchaseenums.SourceTypeExternal || after.SettlePrice != nil {
		t.Fatalf("改类型后应为外部且无结算价，实际 %+v", after)
	}

	internal := purchaseenums.SourceTypeInternal
	flipped, err := f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: withPrice.ID, Type: &internal, SettlePrice: floatPtr(4.5),
	})
	if err != nil {
		t.Fatalf("改类型为内部失败: %v", err)
	}
	if !flipped.RelatedParty || flipped.SettlePrice == nil || *flipped.SettlePrice != 4.5 {
		t.Fatalf("改为内部后应自动成为关联方并接受结算价，实际 %+v", flipped)
	}

	no := false
	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: withPrice.ID, RelatedParty: &no,
	}); err == nil || err.Error() != purchaseenums.ErrSourceInternalNotRelated {
		t.Fatalf("内部货源取消关联方标记应被拒绝，实际 %v", err)
	}

	disabled := purchaseenums.SourceStatusDisabled
	off, err := f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{ID: withPrice.ID, Status: &disabled})
	if err != nil || off.Status != purchaseenums.SourceStatusDisabled {
		t.Fatalf("停用货源失败：%v %+v", err, off)
	}
	blankName := "   "
	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{ID: withPrice.ID, Name: &blankName}); err == nil ||
		err.Error() != purchaseenums.ErrSourceNameRequired {
		t.Fatalf("清空名称应返回 ErrSourceNameRequired，实际 %v", err)
	}
	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{ID: "00000000-0000-0000-0000-000000000000"}); err == nil ||
		err.Error() != purchaseenums.ErrSourceNotFound {
		t.Fatalf("不存在的货源应返回 ErrSourceNotFound，实际 %v", err)
	}
}

func seedSourceMatrix(t *testing.T, f *purchaseFixture) map[string]string {
	t.Helper()
	ids := map[string]string{}
	add := func(code, name, sourceType string, related *bool, settle *float64) {
		res := mustCreateSource(t, f, code, name, sourceType, related, settle)
		ids[code] = res.ID
	}
	add("EXT_A", "外部供应商甲", purchaseenums.SourceTypeExternal, nil, nil)
	add("EXT_B", "外部供应商乙", purchaseenums.SourceTypeExternal, nil, nil)
	add("EXT_REL", "外部关联供应商", purchaseenums.SourceTypeExternal, boolPtr(true), nil)
	add("INT_GROUP", "集团内采购中心", purchaseenums.SourceTypeInternal, nil, floatPtr(12.5))
	add("INT_FACTORY", "自家工厂", purchaseenums.SourceTypeInternal, nil, floatPtr(0))
	return ids
}

func TestSourceRelatedPartyReporting(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	ids := seedSourceMatrix(t, f)

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

	if got := listSources(t, f, &purchasedto.ListSourceReq{RelatedParty: "true"}); len(got) != 3 {
		t.Fatalf("仅关联方应有 3 条，实际 %d", len(got))
	}
	for _, s := range listSources(t, f, &purchasedto.ListSourceReq{RelatedParty: "false"}) {
		if s.RelatedParty {
			t.Fatalf("「仅非关联方」不应出现关联方 %s", s.Code)
		}
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{RelatedParty: "false"}); len(got) != 2 {
		t.Fatalf("仅非关联方应有 2 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{Type: purchaseenums.SourceTypeInternal}); len(got) != 2 {
		t.Fatalf("内部货源应有 2 条，实际 %d", len(got))
	}

	if got := listSources(t, f, &purchasedto.ListSourceReq{
		Type: purchaseenums.SourceTypeExternal, RelatedParty: "true",
	}); len(got) != 1 {
		t.Fatalf("外部 + 关联方应有 1 条，实际 %d", len(got))
	}

	if got := listSources(t, f, &purchasedto.ListSourceReq{Keyword: "集团"}); len(got) != 1 {
		t.Fatalf("按关键词「集团」应有 1 条，实际 %d", len(got))
	}

	_, err := f.purchase.ListSources(ctx, &purchasedto.ListSourceReq{
		ProjectID: f.projectID, RelatedParty: "yes",
	})
	sourceErr(t, err, purchaseenums.ErrSourceFilterInvalid)
	_, err = f.purchase.ListSources(ctx, &purchasedto.ListSourceReq{
		ProjectID: f.projectID, Type: "partner",
	})
	sourceErr(t, err, purchaseenums.ErrSourceTypeInvalid)

	disabled := purchaseenums.SourceStatusDisabled
	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{ID: ids["EXT_B"], Status: &disabled}); err != nil {
		t.Fatalf("停用货源失败: %v", err)
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{}); len(got) != 4 {
		t.Fatalf("默认应只列启用中的 4 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{IncludeDisabled: true}); len(got) != 5 {
		t.Fatalf("includeDisabled 应列全部 5 条，实际 %d", len(got))
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{Status: purchaseenums.SourceStatusDisabled}); len(got) != 1 {
		t.Fatalf("按「已停用」应列 1 条，实际 %d", len(got))
	}
	after := sourceSummary(t, f, "")
	if after.Total != 5 || after.External != 3 || after.Unrelated != 2 {
		t.Fatalf("停用不应改变统计口径，实际 %+v", after)
	}

	other, oerr := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if oerr != nil {
		t.Fatalf("建第二工程失败: %v", oerr)
	}
	if _, err = f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		ProjectID: other.ID, Code: "OTHER_EXT", Name: "另一个工程的外部供应商",
	}); err != nil {
		t.Fatalf("给第二工程建货源失败: %v", err)
	}
	otherSum := sourceSummary(t, f, other.ID)
	if otherSum.Total != 1 || otherSum.External != 1 || otherSum.RelatedParty != 0 {
		t.Fatalf("第二个工程的统计应独立，实际 %+v", otherSum)
	}
	if got := listSources(t, f, &purchasedto.ListSourceReq{ProjectID: other.ID}); len(got) != 1 {
		t.Fatalf("第二个工程应有 1 条货源，实际 %d", len(got))
	}
}

func TestSourceAdminPage(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()

	rec := postForm(engine, "/admin/inventory/sources/create", url.Values{
		"projectId": {f.projectID}, "code": {"group_co"}, "name": {"集团内采购中心"},
		"type": {purchaseenums.SourceTypeInternal}, "relatedParty": {""},
		"settlePrice": {"12.50"}, "sort": {"1"},
		"config": {`{"erp":"sap","warehouseCode":"SZ01"}`},
	})

	assertInventoryJump(t, rec, "ok")

	rec = postForm(engine, "/admin/inventory/sources/create", url.Values{
		"projectId": {f.projectID}, "code": {"EXT_A"}, "name": {"外部供应商甲"},
		"type": {purchaseenums.SourceTypeExternal}, "relatedParty": {""},
	})
	assertInventoryJump(t, rec, "ok")

	all := listSources(t, f, &purchasedto.ListSourceReq{IncludeDisabled: true})
	if len(all) != 2 {
		t.Fatalf("应落库 2 条货源，实际 %d", len(all))
	}
	var internal *purchasedto.SourceResp
	for _, s := range all {
		if s.Code == "GROUP_CO" {
			internal = s
		}
	}
	if internal == nil || !internal.RelatedParty || internal.SettlePrice == nil || *internal.SettlePrice != 12.5 {
		t.Fatalf("表单建的内部货源字段不正确：%+v", internal)
	}

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

	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&relatedParty=true")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "EXT_A") {
		t.Fatalf("按关联方筛选后不应再出现非关联方 EXT_A：%d", rec.Code)
	}

	rec = httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&relatedParty=yes")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "上一次操作未完成") {
		t.Fatalf("非法筛选值应回显错误而不是白屏，实际 %d", rec.Code)
	}

	rec = postForm(engine, "/admin/inventory/sources/update", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
		"code": {"GROUP_CO"}, "name": {"集团内采购中心（已改名）"},
		"type": {purchaseenums.SourceTypeInternal}, "relatedParty": {""},
		"status": {purchaseenums.SourceStatusDisabled}, "settlePrice": {""}, "sort": {"3"},
		"config": {`{"erp":"kingdee"}`},
	})
	assertInventoryJump(t, rec, "ok")
	got, err := f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: internal.ID})
	if err != nil {
		t.Fatalf("读改后的货源失败: %v", err)
	}
	if got.Name != "集团内采购中心（已改名）" || got.Status != purchaseenums.SourceStatusDisabled ||
		got.SettlePrice != nil || got.Sort != 3 {
		t.Fatalf("表单改货源未生效：%+v", got)
	}

	if !reflect.DeepEqual(configJSON(t, got.Config), configJSON(t, json.RawMessage(`{"erp":"kingdee"}`))) {
		t.Fatalf("表单改对接配置未生效：%s", string(got.Config))
	}

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

	rec = postForm(engine, "/admin/inventory/sources/update", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
		"code": {"GROUP_CO"}, "name": {"集团内采购中心（已改名）"},
		"type": {purchaseenums.SourceTypeInternal}, "relatedParty": {"false"},
		"status": {purchaseenums.SourceStatusActive}, "settlePrice": {""},
	})
	assertInventoryJump(t, rec, "err")
	got, _ = f.purchase.GetSource(ctx, &purchasedto.GetSourceReq{ID: internal.ID})
	if !got.RelatedParty {
		t.Fatalf("失败的更新不应改动关联方标志")
	}

	rec = postForm(engine, "/admin/inventory/sources/delete", url.Values{
		"projectId": {f.projectID}, "id": {internal.ID},
	})
	assertInventoryJump(t, rec, "ok")
	if got := listSources(t, f, &purchasedto.ListSourceReq{IncludeDisabled: true}); len(got) != 1 {
		t.Fatalf("删除后应剩 1 条，实际 %d", len(got))
	}
}

func TestSourceAPIChain(t *testing.T) {
	engine, f := newSourceAPIEngine(t)
	if engine == nil {
		return
	}

	rec := postSourceJSON(engine, "/api/inventory/source/create", map[string]interface{}{
		"projectId": f.projectID, "code": "api_ext", "name": "接口建的货源",
		"type": purchaseenums.SourceTypeExternal, "relatedParty": true,
		"config": map[string]interface{}{"erp": "api"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("接口建货源应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    purchasedto.SourceResp `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if created.Code != http.StatusOK || created.Data.Code != "API_EXT" || !created.Data.RelatedParty {
		t.Fatalf("接口返回不正确：%+v", created)
	}

	rec = httptestGet(engine, "/api/inventory/source/list?projectId="+f.projectID+"&relatedParty=true")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "API_EXT") {
		t.Fatalf("接口列表应返回关联方货源，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/inventory/source/list?projectId="+f.projectID+"&relatedParty=yes")

	filterText := inventoryenums.FallbackFor(purchaseenums.ErrSourceFilterInvalid)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), filterText) ||
		strings.Contains(rec.Body.String(), purchaseenums.ErrSourceFilterInvalid) {
		t.Fatalf("非法筛选值应 400 + %s，实际 %d：%s", filterText, rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/inventory/source/summary?projectId="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"relatedParty":1`) {
		t.Fatalf("统计接口应返回关联方计数，实际 %d：%s", rec.Code, rec.Body.String())
	}

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
	notFoundText := inventoryenums.FallbackFor(purchaseenums.ErrSourceNotFound)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), notFoundText) ||
		strings.Contains(rec.Body.String(), purchaseenums.ErrSourceNotFound) {
		t.Fatalf("删除后详情应 404 + %s，实际 %d：%s", notFoundText, rec.Code, rec.Body.String())
	}
}

func TestSourcePermissionsAndMenuSeeded(t *testing.T) {
	f := newPurchaseFixture(t)
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

	if n != 28 {
		t.Fatalf("inventory 模块应有 28 个权限点（104 已从 10 收到 6），实际 %d", n)
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

	var apiPath, apiMethod string
	if err := f.db.Raw("SELECT api_path, api_method FROM sys_permission WHERE permission_code = 'inventory:source_summary'").
		Row().Scan(&apiPath, &apiMethod); err != nil {
		t.Fatalf("查询权限点路径失败: %v", err)
	}
	if apiPath != "/api/inventory/source/summary" || apiMethod != "GET" {
		t.Fatalf("权限点路径 / 方法不正确：%s %s", apiPath, apiMethod)
	}
}

func newSourcePageEngine(t *testing.T) (*gin.Engine, *purchaseFixture) {
	t.Helper()
	f := newPurchaseFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"inventory:source_create": true, "inventory:source_update": true, "inventory:source_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"inventory.source_create": true, "inventory.source_update": true, "inventory.source_delete": true,
		})

	})
	handle := purchasehttp.NewSourcePageHandle(f.purchase, f.projects)
	engine.GET("/admin/inventory/sources", adminui.PageSlot(purchasehttp.SourcesPageTemplate), handle.SourcesPage)
	engine.POST("/admin/inventory/sources/create", handle.SourceCreate)
	engine.POST("/admin/inventory/sources/update", handle.SourceUpdate)
	engine.POST("/admin/inventory/sources/delete", handle.SourceDelete)
	return engine, f
}

func newSourceAPIEngine(t *testing.T) (*gin.Engine, *purchaseFixture) {
	t.Helper()
	f := newPurchaseFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := purchasehttp.NewHandle(f.purchase)
	engine.GET("/api/inventory/source/list", handle.ListSources)
	engine.GET("/api/inventory/source/get", handle.GetSource)
	engine.GET("/api/inventory/source/summary", handle.SourceSummary)
	engine.POST("/api/inventory/source/create", handle.CreateSource)
	engine.POST("/api/inventory/source/update", handle.UpdateSource)
	engine.POST("/api/inventory/source/delete", handle.DeleteSource)
	return engine, f
}

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
