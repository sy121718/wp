package templates

import (
	"bytes"
	"testing"

	"go_wp/pkg/facing"
)

// 内容模板列表页必须能用「错误事实」渲染：字段从成品句改成 Msg 后，
// 模板的字段访问与取词分支要真的跑得通（不能因为方法不识别而整页中断）。
// 取词函数由 InjectGlobalsForTest 注入全局 t/tf，这里不重复给。
func TestContentTemplatesRendersMessageFacts(t *testing.T) {
	set := newAdminTitleSet(t)
	var buf bytes.Buffer
	data := map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"), "csrf_token": "tok",
		"tr": func(k, f string) string { return f },
		"t":  func(k, f string) string { return f },
		"tf": func(k, f string, a []string) string { return f },
		"Title": "MsgContentTemplatesTitle", "Menu": "content-templates",
		"Projects": []any{}, "SelectedProject": "", "EntityType": "",
		"Err": facing.Msg{Key: "hint.key", Fallback: "出错了"},
		"Ready": true, "Templates": []any{}, "TemplateCount": 0,
		"ImpactAvailable": false,
		"ImpactNote": facing.Msg{Key: "impact.key", Fallback: "影响面不完整", Args: []string{"2"}},
		"NavGroups": []any{}, "Buttons": map[string]bool{},
	}
	tmpl, err := set.GetTemplate("admin/contenttemplate/content_templates.html")
	if err != nil {
		t.Fatalf("加载模板失败：%v", err)
	}
	if err := tmpl.Execute(&buf, nil, data); err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
}
