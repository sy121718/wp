package mailmcp

// campaign_write_tools.go — 群发活动的写工具，外加两个为它铺路只读工具。
//
// 为什么顺带补 mail_accounts / mail_templates：SaveCampaign 必填 accountId 与
// templateId（**不是 key**），而在此之前没有任何工具能给出这两个数字 ——
// 模型只能停下来问用户要 id，用户则要去后台翻两页把数字念回来。
// 「工具的输出里要带上下一步动作需要的参数」这条在 stock_find 漏 variantId、
// stock_reasons 漏 id 上已经吃过两回，这是第三处。
//
// 这一组里 campaign_start 是本仓唯一会**向站外真实收件人批量发信**的动作：
// 它不可撤销（信已经出去了），所以描述里把确认要求写到了最死。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/permission"
)

// CampaignWriteTools 返回群发活动写工具集。
func CampaignWriteTools(w mailcontract.CampaignWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("mailmcp: 群发写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{campaignSave(w), campaignStart(w), campaignDelete(w)}, nil
}

// MailSetupTools 返回「建活动之前要看的两个清单」。
//
// 它们读的是发信账号与邮件模板，不是联系人 —— 归在查询侧但只服务于写路径，
// 所以放在本文件里、由装配处与写工具一起注册。
func MailSetupTools(q mailcontract.MailQueryReader) ([]mcp.Tool, error) {
	if q == nil {
		return nil, errors.New("mailmcp: 发信配置查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{mailAccounts(q), mailTemplates(q)}, nil
}

func mailAccounts(q mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("mail_accounts", "列出发信账号",
		"列出本站配置的发信账号（含 id）。新建群发活动时要填 accountId，用这里的 id。\n"+
			"只列账号的对外信息，不含密码。",
		permission.MailAccountList,
		mcp.Object("发信账号列表参数", map[string]mcp.Schema{
			"purpose": mcp.Enum("按用途筛选（可选；不传列全部）", "marketing", "transactional"),
		}),
		func(ctx context.Context, args struct {
			Purpose string `json:"purpose"`
		}) (mcp.Result, error) {
			list, err := q.ListAccounts(ctx, strings.TrimSpace(args.Purpose))
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: accountsText(list)}, nil
		})
}

func accountsText(list []*maildto.AccountItem) string {
	if len(list) == 0 {
		return "还没有配置发信账号。要发信得先在后台「邮箱 → 发信账号」里配一个。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个发信账号：\n", len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		def := ""
		if a.IsDefault {
			def = " · **默认**"
		}
		purpose := a.Purpose
		if purpose == mailmodel.AccountPurposeMarketing {
			purpose = "营销"
		} else if purpose == mailmodel.AccountPurposeTransactional {
			purpose = "事务（注册验证 / 密码重置那类）"
		}
		fmt.Fprintf(&b, "- id=%d %s（%s，发件人 %s <%s>%s）\n",
			a.ID, strings.TrimSpace(a.Name), purpose, strings.TrimSpace(a.FromName), strings.TrimSpace(a.FromEmail), def)
	}
	b.WriteString("营销群发请挑「营销」用途的账号。")
	return strings.TrimRight(b.String(), "\n")
}

func mailTemplates(q mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("mail_templates", "列出邮件模板",
		"列出本站的邮件模板（含 id 与各自需要的变量）。新建群发活动时要填 templateId，用这里的 id。\n"+
			"模板体的 HTML 不在这里返回（可能很长），只有名称、主题与变量清单。",
		permission.MailTemplateList,
		mcp.Object("邮件模板列表参数", map[string]mcp.Schema{
			"key": mcp.String("按模板 key 筛选（可选；不传列全部）"),
		}),
		func(ctx context.Context, args struct {
			Key string `json:"key"`
		}) (mcp.Result, error) {
			list, err := q.ListTemplates(ctx, strings.TrimSpace(args.Key))
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: templatesText(list)}, nil
		})
}

func templatesText(list []*maildto.TemplateItem) string {
	if len(list) == 0 {
		return "没有找到邮件模板。要发活动得先在后台「邮箱 → 邮件模板」里建一个。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个邮件模板：\n", len(list))
	for _, t := range list {
		if t == nil {
			continue
		}
		vars := "无变量"
		if len(t.Variables) > 0 {
			vars = "变量：" + strings.Join(t.Variables, "、")
		}
		fmt.Fprintf(&b, "- id=%d「%s」（key=%s，%s，主题 %s；%s）\n",
			t.ID, strings.TrimSpace(t.Name), strings.TrimSpace(t.TemplateKey),
			strings.TrimSpace(t.Locale), strings.TrimSpace(t.Subject), vars)
	}
	b.WriteString("建活动时活动自己的 subject 会覆盖模板主题；模板里用到的变量要在活动的 variables 里给全。")
	return strings.TrimRight(b.String(), "\n")
}

type campaignSaveArgs struct {
	ID            uint64   `json:"id"`
	Name          string   `json:"name"`
	AccountID     uint64   `json:"accountId"`
	TemplateID    uint64   `json:"templateId"`
	Subject       string   `json:"subject"`
	TargetTags    []string `json:"targetTags"`
	VariablesJSON string   `json:"variablesJson"`
}

// parseCampaignVariables 解析模板变量。
//
// 用 JSON 字符串而不是嵌套对象是刻意的：mcp.Schema 的 object 一律
// AdditionalProperties=false（`internal/mcp/schema.go:26` 写明「多传的字段一律拒绝」），
// 而模板变量是**任意键名**的自由对象 —— 嵌套 schema 表达不了它，
// 硬写会在校验层把用户自定义的变量名全部拒掉。
// 解析失败不静默忽略：变量错了会在启动发送时才暴露，那时活动已经建好了。
func parseCampaignVariables(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("variablesJson 不是合法的 JSON 对象：%v", err)
	}
	return out, nil
}

