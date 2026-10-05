package usermcp

// customer_write_tools.go — 客户账号状态的两个写工具（停用/启用、解除登录锁定）。
//
// 依赖收窄到 usercontract.CustomerWriter（两个方法），拿不到改邮箱 / 改密码 /
// 改角色的能力 —— 那些确实不该由模型代劳。
//
// 边界写在工具说明里而不是靠自觉：
//   · 停用是不可逆地影响**这个人当下能不能登录**，描述里要求先 customer_get 核对身份、
//     再把「谁、现在什么状态、要改成什么」念给用户确认；
//   · 停用是可恢复的（再启用即可）且不删数据，所以给它留了通道；
//     真删账号这类动作没有对应工具。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/permission"
)

// WriteTools 返回客户模块的写工具集。
func WriteTools(w usercontract.CustomerWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("usermcp: 客户写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{customerSetStatus(w), customerUnlock(w)}, nil
}

// 状态取值：与 enums 的常量一一对应，不另造一套说法。
const (
	customerStatusActiveArg   = "active"
	customerStatusDisabledArg = "disabled"
)

type customerSetStatusArgs struct {
	CustomerID uint64 `json:"customerId"`
	Status     string `json:"status"`
}

func customerSetStatus(w usercontract.CustomerWriter) mcp.Tool {
	return mcp.NewWrite("customer_set_status", "启用 / 停用客户账号",
		"把某个客户账号设为正常（active）或停用（disabled）。\n"+
			"停用会让他**立刻登不上**，但数据全部保留，再启用即可恢复。\n"+
			"**执行前必须先做两件事**：① 用 customer_get 确认这个 id 到底是谁（核对邮箱或昵称）；"+
			"② 把「谁、从什么状态改成什么」念给用户确认。别在用户只说了「把那个刷单的停了」时自己挑一个 id 猜。\n"+
			"需要 customerId 时用 customer_find 按邮箱 / 昵称搜。",
		permission.UserCustomerStatus,
		mcp.Object("客户账号状态参数", map[string]mcp.Schema{
			"customerId": mcp.Integer("客户 id（用 customer_find / customer_get 拿到的 id）"),
			"status": mcp.Enum("目标状态：active=正常（可登录），disabled=停用（登不上，数据保留）",
				customerStatusActiveArg, customerStatusDisabledArg),
		}, "customerId", "status"),
		nil,
		func(ctx context.Context, args customerSetStatusArgs) (mcp.Result, error) {
			status, ok := customerStatusValueOf(args.Status)
			if !ok {
				return mcp.Result{}, &mcp.ArgsError{Msg: "status 只能是 active 或 disabled"}
			}
			if args.CustomerID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "customerId 必填：先用 customer_find 按邮箱或昵称找到人"}
			}
			res, err := w.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{
				CustomerID: args.CustomerID,
				Status:     status,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: statusChangeText(res)}, nil
		})
}

// customerStatusValueOf 工具层的说法 → 落库取值。
//
// 不用「空串当默认」：这里的默认值含义截然不同（0 是**停用**），
// 把没给的参数当 0 处理会把一次手滑变成一次停用。
func customerStatusValueOf(v string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case customerStatusActiveArg:
		return userenums.StatusActive, true
	case customerStatusDisabledArg:
		return userenums.StatusDisabled, true
	default:
		return 0, false
	}
}

// statusChangeText 状态变更回执。
//
// 停用要说清「他立刻登不上」，并给出恢复办法 —— 用户按下这个按钮之后最常见的
// 下一个问题是「我刚才是不是弄错了」，回答里没有恢复路径他就得自己找。
func statusChangeText(res *userdto.CustomerStatusResp) string {
	label := res.StatusLabel
	if strings.TrimSpace(label) == "" {
		if _, fallback := userenums.StatusLabel(res.Status); strings.TrimSpace(fallback) != "" {
			label = fallback
		} else {
			label = fmt.Sprint(res.Status)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "客户 %d 已置为「%s」。", res.CustomerID, label)
	if res.Status == userenums.StatusDisabled {
		b.WriteString("\n他现在登不上；数据都在，需要时把 status 设回 active 即可恢复。")
	} else {
		b.WriteString("\n他现在可以正常登录。")
	}
	return b.String()
}

type customerUnlockArgs struct {
	CustomerID uint64 `json:"customerId"`
}

func customerUnlock(w usercontract.CustomerWriter) mcp.Tool {
	return mcp.NewWrite("customer_unlock", "解除客户登录锁定",
		"清掉某个客户的登录锁定与失败次数（连续输错密码会被自动锁定，这个工具就是运营侧的解封）。\n"+
			"与 customer_set_status 的区别：**锁定是按时间自动解除的临时状态，停用是管理动作**。"+
			"被锁的账号本来就会自己恢复，只是要等；停用不会自己恢复。用户说「登不上」时先看清是哪一种（customer_get 里有）。\n"+
			"如果账号本来就没锁，工具会如实告诉你「本来就没锁」—— 那不是失败。",
		permission.UserCustomerUnlock,
		mcp.Object("解除登录锁定参数", map[string]mcp.Schema{
			"customerId": mcp.Integer("客户 id（用 customer_find / customer_get 拿到的 id）"),
		}, "customerId"),
		nil,
		func(ctx context.Context, args customerUnlockArgs) (mcp.Result, error) {
			if args.CustomerID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "customerId 必填：先用 customer_find 按邮箱或昵称找到人"}
			}
			res, err := w.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: args.CustomerID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: unlockText(res)}, nil
		})
}

// unlockText 解锁回执。
//
// Unlocked 与 Cleared 是两件事，分开说：运营点了个不会变化的按钮时需要知道的
// 是「它本来就没锁」，而不是又一次看到「已解除」然后下次再来点一遍。
func unlockText(res *userdto.CustomerUnlockResp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "客户 %d：", res.CustomerID)
	switch {
	case res.Unlocked && res.Cleared:
		b.WriteString("已解除登录锁定，并清空了积攒的登录失败次数。")
	case res.Unlocked:
		b.WriteString("已解除登录锁定。")
	case res.Cleared:
		b.WriteString("当时并没有处于锁定状态，只清空了残留的登录失败次数（这些计数会让它更容易被再次锁上）。")
	default:
		b.WriteString("本来就没有锁定，也没有失败计数 —— 没有做任何改动。")
	}
	return b.String()
}
