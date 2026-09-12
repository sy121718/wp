package core

import (
	"strings"
	"testing"
)

func TestAlignedRepeaterRejectsDrift(t *testing.T) {
	type entry struct {
		Title string `json:"title"`
		Open  bool   `json:"open"`
	}
	type props struct {
		Items []entry `json:"items"`
		Text  string  `json:"text"`
	}
	valid := AlignedRepeaterSpec{AlignKey: "items", Field: "title", Noun: "项", Label: "标题", AddText: "添加"}
	for _, tc := range []struct {
		name string
		edit func(*AlignedRepeaterSpec)
		want string
	}{
		{"数组键拼错", func(s *AlignedRepeaterSpec) { s.AlignKey = "tabs" }, "props.tabs"},
		{"标量冒充数组", func(s *AlignedRepeaterSpec) { s.AlignKey = "text" }, "结构体数组"},
		{"文案键拼错", func(s *AlignedRepeaterSpec) { s.Field = "label" }, "[].label"},
		{"布尔冒充文案", func(s *AlignedRepeaterSpec) { s.Field = "open" }, "字符串"},
		{"附加字段不存在", func(s *AlignedRepeaterSpec) { s.Extra = []RepeaterExtra{{"opened", "展开"}} }, "[].opened"},
		{"附加字段类型错误", func(s *AlignedRepeaterSpec) { s.Extra = []RepeaterExtra{{"title", "标题"}} }, "重复"},
		{"重复附加字段", func(s *AlignedRepeaterSpec) { s.Extra = []RepeaterExtra{{"open", "展开"}, {"open", "展开"}} }, "重复"},
		{"缺少显示名", func(s *AlignedRepeaterSpec) { s.Noun = "" }, "不能为空"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := valid
			tc.edit(&spec)
			if err := ValidateAlignedRepeater(&props{}, spec); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应拒绝 %s，实际 %v", tc.want, err)
			}
		})
	}
	valid.Extra = []RepeaterExtra{{"open", "展开"}}
	if err := ValidateAlignedRepeater(&props{}, valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAlignedRepeater(nil, valid); err == nil {
		t.Fatal("缺 PropsSpec 必须失败")
	}
}

type invalidRepeaterComponent struct{}

func (*invalidRepeaterComponent) Type() string                          { return "test.invalidRepeater" }
func (*invalidRepeaterComponent) Validate(*Node, map[string]bool) error { return nil }
func (*invalidRepeaterComponent) AlignedRepeater() AlignedRepeaterSpec  { return AlignedRepeaterSpec{} }

func TestRegisterRejectsInvalidRepeater(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("声明错误必须在 Register 时失败")
		}
		if _, err := Lookup("test.invalidRepeater"); err == nil {
			t.Fatal("非法组件不得写入注册表")
		}
	}()
	Register(&invalidRepeaterComponent{})
}
