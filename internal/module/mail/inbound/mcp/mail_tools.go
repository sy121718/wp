package mailmcp

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/mcp"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

func CampaignWriteTools(w mailcontract.CampaignWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("mailmcp: 群发写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{campaignSave(w), campaignStart(w), campaignDelete(w)}, nil
}

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
		mailenums.MailAccountList,
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
		mailenums.MailTemplateList,
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
		mailenums.MailCampaignSave,
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
		mailenums.MailCampaignStart,
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
		mailenums.MailCampaignDelete,
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

func ContactWriteTools(w mailcontract.ContactWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("mailmcp: 联系人写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		contactCreate(w),
		contactUpdate(w),
		contactStatus(w),
		contactDelete(w),
		contactTag(w),
	}, nil
}

func contactStatusChoices() []string {
	return []string{
		mailmodel.ContactStatusPending,
		mailmodel.ContactStatusSubscribed,
		mailmodel.ContactStatusUnsubscribed,
		mailmodel.ContactStatusBounced,
	}
}

type contactCreateArgs struct {
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Tags          []string `json:"tags"`
	ConsentSource string   `json:"consentSource"`
}

func contactCreate(w mailcontract.ContactWriter) mcp.Tool {
	return mcp.NewWrite("contact_create", "新建营销联系人",
		"把一个人加进营销联系人名单，之后可以给他发营销邮件。\n"+
			"**status 决定他能不能收到信，默认是 pending（不可发）**：新建时不传 status 落 pending —— "+
			"没有同意证据的人不进可发名单，这是合规底线，不是默认值的随手选择。"+
			"要直接能发，就传 subscribed **并且**在 consentSource 里写清同意依据（线下填表 / 网站勾选 / 客服记录），"+
			"那条记录是将来有人问「凭什么给他发」时唯一的凭据。\n"+
			"邮箱重复会被拒（同一邮箱只能有一条）。",
		mailenums.MailContactSave,
		mcp.Object("新建联系人参数", map[string]mcp.Schema{
			"email":         mcp.String("邮箱（必填，唯一）"),
			"name":          mcp.String("姓名（可选）"),
			"status":        mcp.Enum("同意状态（可选，默认 pending=不可发）：subscribed=已订阅（可发）", contactStatusChoices()...),
			"tags":          mcp.Array("标签（可选）", mcp.String("标签名")),
			"consentSource": mcp.String("同意依据（可选，但把 status 设成 subscribed 时应当给：谁在什么时候怎么同意的）"),
		}, "email"),
		nil,
		func(ctx context.Context, args contactCreateArgs) (mcp.Result, error) {
			id, err := w.CreateContact(ctx, &maildto.SaveContactReq{
				Email:         strings.TrimSpace(args.Email),
				Name:          strings.TrimSpace(args.Name),
				Status:        strings.TrimSpace(args.Status),
				Tags:          cleanTags(args.Tags),
				ConsentSource: strings.TrimSpace(args.ConsentSource),
				OperatorID:    operatorID(ctx),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: contactCreatedText(id, args)}, nil
		})
}

func contactCreatedText(id uint64, args contactCreateArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "已新建联系人 %s（id=%d）。", strings.TrimSpace(args.Email), id)
	if strings.TrimSpace(args.Status) == mailmodel.ContactStatusSubscribed {
		b.WriteString("\n状态是已订阅 —— 他会进入可发名单，订阅类自动化（如果有）可能当场给他发一封欢迎信。")
	} else {
		b.WriteString("\n状态是待确认（pending）：他还**收不到**营销邮件。" +
			"要让他能收到，用 contact_status 改成 subscribed，并在备注里写清同意依据。")
	}
	return b.String()
}

type contactUpdateArgs struct {
	ID     uint64   `json:"id"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Tags   []string `json:"tags"`
	Status string   `json:"status"`
}

func contactUpdate(w mailcontract.ContactWriter) mcp.Tool {
	return mcp.NewWrite("contact_update", "修改联系人资料",
		"改一个营销联系人的邮箱、姓名、标签或同意状态。只传要改的项。\n"+
			"**tags 是覆盖而不是追加**：保存后标签就是这里给的那几个，没列出的会被去掉。"+
			"只想加一个标签、保留原有的，用 contact_tag。\n"+
			"改状态同样会触发订阅类自动化（见 contact_status 的说明）；"+
			"把状态改成 subscribed 时应当在回答里提醒用户「可能马上会有一封自动化邮件发出去」。\n"+
			"id 用 contact_find 拿到的 id。",
		mailenums.MailContactSave,
		mcp.Object("修改联系人参数", map[string]mcp.Schema{
			"id":     mcp.Integer("联系人 id（用 contact_find 拿到的 id）"),
			"email":  mcp.String("新邮箱（可选；改邮箱会做唯一性校验）"),
			"name":   mcp.String("新姓名（可选）"),
			"tags":   mcp.Array("**完整**标签列表（可选；给了就是覆盖，没列出的会被去掉）", mcp.String("标签名")),
			"status": mcp.Enum("同意状态（可选）", contactStatusChoices()...),
		}, "id", "email"),
		nil,
		func(ctx context.Context, args contactUpdateArgs) (mcp.Result, error) {
			if err := w.UpdateContact(ctx, &maildto.SaveContactReq{
				ID:         args.ID,
				Email:      strings.TrimSpace(args.Email),
				Name:       strings.TrimSpace(args.Name),
				Status:     strings.TrimSpace(args.Status),
				Tags:       cleanTags(args.Tags),
				OperatorID: operatorID(ctx),
			}); err != nil {
				return mcp.Result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "已更新联系人 %d。", args.ID)
			if strings.EqualFold(strings.TrimSpace(args.Status), mailmodel.ContactStatusSubscribed) {
				b.WriteString("\n状态置为已订阅：他会进入可发名单，订阅类自动化可能当场发信。")
			}
			return mcp.Result{Text: b.String()}, nil
		})
}

type contactStatusArgs struct {
	ID     uint64 `json:"id"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

func contactStatus(w mailcontract.ContactWriter) mcp.Tool {
	return mcp.NewWrite("contact_status", "修改联系人订阅状态",
		"改一个人的同意状态：已订阅（能收信）/ 待确认（收不到）/ 已退订（明确拒绝）/ 投递失败（地址打不通）。\n"+
			"**置为 subscribed 会触发订阅类自动化** —— 如果这个工程配了「订阅后发欢迎信」的自动化，"+
			"下一条就会当场发出去。所以执行前要跟用户确认，回答里也要提醒他这件事。\n"+
			"note 会记进状态变更留痕：将来有人问「他什么时候同意的、怎么同意的」，"+
			"答案就在这里，所以线上拿到的口头同意也应当写进去。\n"+
			"退订（unsubscribed）是客户自己的意愿，**不要用它做「暂时不想发」的手动屏蔽** ——"+
			"那会覆盖掉他真实的退订记录。",
		mailenums.MailContactStatus,
		mcp.Object("修改联系人状态参数", map[string]mcp.Schema{
			"id":     mcp.Integer("联系人 id（用 contact_find 拿到）"),
			"status": mcp.Enum("目标状态", contactStatusChoices()...),
			"note":   mcp.String("变更备注（可选，但改成 subscribed 时应当写清同意依据）"),
		}, "id", "status"),
		nil,
		func(ctx context.Context, args contactStatusArgs) (mcp.Result, error) {
			if err := w.UpdateContactStatus(ctx, &maildto.UpdateContactStatusReq{
				ID:         args.ID,
				Status:     strings.TrimSpace(args.Status),
				Note:       strings.TrimSpace(args.Note),
				OperatorID: operatorID(ctx),
			}); err != nil {
				return mcp.Result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "联系人 %d 的状态已改为「%s」。", args.ID, contactStatusText(args.Status))
			if strings.EqualFold(strings.TrimSpace(args.Status), mailmodel.ContactStatusSubscribed) {
				b.WriteString("\n注意：他会进入可发名单，订阅类自动化（如果有）可能**马上发出一封邮件**。")
			}
			return mcp.Result{Text: b.String()}, nil
		})
}

type contactDeleteArgs struct {
	IDs []uint64 `json:"ids"`
}

func contactDelete(w mailcontract.ContactWriter) mcp.Tool {
	return mcp.NewWrite("contact_delete", "删除营销联系人",
		"从营销名单里删掉一个或多个联系人。**这是真删除，删掉后收件记录还在但人不在名单里**。\n"+
			"绝大多数情况下用户想要的不是删除而是**退订**（保留记录、不再发信）——"+
			"删掉之后再想查「这个人以前收过什么」就只能翻历史发信记录了。\n"+
			"所以执行前必须把「要删哪几个（念出邮箱）」念给用户确认；"+
			"如果他的诉求只是「别再给这个人发了」，改用 contact_status 置为 unsubscribed。",
		mailenums.MailContactDelete,
		mcp.Object("删除联系人参数", map[string]mcp.Schema{
			"ids": mcp.Array("要删除的联系人 id 列表（用 contact_find 拿到的 id）", mcp.Integer("联系人 id")),
		}, "ids"),
		nil,
		func(ctx context.Context, args contactDeleteArgs) (mcp.Result, error) {
			ids := dedupeUint64(args.IDs)
			if len(ids) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "ids 不能为空：要说明删哪几个。先用 contact_find 拿到 id。"}
			}
			n, err := w.DeleteContacts(ctx, &maildto.DeleteContactsReq{IDs: ids, OperatorID: operatorID(ctx)})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"已删除 %d 个联系人（请求 %d 个）。他们不在名单里了，但历史发信记录仍然保留。",
				n, len(ids))}, nil
		})
}

