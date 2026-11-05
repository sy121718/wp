package templates

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// 这两页曾经整页 500：data 是 struct 视图（adminui.ViewMap 反射转 map），
// 键名默认取字段名，只有字段带 view tag 才换名；模板取的是 .SelectedProject /
// .ThemeSettings。字段名与模板键名一旦漂移，Jet 在缺键处直接 Runtime Error
// 中断渲染，页面变成 500 通用文案。
//
// 为什么拆成两段而不是一条端到端：
// internal/templates 不能 import internal/adminui —— pkg/view/jet 与
// internal/pipeline 都 import 本包，而 adminui 依赖 pkg/view/jet，包内测试
// import adminui 会构成 import cycle（得到的是 cycle 报错，不是渲染结果）。
// 所以：
//  1. 本文件用**与生产同名同 tag 的 struct** 造 data 走真实 Jet 渲染，证明模板
//     引用的键确实是它要的名字、整页渲染不中断；
//  2. 最后一条防漂移自检读模板源码，断言模板引用的每个大写键都能在夹具
//     struct（经 view tag 转换）里找到 —— 生产 struct 加字段而夹具没跟上、
//     或模板改了引用名，这条会立刻红，而不是等到整页 500；
//  3. tag → 键名这条规则本身由 internal/adminui 对着真实 ViewMap 断言
//     （adminui/prepare_theme_view_keys_test.go）。
//
// 夹具必须照抄生产 struct 的**全部**字段（含子结构），否则模板走到某个夹具没抄的
// 字段就断——这正是这条测试第一次写少字段时的失败形态。

// ---- 主题管理页（生产 themeManageData）----

type themeManageViewData struct {
	Title             string
	Menu              string
	Projects          []themeViewProject
	SelectedProjectID string `view:"SelectedProject"`
	Themes            []themeRowView

	Err            facingMsgView
	NoProjectEmpty bool
}

type themeViewProject struct {
	ID   string
	Name string
}

type themeRowView struct {
	ID        string
	ProjectID string
	Name      string
	IsActive  bool
	CreatedAt string
	UpdatedAt string
}

// facingMsgView 对应 facing.Msg（Key / Fallback / Args / Named）。
type facingMsgView struct {
	Key      string
	Fallback string
	Args     []string
	Named    map[string]string
}

// ---- 主题设置页（生产 themeSettingsData，字段照抄 project_page.go）----

type themeSettingsViewData struct {
	Title     string
	Menu      string
	ThemeID   string
	ThemeName string
	ProjectID string

	PColor     string
	TColor     string
	BgColor    string
	SColor     string
	BdColor    string
	FontFamily string

	ThemeSettingsJSON string `view:"ThemeSettings"`

	Groups []themeFieldGroupView

	HeaderBlockID string
	FooterBlockID string

	HeaderBlocks []blockOptionView
	FooterBlocks []blockOptionView

	AnnouncementBlocks  []blockOptionView
	AnnouncementBlockID string

	HeaderTemplateID string
	FooterTemplateID string

	SlotTemplates map[string]string

	HeaderTemplateOptions []structureTemplateOptionView
	FooterTemplateOptions []structureTemplateOptionView

	Err facingMsgView
}

type themeFieldGroupView struct {
	TitleKey      string
	TitleFallback string
	Fields        []themeFieldView
}

type themeFieldView struct {
	LabelKey      string
	LabelFallback string

	Path    string
	Name    string
	Kind    string
	Value   string
	Options []panelOptionView

	Invalid       bool
	ErrorKey      string
	ErrorFallback string
}

type panelOptionView struct {
	Value         string
	LabelKey      string
	LabelFallback string
	Selected      bool
}

type blockOptionView struct {
	ID string

	NameKey      string
	NameFallback string
	Kind         string
}

type structureTemplateOptionView struct {
	ID string

	Name string

	SuffixKey      string
	SuffixFallback string
	Selected       bool
}

// viewMapProbe 只复刻「字段名 / view tag 取哪个」这一条规则，与 adminui.ViewMap
// 同一语义。规则本身另有 internal/adminui 的用例对着真实 ViewMap 断言，这里不
// 复制第二套判断逻辑。
func viewMapProbe(t *testing.T, view any) map[string]any {
	t.Helper()
	out := map[string]any{}
	rv := reflect.ValueOf(view)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return out
		}
		rv = rv.Elem()
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		name := field.Name
		if tag := field.Tag.Get("view"); tag != "" {
			name = tag
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}

func themeRenderData(t *testing.T, view any) map[string]any {
	t.Helper()
	data := adminShellData()
	data["Buttons"] = map[string]bool{}
	for k, v := range viewMapProbe(t, view) {
		data[k] = v
	}
	return data
}

