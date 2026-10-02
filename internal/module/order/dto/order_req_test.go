package orderdto

// order_req_test.go — 请求形状的纯逻辑断言：国家/地区代码的形状约束。
//
// 放在 dto 包内就近测：这条规则被两条入口共用（访客结算片段 + 后台代客建单页），
// 判错了不会报错，只会让某一条入口把 "China" 这样的串写进 VARCHAR(2) 列 ——
// 那时暴露出来的是一笔建不出来的订单，而不是「这里少了个校验」。

import "testing"

// TestNormalizeCountryCode 恰好两个 ASCII 字母才收，大写归一化，其余一律丢弃。
func TestNormalizeCountryCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "大写原样", in: "CN", want: "CN"},
		{name: "小写归一化", in: "cn", want: "CN"},
		{name: "混合大小写", in: "uS", want: "US"},
		{name: "两侧空白去掉", in: "  jp  ", want: "JP"},
		{name: "三字母码不收（列是 VARCHAR(2)）", in: "CHN", want: ""},
		{name: "国家全名不收", in: "China", want: ""},
		{name: "单字母不收", in: "C", want: ""},
		{name: "空串", in: "", want: ""},
		{name: "只有空白", in: "   ", want: ""},
		{name: "字母数字混排不收", in: "C1", want: ""},
		{name: "带连字符的码不收（不是裸 alpha-2）", in: "CN-", want: ""},
		{name: "中文名不收", in: "中国", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCountryCode(tt.in); got != tt.want {
				t.Fatalf("NormalizeCountryCode(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}
