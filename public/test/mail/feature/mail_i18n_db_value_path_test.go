package feature

// mail_i18n_db_value_path_test.go — 「键存在时渲染走 DB 值、而不是模板 fallback」的守卫。
//
// 为什么必须单独有这条：本轮三个真缺陷（admin.mail.marketing.contacts.empty、
// admin.mail.templates.help.label、admin.mail.campaign.empty.action）**全部是
// 「DB 值 ≠ 模板 fallback」形态**。取值链是「当前语言 → 默认语言 → fallback → key」
// （pkg/i18n/snapshot.go:39-57），键一旦存在，模板里的兜底文案永远不会被渲染 ——
// 于是「改了模板忘了改库」在页面上只表现为文案不一致，**页面测试结构上抓不到**：
// 拆页后的渲染用例都不注入 i18n，跑的其实全是 fallback 路径，两边永远一致。
//
// 本文件的机制（不绑定任何具体文案，所以文案再改也不会失效）：
//  1. 用 i18n.InjectForTest 造一个「库里有词条」的世界，给待守的键塞哨兵值；
//  2. 渲染对应页面，正文**必须出现哨兵** —— 出现即证明渲染读的是 DB 值路径；
//  3. 正文**不得出现该键的模板 fallback** —— 这一条把「键存在却渲染了 fallback」
//     直接判死，正是三个缺陷的形态；
//  4. 注入前先渲染一次作对照：那时正文必须出现 fallback、且不出现哨兵 ——
//     证明这两条断言确实能分辨两条路径（否则它们只是永远成立的摆设）。
//
// -race 结论（实测；连前提一起记，别让后人误读成「并发安全已证明」）：
//
//	GOWP_REQUIRE_PG=1 go test -race ./public/test/mail/feature \
//	    -run TestMailPagesRenderDBValueInsteadOfFallback -count=1   → ok，无 race 报告
//
// 前提：本包用例默认串行执行，且没有任何用例调用 t.Parallel()，所以
// 「InjectForTest 注入 → 渲染读取 → t.Cleanup 复位」是严格顺序的。
// 这条干净只证明**当前**没有并发访问这份全局快照（i18n 缓存是包级单例、
// 跨用例共享），不等于 InjectForTest 本身并发安全 —— 将来谁在本包引入并行
// 用例，必须重新跑 -race。

import (
	"context"
	"strings"
	"testing"

	i18n "go_wp/pkg/i18n"

	mailhttp "go_wp/internal/module/mail/inbound/http"
)

const (
	sentinelContactsEmpty  = "SENTINEL_CONTACTS_EMPTY_DESC"
	sentinelCampaignBack   = "SENTINEL_CAMPAIGN_BACK"
	sentinelCampaignAction = "SENTINEL_CAMPAIGN_EMPTY_ACTION"
	sentinelHelpLabel      = "SENTINEL_HELP_LABEL"
)

// mailI18nSentinelCase 一个「页面上的取词位置」：路径、键、哨兵、模板 fallback。
type mailI18nSentinelCase struct {
	name     string
	path     string
	key      string
	sentinel string
	fallback string
}

// TestMailPagesRenderDBValueInsteadOfFallback 见文件头说明。
func TestMailPagesRenderDBValueInsteadOfFallback(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	// 活动报表页以完整报表数据为前提：没有活动时 handler 会 302 回列表页，
	// 断言就落在重定向后的空白上，而不是页面的取词位置。
	if err := f.seedCampaign(context.Background(), "i18n 哨兵活动"); err != nil {
		t.Fatalf("造活动失败: %v", err)
	}
	var campaignID int64
	if err := f.db.Raw("SELECT id FROM mail_campaigns ORDER BY id DESC LIMIT 1").
		Scan(&campaignID).Error; err != nil || campaignID == 0 {
		t.Fatalf("取活动 id 失败: err=%v id=%d", err, campaignID)
	}

	engine := mailSplitEngine(t)
	handle := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail/contacts", handle.MailContactsPage)
	engine.GET("/admin/mail/templates", handle.MailTemplatesPage)
	engine.GET("/admin/mail/campaign", handle.MailCampaignPage)

	cases := []mailI18nSentinelCase{
		{
			// 筛选无结果档才用 .empty；无筛选是 .empty.initial（另一条键）。
			name:     "联系人页筛选空态描述",
			path:     "/admin/mail/contacts?keyword=definitely-no-such-contact",
			key:      "admin.mail.marketing.contacts.empty",
			sentinel: sentinelContactsEmpty,
			fallback: "可调整筛选条件，或点右上角「导入联系人」批量导入。",
		},
		{
			name:     "活动报表页返回按钮",
			path:     "/admin/mail/campaign?id=" + itoa(campaignID),
			key:      "admin.mail.campaign.back",
			sentinel: sentinelCampaignBack,
			fallback: "返回群发活动",
		},
		{
			// 同一页空态动作（mail_campaign.html:87 与 :118 两处都用这条键）。
			name:     "活动报表页空态动作",
			path:     "/admin/mail/campaign?id=" + itoa(campaignID),
			key:      "admin.mail.campaign.empty.action",
			sentinel: sentinelCampaignAction,
			fallback: "回群发活动页启动",
		},
		{
			name:     "邮件模板页帮助按钮",
			path:     "/admin/mail/templates",
			key:      "admin.common.help.label",
			sentinel: sentinelHelpLabel,
			fallback: "查看说明",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 对照组：缓存为空 → 只能落到模板 fallback。
			i18n.InjectForTest(nil, nil)
			before := mailSplitGET(t, engine, tc.path).Body.String()
			if !strings.Contains(before, tc.fallback) {
				t.Fatalf("缓存为空时正文没有出现 fallback %q：对照不成立，后面的断言分辨不了两条路径", tc.fallback)
			}
			if strings.Contains(before, tc.sentinel) {
				t.Fatalf("缓存为空时正文出现了哨兵 %q：注入尚未发生，说明前一个用例没复位缓存", tc.sentinel)
			}

			// 实验组：库里「有」这条词条 → 渲染必须读它。
			i18n.InjectForTest(map[string]map[string]string{
				tc.key: {"zh-CN": tc.sentinel, "en-US": tc.sentinel},
			}, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			after := mailSplitGET(t, engine, tc.path).Body.String()
			if !strings.Contains(after, tc.sentinel) {
				t.Errorf("注入词条后正文没有出现哨兵 %q：渲染没有走 DB 值路径（页面可能根本没引用 %s）",
					tc.sentinel, tc.key)
			}
			if strings.Contains(after, tc.fallback) {
				t.Errorf("注入词条后正文仍出现 fallback %q：键存在却没命中取值链 —— "+
					"正是「模板改了、库里没改」这一轮三个缺陷的形态", tc.fallback)
			}
		})
	}
}

// itoa 十进制转换（本文件只需要正数 id）。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
