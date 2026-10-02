package orderhttp

// order_create_page_country_test.go — 建单页国家下拉的选项装配（纯逻辑，不碰库与模板）。
//
// 这里错的后果都是「页面上看不出来」：默认国家没被预选（运营每次手选一遍，
// 没人会认为这是 bug）、已提交的值被浏览器悄悄换成第一项（改一个字段、另一个字段跟着变）、
// 字典里没有的码被丢掉（页面显示 A，落库是 B）。

import (
	"strings"
	"testing"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
)

func TestCountryOptionViews(t *testing.T) {
	countries := []sysconfigdto.CountryOption{{Code: "CN", Label: "中国"}, {Code: "US", Label: "美国"}}
	tr := func(_, fallback string) string { return fallback }

	selectedCode := func(views []countryOptionView) string {
		for _, v := range views {
			if v.Selected {
				return v.Code
			}
		}
		return "<none>"
	}

	tests := []struct {
		name      string
		countries []sysconfigdto.CountryOption
		submitted string
		want      string
	}{
		{name: "未提交时预选站点默认国家", countries: countries, submitted: "", want: "CN"},
		{name: "已提交的值优先于默认国家", countries: countries, submitted: "US", want: "US"},
		{name: "小写提交值也命中同一项", countries: countries, submitted: "us", want: "US"},
		{name: "字典里没有的码原样补进并选中", countries: countries, submitted: "ZZ", want: "ZZ"},
		// 字典读不到时下拉只剩空项，但**已提交的值必须保住**：把它丢掉会让运营重渲后
		// 看到一个与他提交的不同的值，而库里落的仍是提交值 —— 页面与数据静默分叉。
		{name: "字典为空时提交值仍被补进并选中", countries: nil, submitted: "US", want: "US"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			views := countryOptionViews(tr, tt.countries, tt.submitted)
			if len(views) == 0 {
				t.Fatal("选项列表不能为空：模板 range 到空列表时下拉里一个选项都没有")
			}
			if got := selectedCode(views); got != tt.want {
				t.Fatalf("选中项应为 %q，实际 %q（选项 %+v）", tt.want, got, views)
			}
			if views[0].Code != "" {
				t.Fatalf("首位应是「（不填写）」空项，实际 %+v", views[0])
			}
		})
	}

	// 字典里没有的码要真的出现在选项里（不只是被选中）—— 否则页面显示的是别国的名字，落库是 ZZ。
	views := countryOptionViews(tr, countries, "ZZ")
	found := false
	for _, v := range views {
		if v.Code == "ZZ" && strings.TrimSpace(v.Label) != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("字典缺行时该码应原样作为一项（label 用码本身），实际 %+v", views)
	}
}
