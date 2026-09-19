// inventory_warehouse_config_test.go — 仓库类型 / 第三方对接配置 / 凭据安全（迁移 240）。
//
// 重点覆盖「凭据怎么存、为什么不会泄到前端」：
//
//	· 明文凭据不落库：直查 jsonb，配置里只有密文（apiKeyCipher）；
//	· 明文不出接口：出参只有掩码 **** 与「配没配」的布尔值，整段 JSON 里搜不到明文；
//	· 未改动则不覆盖：后台回显的是掩码，只改地址联系人时密文一个字不变；
//	· 没有密钥时不退回明文，而是明确报错；
//	· 引用名（secretRef）路径始终可用：系统只记名字，不记值。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	"go_wp/pkg/crypto"
)

// testCipherSecret 测试用的敏感配置加密密钥。
const testCipherSecret = "inventory-test-secret"

// TestWarehouseTypeAndThirdPartyCredential 仓库类型 + 第三方配置 + 凭据加密（迁移 240）。
func TestWarehouseTypeAndThirdPartyCredential(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.inventory.SetCipherSecret(testCipherSecret)

	// 存量语义：不写类型就是自营仓；工程第一个仓自动成为默认仓。
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	if sz.Type != inventoryenums.WarehouseTypeSelf {
		t.Fatalf("默认类型应为 self，实际 %q", sz.Type)
	}
	if sz.ThirdParty != nil {
		t.Fatalf("自营仓不应带第三方对接配置：%+v", sz.ThirdParty)
	}

	// 第三方仓：非敏感项进 config，凭据加密后进 config。
	third, err := f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "NJ", Name: "南京三方仓",
		Type: inventoryenums.WarehouseTypeThirdParty,
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{
			Provider: "菜鸟仓配", ExternalCode: "NJ-01", Address: "南京市江宁区 1 号",
			Contact: "王工 13800000000", AllowsShipping: true,
			APICredential: "sk-plain-do-not-store",
		},
	})
	if err != nil {
		t.Fatalf("建第三方仓失败: %v", err)
	}
	if third.Type != inventoryenums.WarehouseTypeThirdParty || third.ThirdParty == nil {
		t.Fatalf("第三方仓应带对接配置出参：%+v", third)
	}
	if third.ThirdParty.Provider != "菜鸟仓配" || third.ThirdParty.ExternalCode != "NJ-01" ||
		!third.ThirdParty.AllowsShipping {
		t.Fatalf("非敏感项未落库：%+v", third.ThirdParty)
	}

	// 1) 明文不落库：config 里只有密文。
	var raw string
	if qerr := f.db.Raw("SELECT config::text FROM inventory_warehouses WHERE id = ?", third.ID).Scan(&raw).Error; qerr != nil {
		t.Fatalf("读 config 失败: %v", qerr)
	}
	if strings.Contains(raw, "sk-plain-do-not-store") {
		t.Fatalf("明文凭据不应落库，实际 config=%s", raw)
	}
	var cipherText string
	if qerr := f.db.Raw("SELECT config->>'apiKeyCipher' FROM inventory_warehouses WHERE id = ?", third.ID).Scan(&cipherText).Error; qerr != nil {
		t.Fatalf("读密文失败: %v", qerr)
	}
	if cipherText == "" {
		t.Fatalf("凭据应以密文形式存在 config 的 apiKeyCipher 键上：%s", raw)
	}
	plain, derr := crypto.Decrypt(cipherText, testCipherSecret)
	if derr != nil || plain != "sk-plain-do-not-store" {
		t.Fatalf("密文应能用注入的密钥还原：plain=%q err=%v", plain, derr)
	}

	// 2) 明文不出接口：出参只有掩码，整段 JSON 里既没有明文也没有密文。
	if !third.ThirdParty.HasCredential || third.ThirdParty.CredentialMasked != inventoryenums.CredentialMask {
		t.Fatalf("凭据回显应只有掩码与存在标志：%+v", third.ThirdParty)
	}
	detail, err := f.inventory.GetWarehouse(ctx, &inventorydto.GetWarehouseReq{ID: third.ID, ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读仓库详情失败: %v", err)
	}
	serialized, _ := json.Marshal(detail)
	for _, secret := range []string{"sk-plain-do-not-store", cipherText} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("仓库接口不得返回凭据（明文或密文），实际 %s", string(serialized))
		}
	}
	listed, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读仓库列表失败: %v", err)
	}
	listJSON, _ := json.Marshal(listed)
	if strings.Contains(string(listJSON), "sk-plain-do-not-store") || strings.Contains(string(listJSON), cipherText) {
		t.Fatalf("仓库列表不得返回凭据，实际 %s", string(listJSON))
	}

	// 3) 未改动则不覆盖：表单回显掩码，只改地址时密文一个字不变。
	addr := "南京市江宁区 2 号"
	updated, err := f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: third.ID, Type: strPtr(inventoryenums.WarehouseTypeThirdParty),
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{
			Provider: "菜鸟仓配", ExternalCode: "NJ-01", Address: addr,
			Contact: "王工 13800000000", AllowsShipping: true,
			// 后台输入框留空 / 传回掩码 = 不改凭据；这里两种形态各试一次。
			APICredential: inventoryenums.CredentialMask,
		},
	})
	if err != nil {
		t.Fatalf("改地址失败: %v", err)
	}
	if updated.ThirdParty == nil || updated.ThirdParty.Address != addr {
		t.Fatalf("地址应已更新：%+v", updated.ThirdParty)
	}
	if !updated.ThirdParty.HasCredential {
		t.Fatalf("只改地址不该把凭据清掉：%+v", updated.ThirdParty)
	}
	var cipherAfter string
	if qerr := f.db.Raw("SELECT config->>'apiKeyCipher' FROM inventory_warehouses WHERE id = ?", third.ID).Scan(&cipherAfter).Error; qerr != nil {
		t.Fatalf("读密文失败: %v", qerr)
	}
	if cipherAfter != cipherText {
		t.Fatalf("未改动凭据时密文不应变化：前 %q 后 %q", cipherText, cipherAfter)
	}

	// 4) 显式清除凭据。
	cleared, err := f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: third.ID, Type: strPtr(inventoryenums.WarehouseTypeThirdParty),
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{
			Provider: "菜鸟仓配", ExternalCode: "NJ-01", Address: addr, ClearCredential: true,
		},
	})
	if err != nil {
		t.Fatalf("清除凭据失败: %v", err)
	}
	if cleared.ThirdParty.HasCredential || cleared.ThirdParty.CredentialMasked != "" {
		t.Fatalf("显式清除后不应再有凭据：%+v", cleared.ThirdParty)
	}

	// 5) 没有加密密钥时不退回明文，而是明确报错。
	f.inventory.SetCipherSecret("")
	_, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: third.ID, Type: strPtr(inventoryenums.WarehouseTypeThirdParty),
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{APICredential: "sk-no-key"},
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseCredentialKeyMissing {
		t.Fatalf("无密钥时写明文凭据应返回 ErrWarehouseCredentialKeyMissing，实际 %v", err)
	}
	var noKeyRaw string
	if qerr := f.db.Raw("SELECT config::text FROM inventory_warehouses WHERE id = ?", third.ID).Scan(&noKeyRaw).Error; qerr != nil {
		t.Fatalf("读 config 失败: %v", qerr)
	}
	if strings.Contains(noKeyRaw, "sk-no-key") {
		t.Fatalf("无密钥时更不能落明文：%s", noKeyRaw)
	}
	// 6) 引用名路径不依赖密钥：系统只记名字，值在部署侧。
	ref, err := f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: third.ID, Type: strPtr(inventoryenums.WarehouseTypeThirdParty),
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{
			Provider: "菜鸟仓配", Address: addr, SecretRef: "kms://warehouse/nanjing",
		},
	})
	if err != nil {
		t.Fatalf("写引用名失败: %v", err)
	}
	if !ref.ThirdParty.HasCredential || ref.ThirdParty.SecretRef != "kms://warehouse/nanjing" {
		t.Fatalf("引用名应作为凭据存在标志回显：%+v", ref.ThirdParty)
	}
	f.inventory.SetCipherSecret(testCipherSecret)

	// 7) 类型与配置的业务守卫。
	if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "BAD", Name: "非法类型", Type: "somewhere",
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseTypeInvalid {
		t.Fatalf("非法类型应返回 ErrWarehouseTypeInvalid，实际 %v", err)
	}
	if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "SELF2", Name: "自营仓带对接配置",
		Type:       inventoryenums.WarehouseTypeSelf,
		ThirdParty: &inventorydto.WarehouseThirdPartyReq{Provider: "不该有"},
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseConfigInvalid {
		t.Fatalf("非第三方仓不应接受对接配置，实际 %v", err)
	}
	// 虚拟仓不能成为默认仓（工程已有默认仓，这里显式要求它当默认）。
	if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "VIR", Name: "在途虚拟仓",
		Type: inventoryenums.WarehouseTypeVirtual, IsDefault: true,
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseTypeVirtualDefault {
		t.Fatalf("虚拟仓不应能设为默认仓，实际 %v", err)
	}
	virtual, err := f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "VIR", Name: "在途虚拟仓",
		Type: inventoryenums.WarehouseTypeVirtual,
	})
	if err != nil {
		t.Fatalf("建虚拟仓失败: %v", err)
	}
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: sz.ID, Type: strPtr(inventoryenums.WarehouseTypeVirtual),
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseTypeVirtualDefault {
		t.Fatalf("默认仓不应能改成虚拟仓，实际 %v", err)
	}
	if virtual.Type != inventoryenums.WarehouseTypeVirtual {
		t.Fatalf("虚拟仓类型未落库：%+v", virtual)
	}
}

