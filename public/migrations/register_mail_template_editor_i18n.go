package migrations

import (
	"strings"
	"sync"
)

func registerMailTemplateEditorI18n() {
	registerMailTemplateEditorI18nOnce.Do(registerMailTemplateEditorI18nSeed)
}

var registerMailTemplateEditorI18nOnce sync.Once

var mailTemplateEditorI18nKeys = []string{
	"admin.mail.template_editor.back",
	"admin.mail.template_editor.canvas.aria",
	"admin.mail.template_editor.field.key",
	"admin.mail.template_editor.field.locale",
	"admin.mail.template_editor.field.name",
	"admin.mail.template_editor.field.subject",
	"admin.mail.template_editor.field.vars",
	"admin.mail.template_editor.heading",
	"admin.mail.template_editor.pane.attrs",
	"admin.mail.template_editor.pane.canvas",
	"admin.mail.template_editor.pane.preview",
	"admin.mail.template_editor.pane.tree",
	"admin.mail.template_editor.ph.locale",
	"admin.mail.template_editor.preview.text",
	"admin.mail.template_editor.preview.title",
	"admin.mail.template_editor.redo",
	"admin.mail.template_editor.save",
	"admin.mail.template_editor.title",
	"admin.mail.template_editor.tree.aria",
	"admin.mail.template_editor.undo",
	"admin.mail.templates.action.visual",
}

func registerMailTemplateEditorI18nSeed() {

	pairs := make([]string, 0, len(mailTemplateEditorI18nKeys)*2)
	for _, key := range mailTemplateEditorI18nKeys {
		pairs = append(pairs, "('"+key+"', 'zh-CN')", "('"+key+"', 'en-US')")
	}
	registerSeed(Seed{
		Version:   "621f-mail-template-editor-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 42 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			strings.Join(pairs, ", ") + ")",
		SQL: mustSQL("621f_mail_template_editor_i18n.sql"),
	})
}