type contactTagArgs struct {
	IDs    []uint64 `json:"ids"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

func contactTag(w mailcontract.ContactWriter) mcp.Tool {
	return mcp.NewWrite("contact_tag", "给联系人打标签",
		"给一批联系人加标签、去标签。加与减在同一次调用里完成 —— 分两次提交会出现"+
			"「加成功、减失败」的半截状态，而运营眼里这是一个动作。\n"+
			"**打标签会触发标签类自动化**：如果工程里配了「打了 vip 标签就发某某信」，"+
			"这条会当场发出去。回答里要提醒用户。\n"+
			"这与 contact_update 的 tags 不同：那个是**覆盖**（没列出就没了），"+
			"这个是**增删**（不动其它标签）。只想加一个用这个。",
		mailenums.MailContactTag,
		mcp.Object("联系人打标签参数", map[string]mcp.Schema{
			"ids":    mcp.Array("联系人 id 列表（用 contact_find 拿到）", mcp.Integer("联系人 id")),
			"add":    mcp.Array("要加上的标签（可选）", mcp.String("标签名")),
			"remove": mcp.Array("要去掉的标签（可选）", mcp.String("标签名")),
		}, "ids"),
		nil,
		func(ctx context.Context, args contactTagArgs) (mcp.Result, error) {
			ids := dedupeUint64(args.IDs)
			if len(ids) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "ids 不能为空：要说明给哪几个人打标签。"}
			}
			add, remove := cleanTags(args.Add), cleanTags(args.Remove)
			if len(add) == 0 && len(remove) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "add 与 remove 不能同时为空：这次调用没有要改的东西。"}
			}
			changed, skipped, err := w.TagContacts(ctx, &maildto.TagContactsReq{
				IDs: ids, Add: add, Remove: remove, OperatorID: operatorID(ctx),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "已修改 %d 个联系人的标签（请求 %d 个）", changed, len(ids))
			if skipped > 0 {

				fmt.Fprintf(&b, "，跳过 %d 个（标签本来就是这个状态）", skipped)
			}
			if len(add) > 0 {
				b.WriteString("，加上：" + strings.Join(add, "、"))
			}
			if len(remove) > 0 {
				b.WriteString("，去掉：" + strings.Join(remove, "、"))
			}
			b.WriteString("。")
			if len(add) > 0 {
				b.WriteString("\n注意：新加上的标签可能命中标签类自动化，**马上会发出邮件**。")
			}
			return mcp.Result{Text: b.String()}, nil
		})
}

func operatorID(ctx context.Context) uint64 {
	if id := mcp.UserIDFrom(ctx); id > 0 {
		return uint64(id)
	}
	return 0
}

func cleanTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func dedupeUint64(in []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(in))
	out := make([]uint64, 0, len(in))
	for _, id := range in {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

const (
	mailDefaultPageSize = 10
	mailMaxPageSize     = 50

	mailReportLinkLimit = 10
)

func QueryTools(query mailcontract.MailQueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("mailmcp: 营销查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{contactFind(query), campaignList(query), campaignStats(query)}, nil
}

type contactFindArgs struct {
	Keyword  string   `json:"keyword"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	PageSize int      `json:"pageSize"`
}