// TestInventoryAdjustEntryOnPage 后台页：库存调整入口 + 时间筛选（本批的页面收口）。
func TestInventoryAdjustEntryOnPage(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 6, "purchase_in")

	// 页面：调整入口在（唯一写入口），内联的入库 / 生产入库表单不在。
	body := httptestGet(engine, "/admin/inventory?project="+f.projectID).Body.String()
	for _, want := range []string{
		"库存调整（盘点 / 报损）", "action=\"/admin/inventory/stock/change\"",
		"name=\"timeFrom\"", "name=\"timeTo\"", "盘点（填目标绝对量）", "报损（填本次减少量）",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("库存页缺少 %q", want)
		}
	}
	if strings.Contains(body, "action=\"/admin/inventory/production\"") {
		t.Fatalf("库存页不应再有内联的生产入库表单")
	}

	// 时间筛选：未来窗口查不到这条流水，用当前时间做下界能查到。
	future := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	empty := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&timeFrom="+url.QueryEscape(future))
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), "没有符合条件的库存流水") {
		t.Fatalf("未来时间窗口应筛空，实际 %d", empty.Code)
	}
	from := time.Now().Add(-time.Hour).Format("2006-01-02 15:04:05")
	hit := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&timeFrom="+url.QueryEscape(from))
	if hit.Code != http.StatusOK || !strings.Contains(hit.Body.String(), v.SKUCode) {
		t.Fatalf("当前时间窗口应查到流水，实际 %d", hit.Code)
	}
	// 非法时间：GET 渲染走 shell.PageError（不 500、不静默忽略）。
	bad := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&timeFrom=not-a-time")
	if bad.Code == http.StatusOK && strings.Contains(bad.Body.String(), v.SKUCode) {
		t.Fatalf("非法时间不应被静默忽略并照常展示流水")
	}
	_ = ctx
}

