package orderhttp

// return_page_handle_test.go — 退货页仓库下拉的**文案口径**测试。
//
// 只测纯函数 warehouseOptionOf：它是「哪些仓能选、选项显示成什么样」的唯一实现。
// 这层错了的代价不高但很难查 —— 下拉里少一个仓，运营只会以为「这个仓不存在」。

import (
	"testing"

	ordercontract "go_wp/internal/module/order/contract"
)

func TestWarehouseOptionOf(t *testing.T) {
	tests := []struct {
		name      string
		in        *ordercontract.ReturnWarehouse
		wantOK    bool
		wantLabel string
	}{
		{"正常仓带短码", &ordercontract.ReturnWarehouse{ID: "w1", Name: "深圳仓", Code: "SZ", Status: "enabled"}, true, "深圳仓（SZ）"},
		{"默认仓加标记", &ordercontract.ReturnWarehouse{ID: "w1", Name: "深圳仓", Code: "SZ", Status: "enabled", IsDefault: true}, true, "深圳仓（SZ） · 默认仓"},
		{"没有短码只显示名称", &ordercontract.ReturnWarehouse{ID: "w2", Name: "临时仓", Status: "enabled"}, true, "临时仓"},
		{"没有名称退到短码", &ordercontract.ReturnWarehouse{ID: "w3", Code: "HK", Status: "enabled"}, true, "HK"},
		{"停用仓不进下拉", &ordercontract.ReturnWarehouse{ID: "w4", Name: "旧仓", Status: "disabled"}, false, ""},
		{"空 id 无效", &ordercontract.ReturnWarehouse{Name: "无 id"}, false, ""},
		{"nil 无效", nil, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt, ok := warehouseOptionOf(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, want=%v", ok, tt.wantOK)
			}
			if ok && opt.Label != tt.wantLabel {
				t.Fatalf("label=%q, want=%q", opt.Label, tt.wantLabel)
			}
		})
	}
}

// TestWarehouseOptionsWithoutService 未注入 inventory 时返回空表（页面只出「默认仓」一项），
// 而不是让整页报错 —— 退货审核本身不依赖仓库列表（服务端会兜底到默认仓）。
func TestWarehouseOptionsWithoutService(t *testing.T) {
	h := &returnPageHandle{}
	if got := h.warehouseOptions(nil, "proj-1"); len(got) != 0 {
		t.Fatalf("未注入时应返回空表，实际 %+v", got)
	}
}
