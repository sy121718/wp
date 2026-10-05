package mailmcp

// mail_tools.go — 营销（邮件）模块的三个只读工具。
//
// 为什么补这一批：用户问「这个邮箱订阅过吗」「上次那封群发效果怎么样」「最近发过哪些邮件」
// 时，答案全在 mail_contacts / mail_campaigns / mail_campaign_events 里，而在此之前
// AI 侧看不到任何营销数据 —— 它会说「我看不到营销模块的数据」然后停下来。
// 底层一直是齐的：ContactFilter 的关键词同时模糊匹配 email 与 name，
// CampaignReport 连打开率与点击率都算好了。
//
// 只读：依赖收窄到 MailQueryReader（三个方法），手里没有 StartCampaign、
// 没有 UpdateContactStatus —— 「AI 顺手把人退订了」不会在某次改动里悄悄变得可能。
//
// 为什么是三个而不是把活动与报表合成一个：列表回答「有哪些、发了多少」，
// 报表回答「效果如何」，而报表需要上一句里拿到的 campaignId。合并的话每次问
// 「有哪些」都会顺带拉一次没人要的完整报表（那是 mail_campaign_events 上的聚合，
// 不便宜）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/permission"
)

const (
	mailDefaultPageSize = 10
	mailMaxPageSize     = 50
	// mailReportLinkLimit 报表里取多少条链接明细。
	//
	// 用户问「哪个链接点得多」时前几条就答完了；把 200 条链接铺进上下文
	// 只会挤掉回答本身。
	mailReportLinkLimit = 10
)

// QueryTools 返回营销模块的只读工具集。
func QueryTools(query mailcontract.MailQueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("mailmcp: 营销查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{contactFind(query), campaignList(query), campaignStats(query)}, nil
}

// —— 联系人 ——

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
		permission.MailContactList,
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

// contactStatusText 订阅状态的展示名。
//
// 不自己写一份中文表：取值是 model 里的四个常量（pending / subscribed /
// unsubscribed / bounced），而四个状态对用户的含义**不一样** ——
// 把 bounced（投递失败，通常是邮箱不存在）读成 unsubscribed（本人主动退订）
// 会让运营去追问一个根本没点过退订的人。
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

// —— 活动 ——

type campaignListArgs struct {
	Status   string `json:"status"`
	PageSize int    `json:"pageSize"`
}

func campaignList(query mailcontract.MailQueryReader) mcp.Tool {
	return mcp.New("campaign_list", "营销活动列表",
		"列出营销邮件（群发活动），用于回答「最近发过哪些邮件」「那封发完了吗」「一共发出去多少人」。\n"+
			"每条都带**投递进度**（目标人数 / 已发送 / 失败）与时间，所以「发完了没」不必再调一次。\n"+
			"要某一次的效果（打开率 / 点击率 / 退订人数）用 campaign_stats 带上这里的 id。",
		permission.MailCampaignList,
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

// —— 单个活动的效果 ——

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
		permission.MailCampaignGet,
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
		// Total 是点击次数、Contacts 是去重人数 —— 两个数都写出来：
		// 同一个人点 5 次与 5 个人各点 1 次，对「这条链接受欢迎吗」是两种答案。
		fmt.Fprintf(&b, "  %d. %s — %d 次点击、%d 人\n", i+1, emptyAsDash(l.URL), l.Total, l.Contacts)
	}
	return b.String()
}

// —— 共用 ——

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