func campaignSave(w mailcontract.CampaignWriter) mcp.Tool {
	return mcp.NewWrite("campaign_save", "新建 / 编辑群发活动",
		"建一个营销群发活动（草稿），或改一个还处于草稿状态的活动。\n"+
			"**这一步只是存草稿，不会发信** —— 发信要另外调 campaign_start。\n"+
			"accountId 与 templateId 必填，分别用 mail_accounts 与 mail_templates 拿（是数字 id，不是 key）。\n"+
			"targetTags 决定**发给谁**：只发给带这些标签的联系人；**不传就是发给所有已订阅的人**，"+
			"所以两个都要想清楚 —— 漏填标签的后果是全站群发。\n"+
			"模板需要的变量要在 variables 里给全，缺变量会在启动发送时被拒（那时才发现会白跑一趟）。\n"+
			"活动不是草稿时就改不了（已发出的内容不能回改），要新建一个。",
		permission.MailCampaignSave,
		mcp.Object("群发活动参数", map[string]mcp.Schema{
			"id":         mcp.Integer("要编辑的活动 id（可选；不传或 0 表示新建）"),
			"name":       mcp.String("活动名称（给自己看的，如「十月新品通知」）"),
			"accountId":  mcp.Integer("发信账号 id（用 mail_accounts 拿；营销群发请挑营销用途的账号）"),
			"templateId": mcp.Integer("邮件模板 id（用 mail_templates 拿，是数字 id 不是 key）"),
			"subject":    mcp.String("邮件主题（会覆盖模板里的主题）"),
			"targetTags": mcp.Array("只发给带这些标签的联系人（**不传 = 发给所有已订阅的人**，谨慎）", mcp.String("标签名")),
			"variablesJson": mcp.String("模板变量，JSON 对象字符串（可选），如 {\"name\":\"张三\"}。" +
				"模板里用到的变量要在这里给全，缺了会在启动发送时才被拒，白跑一趟。"),
		}, "name", "accountId", "templateId", "subject"),
		nil,
		func(ctx context.Context, args campaignSaveArgs) (mcp.Result, error) {
			vars, verr := parseCampaignVariables(args.VariablesJSON)
			if verr != nil {
				return mcp.Result{}, verr
			}
			res, err := w.SaveCampaign(ctx, &maildto.SaveCampaignReq{
				ID:         args.ID,
				Name:       strings.TrimSpace(args.Name),
				AccountID:  args.AccountID,
				TemplateID: args.TemplateID,
				Subject:    strings.TrimSpace(args.Subject),
				TargetTags: cleanTags(args.TargetTags),
				Variables:  vars,
				OperatorID: operatorID(ctx),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: campaignSavedText(res, args)}, nil
		})
}

func campaignSavedText(res *maildto.CampaignItem, args campaignSaveArgs) string {
	if res == nil {
		return "活动已保存（草稿）。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "活动「%s」已保存为**草稿**（id=%d，主题「%s」）。",
		strings.TrimSpace(res.Name), res.ID, strings.TrimSpace(res.Subject))
	b.WriteString("\n它还没有发出去 —— 确认内容没问题后，用 campaign_start 启动。")
	if len(args.TargetTags) == 0 {
		b.WriteString("\n**注意：没有指定 targetTags，启动后会发给所有已订阅的联系人。**")
	} else {
		b.WriteString("\n收件范围：带标签 " + strings.Join(cleanTags(args.TargetTags), "、") + " 的联系人。")
	}
	return b.String()
}

type campaignStartArgs struct {
	CampaignID uint64 `json:"campaignId"`
}

func campaignStart(w mailcontract.CampaignWriter) mcp.Tool {
	return mcp.NewWrite("campaign_start", "启动群发活动（真发信）",
		"**启动一个群发活动 —— 这会把邮件真实发给收件人，发出去就收不回来。**\n"+
			"执行前**必须**先做两件事，缺一不可：\n"+
			"① 用 campaign_list 或活动详情确认收件规模（会发给多少人、范围是哪些标签）；\n"+
			"② 把「活动名、主题、收件人数、发件账号」念给用户，明确问他是否现在发。\n"+
			"用户只说了「发吧」而没看到上面这几项时，先念一遍再问 —— 群发不可撤销，"+
			"发错了只能道歉，而道歉对已经进了收件箱的信没有意义。\n"+
			"只有草稿（或发送失败可重试）状态能启动；已经在发的不能重复启动。",
		permission.MailCampaignStart,
		mcp.Object("启动群发参数", map[string]mcp.Schema{
			"campaignId": mcp.Integer("要启动的活动 id（用 campaign_list 拿到的 id）"),
		}, "campaignId"),
		nil,
		func(ctx context.Context, args campaignStartArgs) (mcp.Result, error) {
			res, err := w.StartCampaign(ctx, &maildto.StartCampaignReq{
				CampaignID: args.CampaignID,
				OperatorID: operatorID(ctx),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: campaignStartedText(args.CampaignID, res)}, nil
		})
}

// campaignStartedText 启动回执。
//
// 必须说明「现在是排队发，不是已经发完了」：否则用户会立刻去数收件箱，
// 发现只到了几封就以为出问题了。
func campaignStartedText(id uint64, res *maildto.StartCampaignResp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "活动 %d 已启动。", id)
	if res != nil && res.Total > 0 {
		fmt.Fprintf(&b, "本次将发送给 **%d** 位联系人。", res.Total)
	}
	b.WriteString("\n发送是逐批进行的（不是一次全发完）—— 用 campaign_stats 看进度，别急着数收件箱。")
	return b.String()
}

type campaignDeleteArgs struct {
	CampaignID uint64 `json:"campaignId"`
}

func campaignDelete(w mailcontract.CampaignWriter) mcp.Tool {
	return mcp.NewWrite("campaign_delete", "删除群发活动",
		"删除一个群发活动。**正在发送中的活动删不掉**（已经发出去的信不会因为删记录而收回）。\n"+
			"删除只影响这条活动记录与它的报表，不影响已有的发信记录。\n"+
			"如果用户只是想「别再继续发」，那不是这个工具能做的 —— 告诉他去后台把活动停下来。",
		permission.MailCampaignDelete,
		mcp.Object("删除群发活动参数", map[string]mcp.Schema{
			"campaignId": mcp.Integer("要删除的活动 id（用 campaign_list 拿到的 id）"),
		}, "campaignId"),
		nil,
		func(ctx context.Context, args campaignDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteCampaign(ctx, args.CampaignID); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"活动 %d 已删除。已经发出去的信不受影响（收不回来），只是活动记录与报表没了。",
				args.CampaignID)}, nil
		})
}
