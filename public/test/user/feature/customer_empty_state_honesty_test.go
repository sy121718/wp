package feature

// customer_empty_state_honesty_test.go — 客户管理页空态的**主行动条件渲染**回归
//（docs/02-O-trade-site-audit.md 的 customers T1 / T2 / T3，customer_detail T1）。
//
// 背景：空态里那个「重置」**无条件渲染**时，无筛选的用户点下去 URL 与页面逐字不变
//（href == 当前 URL）—— 判据 1「操作流闭环：点了没反应」，而且比没有按钮更糟：
// 用户会怀疑按钮坏了，反复点。同一批还修了库值（模板里的中文只是 t() 兜底，
// 词条命中时显示的是 sys_i18n 里的值）与状态按钮的动词（英文界面中英混排）。
//
// 本文件走真 handler + 真模板 + 真库（复用 customer_page_e2e_test.go 的环境），
// 因此断言的是**页面真的渲染出来的东西**，不是模板源码里有没有某个字符串。

import (
	"strconv"
	"strings"
	"testing"

	usermodel "go_wp/internal/module/user/model"
)

// TestCustomersEmptyStateActionsFollowFilter 无筛选时不给「重置」（02-O customers T1）。
func TestCustomersEmptyStateActionsFollowFilter(t *testing.T) {
	env := newCustomerE2EEnv(t)
	if env == nil {
		return
	}

	bare := env.get(t, "/admin/customers").Body.String()
	if strings.Contains(bare, `class="empty-actions"`) {
		t.Error("无筛选时不该渲染空态主行动 —— 那个「重置」指向本页自己，点了没有任何变化")
	}
	if !strings.Contains(bare, "还没有客户") {
		t.Error("无筛选时空态标题应是「还没有客户」（「没有符合条件的客户」在无筛选时自相矛盾）")
	}
	if !strings.Contains(bare, "后台不能直接新建") {
		t.Error("无筛选时空态说明应回答「能不能在这儿建客户」：后台不能直接新建")
	}

	filtered := env.get(t, "/admin/customers?keyword=ZZPROBEXYZZ").Body.String()
	if !strings.Contains(filtered, `class="empty-actions"`) {
		t.Fatal("带着筛选筛空时应给「重置」这个出路")
	}
	if strings.Contains(emptyActionHrefOf(filtered), "keyword=") {
		t.Errorf("「重置」不能把筛选条件带回去（那等于没重置）：%s", emptyActionHrefOf(filtered))
	}
}

// emptyActionHrefOf 摘出第一个空态主行动的 href（空态里只有一两个行动）。
func emptyActionHrefOf(body string) string {
	i := strings.Index(body, `class="empty-actions"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, `href="`)
	if j < 0 || j > 400 {
		return ""
	}
	tail := rest[j+len(`href="`):]
	if k := strings.Index(tail, `"`); k > 0 {
		return tail[:k]
	}
	return ""
}

// TestCustomerStatusActionVerbIsTranslatable 状态按钮的动词不得是硬编码中文。
//
// 详情页的按钮是「<动词> + account_suffix」拼出来的，而后缀早已中英成对
// （zh「这个账号」/ en「 this account」）—— 动词留在 Go 里当纯中文时，英文界面就是
// 「Disable这个账号」式中英混排（02-O customer_detail T1）。判据因此不是「按钮上写着停用」，
// 而是「两段都来自词条」：页面在中文下拼出「停用这个账号」，且动词与后缀各自可取词。
func TestCustomerStatusActionVerbIsTranslatable(t *testing.T) {
	env := newCustomerE2EEnv(t)
	if env == nil {
		return
	}
	customer := env.mkE2ECustomer(t, "zoe", "zoe@example.com", "佐伊", usermodel.UserStatusActive)

	list := env.get(t, "/admin/customers").Body.String()
	if !strings.Contains(list, "停用") {
		t.Error("列表页状态按钮看不清动作（应显示「停用」）")
	}

	detail := env.get(t, "/admin/customers/detail?id="+strconv.FormatUint(customer.ID, 10)).Body.String()
	if !strings.Contains(detail, "停用这个账号") {
		t.Error("详情页按钮应是「停用这个账号」—— 动词与后缀两段都走词条后仍应拼成同一句")
	}
	if strings.Contains(detail, "Disable这个账号") {
		t.Error("出现了中英混排的按钮文案")
	}
}