func contactFind(query mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("contact_find", "按线索查邮件联系人",
		"按模糊线索查营销邮件的联系人（订阅名单），用于回答「这个邮箱订阅过吗」「这批人是什么状态」「谁退订了」。\n"+
			"keyword 会同时匹配**邮箱与姓名**（不区分大小写），用户给邮箱片段或名字都能命中。\n"+
			"tags 是**全部命中**（不是任一）：给多个标签时只返回同时带这些标签的人。\n"+
			"结果里的 id 可以拿去 campaign_stats 之外的地方用；要某次群发的效果请用 campaign_stats。",
		mailenums.MailContactList,
		mcp.Object("查邮件联系人参数", map[string]mcp.Schema{
			"keyword": mcp.String("线索：邮箱或姓名，模糊匹配（可选）"),
			"status": mcp.Enum("只看某种订阅状态（可选；不传则全部）",
				"subscribed", "pending", "unsubscribed", "bounced"),
			"tags":     mcp.Array("只看同时带这些标签的联系人（可选）", mcp.String("标签名")),
			"pageSize": mcp.Integer("最多返回几位，默认 10，上限 50"),
		}, []string{}...),
		func(ctx context.Context, args contactFindArgs) (mcp.Result, error) {
			res, err := query.ListContacts(ctx, &maildto.ContactFilterReq{
				Keyword:  args.Keyword,
				Status:   args.Status,
				Tags:     args.Tags,
				Page:     1,
				PageSize: clampMailPageSize(args.PageSize),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: contactListText(res), Data: res}, nil
		})
}

func contactListText(res *maildto.ContactListResp) string {
	if res == nil {
		return "没有查到联系人。"
	}
	if len(res.Items) == 0 {
		return "没有符合条件的联系人。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 位（共 %d 位符合条件）：\n", len(res.Items), res.Total)
	for i, c := range res.Items {
		fmt.Fprintf(&b, "%d. id=%d %s %s · %s%s%s\n",
			i+1, c.ID, emptyAsDash(c.Email), nameSuffix(c.Name),
			contactStatusText(c.Status), tagSuffix(c.Tags), sourceSuffix(c))
	}
	return strings.TrimRight(b.String(), "\n")
}

func contactStatusText(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "subscribed":
		return "已订阅"
	case "pending":
		return "待确认"
	case "unsubscribed":
		return "已退订"
	case "bounced":
		return "投递失败"
	case "":
		return "状态未知"
	default:
		return status
	}
}

