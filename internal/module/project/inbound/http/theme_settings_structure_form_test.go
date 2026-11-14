package projecthttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	projectdto "go_wp/internal/module/project/dto"
)

func TestStructureTemplateFormValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name    string
		form    url.Values
		key     string
		current string
		want    string
	}{
		{
			name: "带哨兵 + 提交空值 = 主动解绑",
			form: url.Values{"structureTemplateFields": {"1"}, "headerTemplateId": {""}},
			key:  "headerTemplateId", current: "tpl-old", want: "",
		},
		{
			name: "带哨兵 + 提交新值 = 切换",
			form: url.Values{"structureTemplateFields": {"1"}, "headerTemplateId": {"tpl-new"}},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-new",
		},
		{
			name: "不带哨兵（旧表单 / 缺字段）= 保持原值，不清空",
			form: url.Values{},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-old",
		},
		{
			name: "不带哨兵但带了值 = 仍采用提交值（部分表单 / 脚本提交）",
			form: url.Values{"headerTemplateId": {"tpl-manual"}},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-manual",
		},
		{
			name: "带哨兵 + 空白 = 解绑（避免只删空格却当成有效绑定）",
			form: url.Values{"structureTemplateFields": {"1"}, "footerTemplateId": {"   "}},
			key:  "footerTemplateId", current: "tpl-footer", want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := structureTemplateFormValue(tc.form.Get("structureTemplateFields"), tc.form.Get(tc.key), tc.current); got != tc.want {
				t.Fatalf("structureTemplateFormValue(%q) = %q，期望 %q", tc.key, got, tc.want)
			}
		})
	}
}

// TestThemeSettingsFormReqBindsDotKeys 验证主题设置 DTO 的点号 form tag 按字面绑定。
// gin 的 FormPost binder 按 tag 字面查表，不把 colors.primary 折成嵌套路径；
// 换 binder 或改 DTO 时这里会先失败。
func TestThemeSettingsFormReqBindsDotKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	values := url.Values{
		"id":                      {"theme-1"},
		"headerBlockId":           {"blk-h"},
		"footerBlockId":           {"blk-f"},
		"slots.announcement":      {"blk-a"},
		"structureTemplateFields": {"1"},
		"headerTemplateId":        {""},
		"images.lazyLoad":         {"off"},
		"images.skeleton":         {"true"},
		"colors.primary":          {"#3d444f"},
		"typography.heading.font": {"Inter, system-ui, sans-serif"},
		"typography.body.line":    {"1.7"},
		"button.hoverBg":          {"#222222"},
		"surface.borderWidth":     {"0"},
		"motion.duration":         {"300ms"},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/settings/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Request = req
	var form projectdto.ThemeSettingsFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		t.Fatalf("点号 form tag 绑定失败: %v", err)
	}
	cases := []struct{ key, got, want string }{
		{"id", form.ID, values.Get("id")},
		{"headerBlockId", form.HeaderBlockID, values.Get("headerBlockId")},
		{"footerBlockId", form.FooterBlockID, values.Get("footerBlockId")},
		{"slots.announcement", form.AnnouncementBlockID, values.Get("slots.announcement")},
		{"structureTemplateFields", form.StructureTemplateFields, values.Get("structureTemplateFields")},
		{"headerTemplateId", form.HeaderTemplateID, values.Get("headerTemplateId")},
		{"images.lazyLoad", form.ImagesLazyLoad, values.Get("images.lazyLoad")},
		{"images.skeleton", form.ImagesSkeleton, values.Get("images.skeleton")},
		{"colors.primary", form.ColorPrimary, values.Get("colors.primary")},
		{"typography.heading.font", form.HeadingFontFamily, values.Get("typography.heading.font")},
		{"typography.body.line", form.BodyLineHeight, values.Get("typography.body.line")},
		{"button.hoverBg", form.ButtonHoverBackground, values.Get("button.hoverBg")},
		{"surface.borderWidth", form.SurfaceBorderWidth, values.Get("surface.borderWidth")},
		{"motion.duration", form.MotionTransitionDuration, values.Get("motion.duration")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("字段 %s 绑定为 %q，期望 %q", tc.key, tc.got, tc.want)
		}
	}
}

// 槽位模板绑定用 slotTemplateSlot / slotTemplateID 两个静态同名数组，按顺序配对。
func TestThemeSlotTemplateFormValuesPairsStaticArrays(t *testing.T) {
	form := &projectdto.ThemeSettingsFormReq{
		SlotTemplateSlot: []string{"announcement", "footer_extra"},
		SlotTemplateID:   []string{"tpl-a", "tpl-b"},
	}
	got, err := themeSlotTemplateFormValues(form, nil)
	if err != nil {
		t.Fatalf("合法配对不应失败：%v", err)
	}
	if got["announcement"] != "tpl-a" || got["footer_extra"] != "tpl-b" {
		t.Fatalf("槽位与模板串位：%+v", got)
	}
}

// 长度不一致 / 非法槽位名 / 重复槽位都必须拒绝，旧设置不变。
func TestThemeSlotTemplateFormValuesRejectsMalformed(t *testing.T) {
	current := map[string]string{"announcement": "existing"}
	cases := map[string]*projectdto.ThemeSettingsFormReq{
		"长度不一致": {SlotTemplateSlot: []string{"a"}, SlotTemplateID: []string{"1", "2"}},
		"非法槽位名": {SlotTemplateSlot: []string{"../evil"}, SlotTemplateID: []string{"1"}},
		"大写槽位名": {SlotTemplateSlot: []string{"Announcement"}, SlotTemplateID: []string{"1"}},
		"重复槽位":  {SlotTemplateSlot: []string{"a", "a"}, SlotTemplateID: []string{"1", "2"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := themeSlotTemplateFormValues(form, current); err == nil {
				t.Fatal("畸形槽位输入必须拒绝")
			}
		})
	}
}

// 没有提交槽位时保留库里已有绑定；空 id 表示清空该槽位。
func TestThemeSlotTemplateFormValuesKeepsOrClears(t *testing.T) {
	current := map[string]string{"announcement": "existing"}
	if got, err := themeSlotTemplateFormValues(&projectdto.ThemeSettingsFormReq{}, current); err != nil || got["announcement"] != "existing" {
		t.Fatalf("未提交槽位应保留现有绑定：%+v err=%v", got, err)
	}
	got, err := themeSlotTemplateFormValues(&projectdto.ThemeSettingsFormReq{
		SlotTemplateSlot: []string{"announcement"}, SlotTemplateID: []string{""},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["announcement"]; !ok || v != "" {
		t.Fatalf("空 id 应表示清空该槽位：%+v", got)
	}
}
