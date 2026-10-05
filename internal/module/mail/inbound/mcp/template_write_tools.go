package mailmcp

// template_write_tools.go — 邮件模板的读全文 / 保存 / 删除。
//
// 模板的身份是 **(templateKey, locale) 这一对**，不是 id —— 这是本组最容易搞错的地方：
// 同一个 key 有中英两版，删错 locale 会删掉另一版。所以 delete 与 get 都要两个参数，
// 而 template_save 是 **upsert**（同 key 同 locale 直接覆盖），不是「新建」。
//
// 本组**没有「用模板发一封」**：service 的 SendTemplate 没有 HTTP 路由，
// 因此没有对应权限点，而工具权限是 fail closed 的。要发信走群发活动。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/permission"
)

// TemplateWriter 邮件模板的写能力（给 AI 工具的窄门）。
//
// 刻意不含 SendTransactional（不带模板的一次性发信）与 automation 一整族
// （自动化的正确性取决于触发条件与收件范围，属于「配一条链路」而不是「改一封信」）。
type TemplateWriter interface {
	UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (*maildto.TemplateItem, error)
	DeleteTemplate(ctx context.Context, key, locale string) error
}

// TemplateReader 取模板全文（清单工具只回名称与变量，正文可能很长）。
//
// 复用 ListTemplates 而不是另开一个 GetTemplate：service 层没有单取的入口
// （只有 model 层有），而模板总量是个位数，按 key 过滤一次就够了。
type TemplateReader interface {
	ListTemplates(ctx context.Context, key string) ([]*maildto.TemplateItem, error)
}

