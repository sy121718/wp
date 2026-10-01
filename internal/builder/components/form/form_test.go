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
		".sky-form-field",
		"border: 1px solid var(--sky-c-border-strong, #d1d5db)",
		"border-radius: 6px",
		".sky-form-submit",
		"background: var(--sky-btn-bg, var(--sky-c-primary, #2563eb))",
		"textarea",
		"select",
		"resize: vertical",
		".sky-form-check",
	}
	for _, want := range wants {
		if !strings.Contains(css1, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css1)
		}
	}
}

// renderCtx 模板渲染上下文（字段名与 builder/nodeView 对齐：Classes/CustomID/NodeID/V）。
// NodeID 是控件 id 的前缀：label[for] 与控件 id 由「节点 ID + 字段 name」拼成，
// 保证同页多表单不撞 id（此前 label 与控件完全没有关联）。
type renderCtx struct {
	Classes  string
	CustomID string
	NodeID   string
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
	if err := tpl.Execute(&buf, nil, renderCtx{Classes: "sky-c-f1", CustomID: "contact-form", NodeID: "f1", V: view}); err != nil {
		t.Fatalf("渲染 form 模板失败: %v", err)
	}
	got := buf.String()

	wants := []string{
		`<form class="sky-c-f1" method="post" id="contact-form" action="/submit">`,
		// label[for] 与控件 id 必须成对：读屏靠它把「姓名」和输入框关联起来。
		`<label for="sky-form-f1-name">姓名 &amp; &lt;称呼&gt;</label>`,
		`<input id="sky-form-f1-name" type="text" name="name" placeholder="请输入姓名" required>`,
		`<input id="sky-form-f1-email" type="email" name="email" placeholder="you@example.com">`,
		`<textarea id="sky-form-f1-message" name="message" placeholder="说点什么"></textarea>`,
		// data-ui-select：构建期据此刻断该产物要不要内联「原始控件基座」的下拉替身
		// （原生 select 的弹层在部分桌面环境行为异常，见 js/ui/select.js）。
		`<select data-ui-select id="sky-form-f1-city" name="city">`,
		`<option value="北京">北京</option>`,
		`<option value="上海 &amp; 广州">上海 &amp; 广州</option>`,
		`<input type="checkbox" name="agree" required>`,
		`<button type="submit" class="sky-form-submit">提交表单</button>`,
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
	// 关联完整性：每个 label 的 for 都能在产物里找到同名 id（漏一个就是读屏读不出字段名）。
	for _, seg := range strings.Split(got, "for=\"")[1:] {
		id := seg[:strings.Index(seg, "\"")]
		if !strings.Contains(got, `id="`+id+`"`) {
			t.Errorf("label for=%q 找不到对应控件 id", id)
		}
	}
	// 防注入：原始未转义的危险字符不应出现。
	for _, bad := range []string{`<称呼>`, `<条款>`, "上海 & 广州"} {
		if strings.Contains(got, bad) {
			t.Errorf("渲染输出含未转义内容 %q\n%s", bad, got)
		}
	}
}
