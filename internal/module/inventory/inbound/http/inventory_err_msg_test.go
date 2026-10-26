package inventoryhttp

// inventory_err_msg_test.go — 结论「事实」的形态判据（本批收口的核心：
// 错误怎么变成一句可以给人看的话）。
//
// 被守的边界（批次约定的第四节）：
//  1. 判定只有一条（adminui.PageErrorFact → pkg/response.IsBusinessError），本模块**不再**有
//     自己的文案 key 白名单 —— 所以「新增一条 enums 业务错误却忘了登记」这种漏法在结构上不存在；
//  2. 复合错误的**分隔符是 ASCII 冒号**：写成全角「：」时页面出口切不开，整条错误会被判成
//     内部故障（用户拿到「系统内部错误」），明细里那句「被哪个商品占了」也一并丢掉 ——
//     这一条不报错、不崩，只是页面上少一句话，所以必须有会变红的判据；
//  3. 交出去的是事实（key + 中文兜底），不是 Go 拼好的整句：取词在渲染期发生。
//
// 纯逻辑，不碰数据库、不起装配：错误 → 事实的映射错一个字，页面上就少一句可行动的话。

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/i18n"
)

func TestInventoryErrMsgSplitsFacts(t *testing.T) {
	// 无词条（i18n 未初始化）：走模块兜底表 —— 这一档必须给出中文，而不是裸 key。
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	c, _ := newHXContext(t, "", "")

	// 复合错误：业务 key + ErrorDetail 明细（service 里唯一一处，见 inventory_stock.go 的
	// assertExternalSKUProductScope）。分隔符写错一个字符（全角）就退化成下面那条
	// 「全角冒号判成内部故障」的用例 —— 两条必须同时在。
	composite := fmt.Errorf("%s: %s", inventoryenums.ErrExternalSKUProductConflict,
		i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", "p-1"))
	// 同样的内容，分隔符换成全角：**不是**受支持的形态，应当落归口而不是把明细摆出来。
	fullWidth := fmt.Errorf("%s：%s", inventoryenums.ErrExternalSKUProductConflict,
		i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", "p-1"))

	for _, tc := range []struct {
		name     string
		err      error
		wantKey  string
		contains []string
		// wantInternal 命中归口事实（key = MsgInternalError + 中文原文）。
		wantInternal bool
		// wantEmpty 空事实（err 为 nil）。
		wantEmpty bool
	}{
		{
			name: "业务错误：key + 兜底表中文",
			err:  errors.New(inventoryenums.ErrWarehouseCodeTaken),
			// fallback 取自 inventoryErrFallbacks（不是把 key 原样铺出去）。
			wantKey:  inventoryenums.ErrWarehouseCodeTaken,
			contains: []string{inventoryErrFallback(inventoryenums.ErrWarehouseCodeTaken)},
		},
		{
			name:     "复合错误：key + 明细（ASCII 冒号）",
			err:      composite,
			contains: []string{inventoryErrFallback(inventoryenums.ErrExternalSKUProductConflict), "该外部编码在本仓已属于商品 p-1"},
		},
		{
			name:         "全角冒号：切不开，落归口（格式是协议的一部分）",
			err:          fullWidth,
			wantKey:      adminui.MsgInternalError,
			wantInternal: true,
		},
		{
			name:         "非业务错误（库表原文）：归口 + 原文只进日志",
			err:          errors.New(`pq: relation "inventory_warehouses" does not exist`),
			wantKey:      adminui.MsgInternalError,
			wantInternal: true,
		},
		{
			name:      "nil：空事实（不是归口文案）",
			err:       nil,
			wantEmpty: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := inventoryErrMsg(c, tc.err)
			if tc.wantEmpty {
				if !got.Empty() {
					t.Fatalf("nil 应是空事实，实际 %+v", got)
				}
				if text := inventoryErrText(c, tc.err); text != "" {
					t.Errorf("nil 的文本形态 = %q，want 空串（README：空串与归口文案必须分得开）", text)
				}
				return
			}
			if got.Key != tc.wantKey {
				t.Errorf("Key = %q，want %q", got.Key, tc.wantKey)
			}
			if tc.wantInternal {
				// 与 JSON 出口同一句归口文案（adminui.Msg 带切片，不能直接比结构体）。
				want := adminui.InternalMsg()
				if got.Key != want.Key || got.Fallback != want.Fallback {
					t.Errorf("归口事实 = %+v，want %+v", got, want)
				}
			}
			// 兜底绝不可是空串：空串会让失败在提示页上**完全不可见**（用户以为操作成功了）。
			if strings.TrimSpace(got.Fallback) == "" {
				t.Fatal("Fallback 是空串 —— 提示页会显示成空白")
			}
			for _, want := range tc.contains {
				if !strings.Contains(got.Fallback, want) {
					t.Errorf("Fallback = %q，应含 %q", got.Fallback, want)
				}
			}
			// 文本形态与事实形态同源（片段槽位用的就是它）。
			if text := inventoryErrText(c, tc.err); text != adminui.MsgText(c, got) {
				t.Errorf("文本形态 = %q，与事实形态取词结果 %q 不一致", text, adminui.MsgText(c, got))
			}
		})
	}
}

// 批量结论：调用点只给数字，取词与 `%s` 填充在渲染期做 —— 这一条同时钉住两件事：
//  1. 语言跟着**请求**走（同一个事实，两种 Accept-Language 出两种语言）；
//  2. 词条不是 `%s` 形态时**回落兜底**而不是产出垃圾：迁移 243 把
//     admin.inventory.bulk.partial / .deleted 两条 seed 成了 `%d`（中英都是），
//     TranslateArgs 会丢弃它、回落模块兜底（`%s`），于是中文界面输出逐字不变；
//     若兜底也跟着写成 `%d`、参数却是字符串，Sprintf 会给出 `%!d(string=2)`
//     这种「看起来像一句结论」的垃圾 —— 那才是真正危险的形态。
func TestInventoryBulkMsgFollowsRequestLanguage(t *testing.T) {
	entries := map[string]map[string]string{
		"admin.inventory.bulk.deleted":       {"zh-CN": "已删除 %d 个仓库", "en-US": "Deleted %d warehouses"},
		"admin.inventory.bulk.reasonDone":    {"zh-CN": "已更新 %s 个原因", "en-US": "Updated %s reason(s)"},
		"admin.inventory.bulk.reasonPartial": {"zh-CN": "已更新 %s 个原因，%s 个未能更新", "en-US": "Updated %s reason(s), %s failed"},
	}
	i18n.InjectForTest(entries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, tc := range []struct {
		name, acceptLang string
		msg              adminui.Msg
		want             string
	}{
		{"%s 词条 / 中文", "zh-CN", inventoryBulkReasonDone.WithArgs("2"), "已更新 2 个原因"},
		{"%s 词条 / 英文", "en-US", inventoryBulkReasonDone.WithArgs("2"), "Updated 2 reason(s)"},
		{"两个参数按顺序填", "zh-CN", inventoryBulkReasonPartial.WithArgs("2", "1"), "已更新 2 个原因，1 个未能更新"},
		// 旧词条是 %d：丢弃词条、回落中文兜底（这里断言的是**不掉进 Sprintf 垃圾**）。
		{"%d 词条回落兜底", "en-US", inventoryBulkWarehouseDone.WithArgs("2"), "已删除 2 个仓库"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newHXContext(t, "", "")
			c.Request.Header.Set("Accept-Language", tc.acceptLang)
			got := adminui.MsgText(c, tc.msg)
			if got != tc.want {
				t.Errorf("文案 = %q，want %q", got, tc.want)
			}
			if strings.Contains(got, "%!") || strings.Contains(got, "%s") {
				t.Errorf("文案里有未填充的占位符：%q", got)
			}
		})
	}
}

// 兜底表是「缺词条时的展示原文」，不是可透出性判据：它漏登记一个 key 时页面会显示裸 key，
// 而不是把内部细节透出去 —— 两个方向的偏差要比清楚（宁可显示得难看，不可泄漏）。
func TestInventoryErrFallbackNeverInvitesRawInternalText(t *testing.T) {
	if got := inventoryErrFallback("SomeUnknownError"); got != "SomeUnknownError" {
		t.Errorf("未登记的 key 应回落 key 本身（一眼可见），实际 %q", got)
	}
	for _, leak := range []string{"does not exist", "SQLSTATE", "pq: "} {
		if strings.Contains(inventoryErrFallback(inventoryenums.ErrWarehouseCodeTaken), leak) {
			t.Errorf("兜底表里出现了内部错误特征 %q", leak)
		}
	}
}
