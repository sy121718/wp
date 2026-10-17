package productdto

// product_form_bind_test.go — 表单字段绑定语义的判据（每条都是「改了就变红」的语义锚）。
//
// 为什么值得单独一大张表：这批的验收要求是「空值 / 零值 / 非法数字必须有测试，且测试要能变红」。
// 表单绑定最贵的失败模式是**静默改语义** —— 把「空串」当成「明确填了 0」、把非法数字
// 吞成缺省、把 NaN 放进金额。三者都不报错、页面看起来也正常，只有运营有一天发现
// 「价格莫名变成 0」时才会有人回头查。

import (
	"errors"
	"math"
	"testing"
)

// TestOptFloatSemantics 可选浮点字段的四档语义：未提交 / 0 / 负数 / 非法。
func TestOptFloatSemantics(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantNil   bool
		wantValue float64
		wantErr   bool
	}{
		{name: "未提交（空串）与不填等价", raw: "", wantNil: true},
		{name: "纯空白与空串等价", raw: "   ", wantNil: true},
		{name: "0 是有效值（不是缺省）", raw: "0", wantValue: 0},
		{name: "0.00 同样有效", raw: "0.00", wantValue: 0},
		{name: "正整数原样", raw: "128", wantValue: 128},
		{name: "小数原样（精度不由绑定层截断）", raw: "12.345", wantValue: 12.345},
		{name: "科学计数法可解析", raw: "1e3", wantValue: 1000},
		{name: "负数原样放行（是否允许由 service 判）", raw: "-7.5", wantValue: -7.5},
		{name: "带空白能解析", raw: " 9 ", wantValue: 9},
		{name: "非数字非法", raw: "abc", wantErr: true},
		{name: "半截数字非法", raw: "12px", wantErr: true},
		{name: "NaN 非法（不是数字）", raw: "NaN", wantErr: true},
		{name: "正无穷非法", raw: "Inf", wantErr: true},
		{name: "正无穷（带符号）非法", raw: "+Inf", wantErr: true},
		{name: "负无穷非法", raw: "-Inf", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := OptFloat("defaultPrice", tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("OptFloat(%q) 应返回绑定错误，实际 got=%v err=nil", tc.raw, deref(got))
				}
				var fe *FieldValueError
				if !errors.As(err, &fe) {
					t.Fatalf("错误必须是 *FieldValueError（类型事实），实际 %T", err)
				}
				if fe.Field != "defaultPrice" || fe.Kind != FieldKindNumber {
					t.Fatalf("错误事实 = {field:%q kind:%q}，want {defaultPrice number}", fe.Field, fe.Kind)
				}
				if got != nil {
					t.Fatalf("非法值不该同时给出结果指针，实际 %v", *got)
				}
				return
			}
			if err != nil {
				t.Fatalf("OptFloat(%q) 不该报错：%v", tc.raw, err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("未提交应得到 nil（= 本次不改），实际 %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("OptFloat(%q) = nil，want %v —— 空串与 0 的区分被吃掉了", tc.raw, tc.wantValue)
			}
			if *got != tc.wantValue {
				t.Fatalf("OptFloat(%q) = %v，want %v", tc.raw, *got, tc.wantValue)
			}
		})
	}
}

// TestOptIntSemantics 可选整数字段：0 有效、负数放行、小数**不截断**而是非法。
func TestOptIntSemantics(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantNil   bool
		wantValue int
		wantErr   bool
	}{
		{name: "空串 = 未提交", raw: "", wantNil: true},
		{name: "0 是有效值", raw: "0", wantValue: 0},
		{name: "负数放行", raw: "-3", wantValue: -3},
		{name: "带空白能解析", raw: " 42 ", wantValue: 42},
		{name: "非数字非法", raw: "abc", wantErr: true},
		{name: "小数非法（不悄悄截断）", raw: "1.5", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := OptInt("maxOptions", tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("OptInt(%q) 应返回绑定错误", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("OptInt(%q) 不该报错：%v", tc.raw, err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("未提交应得到 nil，实际 %v", *got)
				}
				return
			}
			if got == nil || *got != tc.wantValue {
				t.Fatalf("OptInt(%q) = %v，want %v", tc.raw, deref(got), tc.wantValue)
			}
		})
	}
}

// TestFormIntSemantics 非可选整数：空串取默认值；非法取默认值**并**报错事实（由调用方决定报不报）。
func TestFormIntSemantics(t *testing.T) {
	if v, err := FormInt("minTotalQty", "", 1); v != 1 || err != nil {
		t.Fatalf("空串应取默认值 1 且不报错，实际 v=%d err=%v", v, err)
	}
	if v, err := FormInt("minTotalQty", "0", 1); v != 0 || err != nil {
		t.Fatalf("显式 0 必须胜过默认值，实际 v=%d err=%v", v, err)
	}
	v, err := FormInt("minTotalQty", "abc", 1)
	if v != 1 || err == nil {
		t.Fatalf("非法应同时给出默认值与错误事实，实际 v=%d err=%v", v, err)
	}
	var fe *FieldValueError
	if !errors.As(err, &fe) || fe.Field != "minTotalQty" {
		t.Fatalf("错误事实应带字段名 minTotalQty，实际 %v", err)
	}
}

// TestFormTextTrims 文本字段：未提交与空串等价（都得到空串，由调用方按字段语义决定不改/清空）。
func TestFormTextTrims(t *testing.T) {
	if got := FormText(""); got != "" {
		t.Fatalf("空串应得到空串，实际 %q", got)
	}
	if got := FormText("   "); got != "" {
		t.Fatalf("纯空白应归一成空串（未提交与空白等价），实际 %q", got)
	}
	if got := FormText(" a b "); got != "a b" {
		t.Fatalf("应只去首尾空白，实际 %q", got)
	}
}

// TestFieldValueErrorIsTypeFact 错误是**类型事实**：判据不靠比对原文（改措辞不该影响判定）。
func TestFieldValueErrorIsTypeFact(t *testing.T) {
	_, err := OptFloat("price", "abc")
	var fe *FieldValueError
	if !errors.As(err, &fe) {
		t.Fatalf("optFloat 的错误必须能被 errors.As 取到 *FieldValueError，实际 %T", err)
	}
	if got := fe.Error(); got == "" || got == "abc" {
		t.Fatalf("Error() 应给出「字段 + 种类」的定位串（只进日志），实际 %q", got)
	}
	var nilErr *FieldValueError
	if nilErr.Error() != "" {
		t.Fatalf("nil 接收者不该 panic，实际 %q", nilErr.Error())
	}
}

// TestNonFiniteRejected 归口判据：NaN / ±Inf 一律非法（防「后续所有比较都失效」）。
func TestNonFiniteRejected(t *testing.T) {
	for _, raw := range []string{"NaN", "Inf", "-Inf", "+Inf", "nan", "inf"} {
		if _, err := OptFloat("weight", raw); err == nil {
			t.Fatalf("OptFloat(%q) 应判非法（非有限数不能进金额 / 重量）", raw)
		}
	}
	if !isNotFinite(math.NaN()) || !isNotFinite(math.Inf(1)) || !isNotFinite(math.Inf(-1)) {
		t.Fatalf("isNotFinite 必须同时认出 NaN 与正负无穷")
	}
	if isNotFinite(0) || isNotFinite(-1.5) {
		t.Fatalf("有限数不该被判成非有限数")
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
