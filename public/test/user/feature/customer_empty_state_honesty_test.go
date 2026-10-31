package feature

import (
	"strconv"
	"strings"
	"testing"

	usermodel "go_wp/internal/module/user/model"
)

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