func nameSuffix(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "（" + name + "）"
}

func tagSuffix(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return " · 标签 " + strings.Join(tags, "/")
}

func sourceSuffix(c maildto.ContactItem) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(c.Source) != "" {
		parts = append(parts, "来源 "+c.Source)
	}
	if strings.TrimSpace(c.CreateTime) != "" {
		parts = append(parts, "加入 "+c.CreateTime)
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

type campaignListArgs struct {
	Status   string `json:"status"`
	PageSize int    `json:"pageSize"`
}

func campaignList(query mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("campaign_list", "营销活动列表",
		"列出营销邮件（群发活动），用于回答「最近发过哪些邮件」「那封发完了吗」「一共发出去多少人」。\n"+
			"每条都带**投递进度**（目标人数 / 已发送 / 失败）与时间，所以「发完了没」不必再调一次。\n"+
			"要某一次的效果（打开率 / 点击率 / 退订人数）用 campaign_stats 带上这里的 id。",
		mailenums.MailCampaignList,
		mcp.Object("列营销活动参数", map[string]mcp.Schema{
			"status": mcp.Enum("只看某个状态（可选；不传则全部）",
				"draft", "scheduled", "sending", "sent", "failed", "canceled"),
			"pageSize": mcp.Integer("最多返回几条，默认 10，上限 50"),
		}, []string{}...),
		func(ctx context.Context, args campaignListArgs) (mcp.Result, error) {
			res, err := query.ListCampaigns(ctx, &maildto.CampaignListReq{
				Status:   args.Status,
				Page:     1,
				PageSize: clampMailPageSize(args.PageSize),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: campaignListText(res), Data: res}, nil
		})
}

func campaignListText(res *maildto.CampaignListResp) string {
	if res == nil {
		return "没有查到营销活动。"
	}
	if len(res.Items) == 0 {
		return "没有符合条件的营销活动。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个活动，最近 %d 个：\n", res.Total, len(res.Items))
	for i, c := range res.Items {
		fmt.Fprintf(&b, "%d. id=%d「%s」%s · 目标 %d 人、已发 %d、失败 %d%s\n",
			i+1, c.ID, emptyAsDash(c.Name), campaignStatusText(c.Status),
			c.TotalCount, c.SentCount, c.FailedCount, campaignTimeSuffix(c))
	}
	return strings.TrimRight(b.String(), "\n")
}

func campaignStatusText(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft":
		return "草稿"
	case "scheduled":
		return "已排期"
	case "sending":
		return "发送中"
	case "sent":
		return "已发送"
	case "failed":
		return "发送失败"
	case "canceled":
		return "已取消"
	case "":
		return "状态未知"
	default:
		return status
	}
}

func campaignTimeSuffix(c maildto.CampaignItem) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(c.StartedAt) != "" {
		parts = append(parts, "开始 "+c.StartedAt)
	}
	if strings.TrimSpace(c.FinishedAt) != "" {
		parts = append(parts, "结束 "+c.FinishedAt)
	}
	if len(parts) == 0 && strings.TrimSpace(c.CreateTime) != "" {
		parts = append(parts, "创建 "+c.CreateTime)
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

type campaignStatsArgs struct {
	CampaignID uint64 `json:"campaignId"`
}

func campaignStats(query mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("campaign_stats", "单次营销活动的效果",
		"读一次营销邮件群发的效果：送达 / 失败、打开与点击的去重人数与次数、打开率与点击率、"+
			"退订与投诉人数、投递失败人数，以及点击最多的几个链接。\n"+
			"用于回答「上次那封效果怎么样」「打开率是多少」「哪个链接点得多」。\n"+
			"campaignId 从 campaign_list 的结果里拿。注意打开率与点击率都是**估算**"+
			"（邮件客户端的图片预加载会抬高打开数），汇报时应当说明这一点。",
		mailenums.MailCampaignGet,
		mcp.Object("读单次活动效果参数", map[string]mcp.Schema{
			"campaignId": mcp.Integer("活动 id（campaign_list 结果里的 id）"),
		}, "campaignId"),
		func(ctx context.Context, args campaignStatsArgs) (mcp.Result, error) {
			if args.CampaignID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "campaignId 必填，且要来自 campaign_list 的结果"}
			}
			res, err := query.CampaignReport(ctx, args.CampaignID, 1, mailReportLinkLimit)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: campaignReportText(res), Data: res}, nil
		})
}