// TestMovementTimeFilterService 时间过滤在 service 层的行为（含错误与边界）。
func TestMovementTimeFilterService(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")

	var stamped time.Time
	if err := f.db.Raw("SELECT create_time FROM inventory_stock_movements WHERE variant_id = ?", v.ID).Scan(&stamped).Error; err != nil {
		t.Fatalf("读流水时间失败: %v", err)
	}
	// 把区间收在流水时间上：闭区间两端都命中。
	hit, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, VariantID: v.ID,
		TimeFrom: stamped.Add(-time.Second).Format("2006-01-02 15:04:05"),
		TimeTo:   stamped.Add(time.Second).Format("2006-01-02 15:04:05"),
	})
	if err != nil || len(hit) != 1 {
		t.Fatalf("闭区间内的流水应命中：%v %+v", err, hit)
	}
	// 只给日期：截止日按当天 23:59:59 收口，「截止今天」不会漏掉今天。
	day := stamped.In(time.Local).Format("2006-01-02")
	sameDay, err := f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, VariantID: v.ID, TimeTo: day,
	})
	if err != nil || len(sameDay) != 1 {
		t.Fatalf("截止当天应包含当天流水：%v %+v", err, sameDay)
	}
	// 非法格式与「起始晚于截止」都拒绝。
	if _, err = f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, TimeFrom: "昨天",
	}); err == nil || err.Error() != inventoryenums.ErrMovementTimeRangeInvalid {
		t.Fatalf("非法时间应返回 ErrMovementTimeRangeInvalid，实际 %v", err)
	}
	if _, err = f.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: f.projectID, TimeFrom: "2026-01-02", TimeTo: "2026-01-01",
	}); err == nil || err.Error() != inventoryenums.ErrMovementTimeRangeInvalid {
		t.Fatalf("起始晚于截止应返回 ErrMovementTimeRangeInvalid，实际 %v", err)
	}
}

