package inventoryhttp

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

	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	c, _ := newHXContext(t, "", "")

	composite := fmt.Errorf("%s: %s", inventoryenums.ErrExternalSKUProductConflict,
		i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", "p-1"))

	fullWidth := fmt.Errorf("%s：%s", inventoryenums.ErrExternalSKUProductConflict,
		i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", "p-1"))

	for _, tc := range []struct {
		name     string
		err      error
		wantKey  string
		contains []string

		wantInternal bool

		wantEmpty bool
	}{
		{
			name: "业务错误：key + 兜底表中文",
			err:  errors.New(inventoryenums.ErrWarehouseCodeTaken),

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

				want := adminui.InternalMsg()
				if got.Key != want.Key || got.Fallback != want.Fallback {
					t.Errorf("归口事实 = %+v，want %+v", got, want)
				}
			}

			if strings.TrimSpace(got.Fallback) == "" {
				t.Fatal("Fallback 是空串 —— 提示页会显示成空白")
			}
			for _, want := range tc.contains {
				if !strings.Contains(got.Fallback, want) {
					t.Errorf("Fallback = %q，应含 %q", got.Fallback, want)
				}
			}

			if text := inventoryErrText(c, tc.err); text != adminui.MsgText(c, got) {
				t.Errorf("文本形态 = %q，与事实形态取词结果 %q 不一致", text, adminui.MsgText(c, got))
			}
		})
	}
}

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