// TemplateTools 返回邮件模板工具集（2 写 + 1 读全文）。
//
// **刻意不含「用模板发一封」**：service 里确实有 SendTemplate，但它没有任何
// HTTP 路由，因此也没有对应的权限点 —— 而工具权限是 fail closed 的，
// 硬凑一个相近的权限点（如 MailTemplateSave）等于用「能改模板」去换「能发信」。
// 需要发信请走群发活动（campaign_save + campaign_start），那条路有独立权限点。
func TemplateTools(w TemplateWriter, r TemplateReader) ([]mcp.Tool, error) {
	if w == nil || r == nil {
		return nil, errors.New("mailmcp: 模板工具依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{templateGet(r), templateSave(w), templateDelete(w)}, nil
}

func templateGet(r TemplateReader) mcp.Tool {
	return mcp.New("template_get", "取邮件模板全文",
		"按 templateKey + locale 取一个模板的完整内容（主题、HTML 正文、纯文本正文、变量）。\n"+
			"**改模板之前必须先调它** —— 保存是整体覆盖，不知道原文就改一个字，"+
			"等于把剩下的内容全丢了。\n"+
			"locale 不传按 zh-CN 处理；同一个 key 通常中英各有一版。",
		permission.MailTemplateList,
		mcp.Object("取模板全文参数", map[string]mcp.Schema{
			"templateKey": mcp.String("模板 key（用 mail_templates 拿）"),
			"locale":      mcp.String("语言版本（可选，默认 zh-CN）"),
		}, "templateKey"),
		func(ctx context.Context, args templateKeyArgs) (mcp.Result, error) {
			key := strings.TrimSpace(args.TemplateKey)
			loc := localeOf(args.Locale)
			list, err := r.ListTemplates(ctx, key)
			if err != nil {
				return mcp.Result{}, err
			}
			var hit *maildto.TemplateItem
			for _, it := range list {
				if it == nil || it.TemplateKey != key {
					continue
				}
				if it.Locale == loc {
					hit = it
					break
				}
				if hit == nil {
					hit = it // 语言没对上时先记一个，下面会说明实际取到了哪一版
				}
			}
			return mcp.Result{Text: templateFullText(hit, key, loc)}, nil
		})
}

func templateFullText(t *maildto.TemplateItem, wantKey, wantLocale string) string {
	if t == nil {
		return fmt.Sprintf("没有找到模板 %s（%s）。用 mail_templates 看一下有哪些 key。", wantKey, wantLocale)
	}
	var b strings.Builder
	// 语言没对上要说出来：不然模型会把英文版的内容当成中文版去改。
	if t.Locale != wantLocale {
		fmt.Fprintf(&b, "**注意：没有 %s 版本，下面是 %s 版的内容。**\n", wantLocale, t.Locale)
	}
	fmt.Fprintf(&b, "模板「%s」（key=%s，%s，id=%d）\n", t.Name, t.TemplateKey, t.Locale, t.ID)
	fmt.Fprintf(&b, "主题：%s\n", t.Subject)
	if len(t.Variables) > 0 {
		fmt.Fprintf(&b, "变量：%s\n", strings.Join(t.Variables, "、"))
	} else {
		b.WriteString("变量：无\n")
	}
	b.WriteString("\n--- HTML 正文 ---\n")
	b.WriteString(t.BodyHTML)
	if strings.TrimSpace(t.BodyText) != "" {
		b.WriteString("\n--- 纯文本正文 ---\n")
		b.WriteString(t.BodyText)
	}
	b.WriteString("\n\n保存时要把三段（主题 / HTML / 纯文本）**整套重发**，只发主题会把正文清空。")
	return b.String()
}

type templateKeyArgs struct {
	TemplateKey string `json:"templateKey"`
	Locale      string `json:"locale"`
}

// localeOf 空值落 zh-CN —— 与后台页面的默认语言一致。
func localeOf(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "zh-CN"
	}
	return locale
}

type templateSaveArgs struct {
	TemplateKey string   `json:"templateKey"`
	Locale      string   `json:"locale"`
	Name        string   `json:"name"`
	Subject     string   `json:"subject"`
	BodyHTML    string   `json:"bodyHtml"`
	BodyText    string   `json:"bodyText"`
	Variables   []string `json:"variables"`
}

func templateSave(w TemplateWriter) mcp.Tool {
	return mcp.NewWrite("template_save", "保存邮件模板（覆盖）",
		"保存一个邮件模板。**这是覆盖写，不是新建**：同一个 templateKey + locale 已存在时，"+
			"整条记录会被替换掉。\n"+
			"所以改一个已存在的模板前，**必须先调 template_get 取回全文**，"+
			"在原内容上改完再整体发过来 —— 只发主题会把正文清空。\n"+
			"变量用 Go 模板语法写：`{{.name}}`。用到的变量名要在 variables 里声明；"+
			"发送时变量缺失会**直接报错**（配的是 missingkey=error），不会静默留空。\n"+
			"保存前服务端会试着渲染一次（用空变量），模板语法错或变量名写错当场就能发现。",
		permission.MailTemplateSave,
		mcp.Object("保存模板参数", map[string]mcp.Schema{
			"templateKey": mcp.String("模板 key（新建时自己起一个，如 october_sale）"),
			"locale":      mcp.String("语言版本（可选，默认 zh-CN）"),
			"name":        mcp.String("模板名（给自己看的）"),
			"subject":     mcp.String("邮件主题（可用变量，如 你好 {{.name}}）"),
			"bodyHtml":    mcp.String("HTML 正文"),
			"bodyText":    mcp.String("纯文本正文（可选；给不支持 HTML 的客户端兜底）"),
			"variables":   mcp.Array("声明的变量名（可选；如 [\"name\",\"site_name\"]）", mcp.String("变量名，不带 {{.}}")),
		}, "templateKey", "subject", "bodyHtml"),
		nil,
		func(ctx context.Context, args templateSaveArgs) (mcp.Result, error) {
			res, err := w.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
				TemplateKey: strings.TrimSpace(args.TemplateKey),
				Locale:      localeOf(args.Locale),
				Name:        strings.TrimSpace(args.Name),
				Subject:     strings.TrimSpace(args.Subject),
				BodyHTML:    args.BodyHTML,
				BodyText:    args.BodyText,
				Variables:   cleanTags(args.Variables),
				OperatorID:  operatorID(ctx),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "模板已保存。"}, nil
			}
			return mcp.Result{Text: fmt.Sprintf(
				"模板「%s」已保存（key=%s，%s，id=%d，%d 字节 HTML 正文）。\n"+
					"同 key 同语言的旧版本已被替换。要拿它发信，建一个群发活动（campaign_save → campaign_start）。",
				res.Name, res.TemplateKey, res.Locale, res.ID, len(res.BodyHTML))}, nil
		})
}

type templateDeleteArgs struct {
	TemplateKey string `json:"templateKey"`
	Locale      string `json:"locale"`
}

func templateDelete(w TemplateWriter) mcp.Tool {
	return mcp.NewWrite("template_delete", "删除邮件模板",
		"删除一个邮件模板。**删的是 (templateKey, locale) 这一条** —— "+
			"同一个 key 的中文版与英文版是两条记录，删中文不会动英文。\n"+
			"**正在被群发活动引用的模板删不掉**（活动存的是模板 id）。\n"+
			"如果只是想换内容，用 template_save 覆盖更合适 —— 删了之后再建，"+
			"引用它的活动与自动化会指向一个不存在的模板。",
		permission.MailTemplateDelete,
		mcp.Object("删除模板参数", map[string]mcp.Schema{
			"templateKey": mcp.String("模板 key"),
			"locale":      mcp.String("语言版本（可选，默认 zh-CN；**只删这一版**）"),
		}, "templateKey"),
		nil,
		func(ctx context.Context, args templateDeleteArgs) (mcp.Result, error) {
			loc := localeOf(args.Locale)
			if err := w.DeleteTemplate(ctx, strings.TrimSpace(args.TemplateKey), loc); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"模板 %s（%s）已删除。同 key 的其它语言版本不受影响；"+
					"如果本来只是想换内容，下次用 template_save 覆盖更安全。"+
					"要重新提供这个模板的话，记得用 template_save 把整段内容一起发过来。",
				strings.TrimSpace(args.TemplateKey), loc)}, nil
		})
}
