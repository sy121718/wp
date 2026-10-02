package orderhttp

// order_page_labels_test.go — 地址行拼装的形状断言（迁移 501 的国家/地区排在最前）。
//
// 为什么断言这个：国家是**可选**列（存量订单与非中国站点都是空串），
// 拼接时漏掉「空段跳过」就会在每一条没有国家的地址前面留一个多余空格 ——
// 页面照常渲染、没人会为此报错，只有断言能看见。

import "testing"

func TestOrderAddressLabelPutsCountryFirst(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{
			name: "国家排在最前",
			in:   []string{"CN", "张三", "13800000000", "广东省", "深圳市", "南山区", "某某路 1 号", "518000"},
			want: "CN 张三 13800000000 广东省 深圳市 南山区 某某路 1 号 518000",
		},
		{
			name: "没有国家时整段跳过（不留前导空格）",
			in:   []string{"", "张三", "深圳市"},
			want: "张三 深圳市",
		},
		{
			name: "国家两侧空白也按空段处理",
			in:   []string{"  ", "深圳市"},
			want: "深圳市",
		},
		{
			name: "全空返回空串（模板据此显示「未填写」）",
			in:   []string{"", "", ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orderAddressLabel(tt.in...); got != tt.want {
				t.Fatalf("orderAddressLabel(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}
