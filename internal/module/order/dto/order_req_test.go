package orderdto

import "testing"

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