func TestThemeManagePageRendersWithStructViewKeys(t *testing.T) {
	view := &themeManageViewData{
		Title: "主题管理", Menu: "themes",
		Projects:          []themeViewProject{{ID: "p1", Name: "官网"}, {ID: "p2", Name: "商城"}},
		SelectedProjectID: "p2",
		Themes: []themeRowView{{
			ID: "t-1", ProjectID: "p2", Name: "默认主题", IsActive: true,
			CreatedAt: "2026-01-02 03:04", UpdatedAt: "2026-01-03 04:05",
		}},
		Err: facingMsgView{Key: "admin.theme.err.sample", Fallback: "示例失败", Args: []string{"x"}},
	}
	data := themeRenderData(t, view)

	if _, ok := data["SelectedProject"]; !ok {
		t.Fatalf("data 缺少 SelectedProject 键 —— 模板会整页中断；现有键：%v", sortedKeysOf(data))
	}
	if _, ok := data["SelectedProjectID"]; ok {
		t.Errorf("data 里不该同时出现字段名 SelectedProjectID（模板取不到它）")
	}

	out, err := render(t, newAdminTestSet(), "admin/project/theme", data)
	if err != nil {
		t.Fatalf("主题管理页渲染失败（data 键名与模板引用对不上）: %v", err)
	}
	for _, want := range []string{
		`<h1>主题管理</h1>`,
		`class="data-table"`,
		`<strong>默认主题</strong>`,
		`href="/admin/themes/settings?id=t-1"`,
		`<option value="p2" selected>商城</option>`,
		`</html>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("主题管理页缺少 %q", want)
		}
	}
}

func TestThemeSettingsPageRendersWithStructViewKeys(t *testing.T) {
	view := &themeSettingsViewData{
		Title: "主题设置", Menu: "themes",
		ThemeID: "t-1", ThemeName: "默认主题", ProjectID: "p1",
		ThemeSettingsJSON: `{"colors":{"primary":"#123456"}}`,
		// 夹具必须是**非空真实样例**：防漂移自检要递归收集键，空容器（空切片 /
		// 零值 Msg）递归下去什么都拿不到，{{range .Groups}}{{.Fields}}{{end}} 与
		// {{if .Err.Key}} 里的内部字段就会被误判成缺失。所以每个容器都给一个元素。
		Groups: []themeFieldGroupView{{
			TitleKey: "admin.theme_settings.group.colors", TitleFallback: "颜色",
			Fields: []themeFieldView{{
				LabelKey: "admin.theme_settings.field.pcolor", LabelFallback: "主色",
				Path: "colors.primary", Name: "colors.primary", Kind: "color", Value: "#123456",
				Options: []panelOptionView{{
					Value: "#123456", LabelKey: "admin.theme_settings.option.sample", LabelFallback: "示例",
				}},
				Invalid:       true,
				ErrorKey:      "admin.theme_settings.field.pcolor.invalid",
				ErrorFallback: "主色取值不合法",
			}},
		}},
		HeaderBlocks:          []blockOptionView{{ID: "b-1", NameKey: "页眉块", NameFallback: "页眉块", Kind: "header"}},
		FooterBlocks:          []blockOptionView{{ID: "b-2", NameKey: "页脚块", NameFallback: "页脚块", Kind: "footer"}},
		AnnouncementBlocks:    []blockOptionView{{ID: "b-3", NameKey: "公告块", NameFallback: "公告块", Kind: "announcement"}},
		HeaderTemplateOptions: []structureTemplateOptionView{{ID: "st-1", Name: "结构模板", SuffixKey: "k", SuffixFallback: "（当前生效）", Selected: true}},
		FooterTemplateOptions: []structureTemplateOptionView{{ID: "st-2", Name: "结构模板二", SuffixKey: "k2", SuffixFallback: "（当前生效）", Selected: true}},
		SlotTemplates:         map[string]string{"checkout": "st-1"},
		Err:                   facingMsgView{Key: "admin.theme_settings.err.sample", Fallback: "示例失败", Args: []string{"x"}},
	}
	data := themeRenderData(t, view)

	if _, ok := data["ThemeSettings"]; !ok {
		t.Fatalf("data 缺少 ThemeSettings 键 —— 模板会整页中断；现有键：%v", sortedKeysOf(data))
	}
	if _, ok := data["ThemeSettingsJSON"]; ok {
		t.Errorf("data 里不该同时出现字段名 ThemeSettingsJSON（模板取不到它）")
	}

	out, err := render(t, newAdminTestSet(), "admin/project/theme_settings", data)
	if err != nil {
		t.Fatalf("主题设置页渲染失败（data 键名与模板引用对不上）: %v", err)
	}
	for _, want := range []string{
		`id="theme-settings-json"`,
		`{"colors":{"primary":"#123456"}}`,
		`id="theme-settings-form"`,
		`</html>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("主题设置页缺少 %q", want)
		}
	}
}

func sortedKeysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
