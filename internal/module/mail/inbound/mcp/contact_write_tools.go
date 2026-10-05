package mailmcp

// contact_write_tools.go — 联系人的增改删、状态变更与打标签。
//
// 依赖收窄到 mailcontract.ContactWriter。刻意**不含** ImportContacts 与
// 从系统用户拉人（PullSystemUsers）：那两个动作会一次性把成百上千人拉进名单，
// 而它们的正确性取决于「谁同意接收」这条政策判断 —— 那是人做决定的事，
// 助手能做的是在用户已经决定之后帮他改一个人、打一批标签。
//
// 这一组里两个「看不见的后果」必须写进描述与回执，否则用户不会知道发生过：
//   · 状态置 subscribed 会**触发订阅类自动化**（下一条欢迎邮件可能当场发出去）；
//   · 打标签会触发标签类自动化（同样当场发信）。
// 两者都不是「只改了一行数据」，而用户说「给他打个 vip 标签」时想的只是分类。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/permission"
)

// ContactWriteTools 返回联系人写工具集。
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

// contactStatusChoices 同意状态取值（与 mailmodel.ContactStatus* 一致）。
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
		permission.MailContactSave,
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
		permission.MailContactSave,
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
		permission.MailContactStatus,
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
		permission.MailContactDelete,
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
		permission.MailContactTag,
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
				// 跳过不等于失败：多半是这些人本来就有 / 本来就没有那个标签。
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

// operatorID 操作人 id；拿不到就 0（service 用它写留痕，0 表示无操作人）。
func operatorID(ctx context.Context) uint64 {
	if id := mcp.UserIDFrom(ctx); id > 0 {
		return uint64(id)
	}
	return 0
}

// cleanTags 去空白、去重、保序。
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

// dedupeUint64 去重保序（重复 id 会让「实际删除数」对不上请求数）。
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
