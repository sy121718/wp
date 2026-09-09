package form

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// textField 构造一个合法的 text 字段（测试用快捷方式）。
func textField(name string) FormField {
	return FormField{Type: FieldText, Label: "文本", Name: name}
}

// TestValidateExtra 表单校验：字段白名单、表单键白名单、select 选项约束、文本长度防注入。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"单 text 字段合法", &Props{Fields: []FormField{textField("username")}}, false},
		{"email 字段合法", &Props{Fields: []FormField{{Type: FieldEmail, Label: "邮箱", Name: "email", Placeholder: "you@example.com"}}}, false},
		{"textarea 字段合法", &Props{Fields: []FormField{{Type: FieldTextarea, Label: "留言", Name: "message"}}}, false},
		{"checkbox 字段合法", &Props{Fields: []FormField{{Type: FieldCheckbox, Label: "同意条款", Name: "agree", Required: true}}}, false},
		{"select 带选项合法", &Props{Fields: []FormField{{Type: FieldSelect, Label: "城市", Name: "city", Options: []string{"北京", "上海"}}}}, false},
		{"空字段列表拒绝", &Props{}, true},
		{"非法字段类型拒绝", &Props{Fields: []FormField{{Type: "file", Label: "上传", Name: "up"}}}, true},
		{"空标签拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "", Name: "a"}}}, true},
		{"标签超长拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: strings.Repeat("字", 101), Name: "a"}}}, true},
		{"空 name 拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "文本", Name: ""}}}, true},
		{"数字开头 name 拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "文本", Name: "1abc"}}}, true},
		{"特殊字符 name 拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "文本", Name: "a-b"}}}, true},
		{"name 超长拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "文本", Name: "a" + strings.Repeat("b", 50)}}}, true},
		{"占位提示超长拒绝", &Props{Fields: []FormField{{Type: FieldText, Label: "文本", Name: "a", Placeholder: strings.Repeat("字", 201)}}}, true},
		{"select 无选项拒绝", &Props{Fields: []FormField{{Type: FieldSelect, Label: "城市", Name: "city"}}}, true},
		{"select 选项超量拒绝", &Props{Fields: []FormField{{Type: FieldSelect, Label: "城市", Name: "city", Options: repeatStrings("选", 21)}}}, true},
		{"select 选项超长拒绝", &Props{Fields: []FormField{{Type: FieldSelect, Label: "城市", Name: "city", Options: []string{strings.Repeat("字", 101)}}}}, true},
		{"字段数量超上限拒绝", &Props{Fields: repeatFields(21)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "f1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// repeatStrings 生成 n 个相同字符串的切片。
func repeatStrings(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// repeatFields 生成 n 个合法字段（测试数量上限用）。
func repeatFields(n int) []FormField {
	out := make([]FormField, n)
	for i := range out {
		out[i] = FormField{Type: FieldText, Label: "文本", Name: "f" + strings.Repeat("a", 1)}
	}
	return out
}

// TestBuildView 字段转视图：method/submitLabel 缺省回退、字段映射与顺序。
func TestBuildView(t *testing.T) {
	p := &Props{
		Fields: []FormField{
			{Type: FieldText, Label: "姓名", Name: "name", Placeholder: "请输入", Required: true},
			{Type: FieldSelect, Label: "城市", Name: "city", Options: []string{"北京", "上海"}},
		},
	}
	v := BuildView(p)
	if v.Method != "post" {
		t.Fatalf("Method 缺省应为 post，got %q", v.Method)
	}
	if v.SubmitLabel != "提交" {
		t.Fatalf("SubmitLabel 缺省应为「提交」，got %q", v.SubmitLabel)
	}
	if len(v.Fields) != 2 {
		t.Fatalf("字段数量 = %d, 期望 2", len(v.Fields))
	}
	if v.Fields[0].Name != "name" || !v.Fields[0].Required || v.Fields[0].Placeholder != "请输入" {
		t.Fatalf("字段 0 映射错误: %+v", v.Fields[0])
	}
	if len(v.Fields[1].Options) != 2 || v.Fields[1].Options[0] != "北京" {
		t.Fatalf("字段 1 选项映射错误: %+v", v.Fields[1])
	}

	// 显式 method 与 submitLabel 透传。
	p2 := &Props{Fields: []FormField{textField("a")}, Method: "get", SubmitLabel: "发送", Action: "/contact"}
	v2 := BuildView(p2)
	if v2.Method != "get" || v2.SubmitLabel != "发送" || v2.Action != "/contact" {
		t.Fatalf("显式 method/submitLabel/action 透传失败: %+v", v2)
	}
}

// TestCompileCSS 样式编译：确定性 + 关键声明存在。
func TestCompileCSS(t *testing.T) {
	p := &Props{Fields: []FormField{textField("a")}}
	b1 := &core.CSSBuckets{}
	compileCSS("f1", p, b1)
	css1 := b1.String()

	// 确定性：二次编译字节一致。
	b2 := &core.CSSBuckets{}
	compileCSS("f1", p, b2)
	if b2.String() != css1 {
		t.Fatalf("compileCSS 非确定性输出:\n--- 首次 ---\n%s\n--- 二次 ---\n%s", css1, b2.String())
	}

	wants := []string{
		"flex-direction: column",
		"gap: 16px",
		".wp-form-field",
		"border: 1px solid rgba(0,0,0,.15)",
		"border-radius: 6px",
		".wp-form-submit",
		"background: var(--wp-btn-bg, var(--wp-c-primary, #2563eb))",
		"textarea",
		"select",
		"resize: vertical",
		".wp-form-check",
	}
	for _, want := range wants {
		if !strings.Contains(css1, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css1)
		}
	}
}

// renderCtx 模板渲染上下文（字段名与 builder/nodeView 对齐：Classes/CustomID/V）。
type renderCtx struct {
	Classes  string
	CustomID string
	V        View
}

// TestFormTemplateRender 渲染 form.jet 模板：覆盖全部字段类型 + 转义。
func TestFormTemplateRender(t *testing.T) {
	p := &Props{
		Method:      "post",
		Action:      "/submit",
		SubmitLabel: "提交表单",
		Fields: []FormField{
			{Type: FieldText, Label: "姓名 & <称呼>", Name: "name", Placeholder: "请输入姓名", Required: true},
			{Type: FieldEmail, Label: "邮箱", Name: "email", Placeholder: "you@example.com"},
			{Type: FieldTextarea, Label: "留言", Name: "message", Placeholder: "说点什么"},
			{Type: FieldSelect, Label: "城市", Name: "city", Options: []string{"北京", "上海 & 广州"}},
			{Type: FieldCheckbox, Label: "同意 <条款> & 隐私", Name: "agree", Required: true},
		},
	}
	view := BuildView(p)

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("NewEmbeddedComponentSet: %v", err)
	}
	tpl, err := set.GetTemplate("form")
	if err != nil {
		t.Fatalf("GetTemplate(form): %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, nil, renderCtx{Classes: "wp-c-f1", CustomID: "contact-form", V: view}); err != nil {
		t.Fatalf("渲染 form 模板失败: %v", err)
	}
	got := buf.String()

	wants := []string{
		`<form class="wp-c-f1" method="post" id="contact-form" action="/submit">`,
		`<input type="text" name="name" placeholder="请输入姓名" required>`,
		`<input type="email" name="email" placeholder="you@example.com">`,
		`<textarea name="message" placeholder="说点什么"></textarea>`,
		`<select name="city">`,
		`<option value="北京">北京</option>`,
		`<option value="上海 &amp; 广州">上海 &amp; 广州</option>`,
		`<input type="checkbox" name="agree" required>`,
		`<button type="submit" class="wp-form-submit">提交表单</button>`,
		`</form>`,
		// 标签特殊字符转义。
		`姓名 &amp; &lt;称呼&gt;`,
		`同意 &lt;条款&gt; &amp; 隐私`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("渲染输出缺少 %q\n%s", want, got)
		}
	}
	// 防注入：原始未转义的危险字符不应出现。
	for _, bad := range []string{`<称呼>`, `<条款>`, "上海 & 广州"} {
		if strings.Contains(got, bad) {
			t.Errorf("渲染输出含未转义内容 %q\n%s", bad, got)
		}
	}
}