func campaignReportText(res *maildto.CampaignReport) string {
	if res == nil {
		return "没有取到这次活动的报表。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "活动「%s」（id=%d，%s）的效果：\n",
		emptyAsDash(res.Campaign.Name), res.Campaign.ID, campaignStatusText(res.Campaign.Status))
	fmt.Fprintf(&b, "* 投递：目标 %d 人，送达 %d，失败 %d\n", res.Target, res.Sent, res.Failed)
	fmt.Fprintf(&b, "* 打开：%d 人（%d 次），打开率 %.1f%%\n", res.Opened, res.OpenEvents, res.OpenRate)
	fmt.Fprintf(&b, "* 点击：%d 人（%d 次），点击率 %.1f%%\n", res.Clicked, res.ClickEvents, res.ClickRate)
	if res.Unsubscribed > 0 || res.Complained > 0 || res.Bounced > 0 {
		fmt.Fprintf(&b, "* 负向：退订 %d、投诉 %d、投递失败 %d\n", res.Unsubscribed, res.Complained, res.Bounced)
	}
	if len(res.Links) > 0 {
		fmt.Fprintf(&b, "* 点击最多的链接：\n%s", linkStatText(res.Links))
	}
	b.WriteString("（打开率与点击率是估算值：邮件客户端的图片预加载会抬高打开数，汇报时宜说明）")
	return strings.TrimRight(b.String(), "\n")
}

func linkStatText(links []maildto.LinkStat) string {
	var b strings.Builder
	for i, l := range links {

		fmt.Fprintf(&b, "  %d. %s — %d 次点击、%d 人\n", i+1, emptyAsDash(l.URL), l.Total, l.Contacts)
	}
	return b.String()
}

func clampMailPageSize(n int) int {
	if n <= 0 {
		return mailDefaultPageSize
	}
	if n > mailMaxPageSize {
		return mailMaxPageSize
	}
	return n
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

type TemplateWriter interface {
	UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (*maildto.TemplateItem, error)
	DeleteTemplate(ctx context.Context, key, locale string) error
}

type TemplateReader interface {
	ListTemplates(ctx context.Context, key string) ([]*maildto.TemplateItem, error)
}

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
		mailenums.MailTemplateList,
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
					hit = it
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
		mailenums.MailTemplateSave,
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
		mailenums.MailTemplateDelete,
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