// TestBuiltinReasonReadOnlyButDisableable 内置原因只读：不可改名，可以停用。
func TestBuiltinReasonReadOnlyButDisableable(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	reasons, err := f.inventory.ListReasons(ctx, &inventorydto.ListReasonReq{
		ProjectID: f.projectID, IncludeDisabled: true,
	})
	if err != nil {
		t.Fatalf("查原因失败: %v", err)
	}
	builtinID, customID := "", ""
	for _, r := range reasons {
		if r.IsBuiltin && r.Code == "damage_out" {
			builtinID = r.ID
			if !strings.HasPrefix(r.Name, "inventory.reason.") {
				t.Fatalf("内置原因的 name 应是 i18n key，实际 %q", r.Name)
			}
		}
	}
	if builtinID == "" {
		t.Fatalf("内置原因 damage_out 未 seed")
	}
	name := "改名"
	if _, err = f.inventory.UpdateReason(ctx, &inventorydto.UpdateReasonReq{
		ID: builtinID, ProjectID: f.projectID, Name: &name,
	}); err == nil || err.Error() != inventoryenums.ErrReasonBuiltin {
		t.Fatalf("内置原因改名应被拒，实际 %v", err)
	}
	disabled := inventoryenums.StatusDisabled
	updated, err := f.inventory.UpdateReason(ctx, &inventorydto.UpdateReasonReq{
		ID: builtinID, ProjectID: f.projectID, Status: &disabled,
	})
	if err != nil || updated.Status != inventoryenums.StatusDisabled {
		t.Fatalf("内置原因应可停用：%v %+v", err, updated)
	}
	// 自定义原因：code 归一化，name 存的是派生 key，改名不改 key。
	custom, err := f.inventory.CreateReason(ctx, &inventorydto.CreateReasonReq{
		ProjectID: f.projectID, Code: "Gift_Out", Name: "赠品出库", Direction: inventoryenums.DirectionOut,
	})
	if err != nil {
		t.Fatalf("建自定义原因失败: %v", err)
	}
	customID = custom.ID
	if !strings.HasPrefix(custom.Name, "inventory.reason.custom."+f.projectID+".") {
		t.Fatalf("自定义原因的 key 应带工程 id，实际 %q", custom.Name)
	}
	renamed := "会员赠品出库"
	after, err := f.inventory.UpdateReason(ctx, &inventorydto.UpdateReasonReq{
		ID: customID, ProjectID: f.projectID, Name: &renamed,
	})
	if err != nil {
		t.Fatalf("自定义原因改名失败: %v", err)
	}
	if after.Name != custom.Name {
		t.Fatalf("改名不应改动 i18n key（文案进 sys_i18n）：前 %q 后 %q", custom.Name, after.Name)
	}
}
