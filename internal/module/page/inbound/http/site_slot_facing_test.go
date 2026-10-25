package pagehttp

// site_slot_facing_test.go — 站点槽位页文案的**键形态**判定回归。
//
// 历史背景：这里曾经是真缺陷 —— 写侧把取好词的中文文案放进 ?err=，读侧却按错误常量名查，
// 于是所有业务错误都被判为未命中、一律回落「系统内部错误，请稍后重试」。
//
// task-152 之后这段判定搬到了共享出口 adminui.PageErrorFact（判据是
// pkg/response.IsBusinessError 的**形态**正则），本模块不再有白名单；站点槽位页自己的
// 成功 / 参数级文案则退化成「key + 中文兜底表」（siteSlotFacingMessages 只提供兜底，
// 不再充当可透出性的判据）。这条测试钉住这两件事：
//
//  1. 页内自造文案的 key 与兜底**成对存在**（缺兜底 → 词条缺失时页面上是裸 key）；
//  2. 错误转文案走共享出口且**不许用包含匹配绕过** —— 夹带一段已知 key 的串不该命中，
//     否则「只要夹带就能透出」的口子又回来了。
//
// 断言里的中文一律**从 map 里取**，不手抄：手抄的文案在有人改兜底表时会让测试
// 在「谁都没写错」的情况下变红，而那种红最后总是被改成「把测试的字符串也改掉」——
// 于是测试就失去意义了。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
)

func TestSiteSlotMessagesArePairedFacts(t *testing.T) {
	for _, key := range []string{
		siteSlotBoundText, siteSlotUnboundText, siteSlotNoProjectText, siteSlotNoPageText,
	} {
		fallback, ok := siteSlotFacingMessages[key]
		if !ok || strings.TrimSpace(fallback) == "" {
			t.Fatalf("文案 %s 缺少中文兜底（词条缺失时页面上会显示裸 key）", key)
		}
		msg := siteSlotMsg(key)
		if msg.Key != key || msg.Fallback != fallback {
			t.Fatalf("siteSlotMsg(%s) 应给出 key + 表内兜底，实际 %+v", key, msg)
		}
	}
}

func TestSiteSlotErrorsGoThroughSharedExit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/site-slots/bind", nil)

	// 业务 sentinel：判据是形态（Err* 常量名），不需要本页登记过。
	msg := pageErrMsgQuiet(c, errors.New(pageenums.ErrSlotPageMiss))
	if !strings.Contains(msg.Key, string(pageenums.ErrSlotPageMiss)) || msg.Key != string(pageenums.ErrSlotPageMiss) {
		t.Fatalf("业务 sentinel 应原样带出 key，实际 %q", msg.Key)
	}

	// 基础设施错误：归口，原文只进日志。
	leaky := errors.New(`pq: relation "pages" does not exist (SQLSTATE 42P01)`)
	fallback := pageErrMsgQuiet(c, leaky)
	if fallback.Key != adminui.MsgInternalError {
		t.Fatalf("未命中的错误应落归口 key，实际 %q", fallback.Key)
	}
	for _, tok := range []string{"SQLSTATE", "relation"} {
		if strings.Contains(pageMsgText(c, fallback), tok) {
			t.Fatalf("归口文案泄漏了内部细节 %q", tok)
		}
	}

	// 形态判据是**锚定的整体匹配**：夹带一段已知 key 的串不该命中。
	for _, raw := range []string{
		"<script>alert(1)</script>" + string(pageenums.ErrSlotPageMiss),
		string(pageenums.ErrSlotPageMiss) + "<script>alert(1)</script>",
	} {
		if got := pageErrMsgQuiet(c, errors.New(raw)); got.Key != adminui.MsgInternalError {
			t.Fatalf("夹带内容不该命中业务错误形态，实际 %q", got.Key)
		}
	}
}

// —— 展示字段不再由 Go 产出（task-103 的反向断言）——

// TestSiteSlotRowHasNoDisplayFields 行投影只带事实：徽章类名与已取词文案都必须由模板产出。
//
// 正向判据（模板按事实选档、优先级、缺键兜底）在 internal/templates/site_structure_i18n_test.go；
// 这里钉反向面 —— 谁把 Badge / StateLabel 加回投影，就会出现「Go 与模板各写一套档位」的双真源：
// 模板只读事实（改了档位规则却看不到 Go 那份），而 Go 那份没人读，两边悄悄分叉。
func TestSiteSlotRowHasNoDisplayFields(t *testing.T) {
	base := pagecontract.SiteSlotItemResp{DraftPath: "/checkout"}
	cases := []struct {
		name string
		it   pagecontract.SiteSlotItemResp
	}{
		{name: "未绑定", it: base},
		{name: "绑定但页面已删除", it: func() pagecontract.SiteSlotItemResp { it := base; it.Bound, it.PageDeleted = true, true; return it }()},
		{name: "绑定且已发布", it: func() pagecontract.SiteSlotItemResp { it := base; it.Bound, it.Published = true, true; return it }()},
		{name: "绑定但未发布", it: func() pagecontract.SiteSlotItemResp { it := base; it.Bound = true; return it }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := siteSlotRow(tc.it, nil)
			// 视图换强类型之后判据从「map 有没有这个键」变成「类型有没有这个字段」：
			// 前者拼错键名静默通过，后者拼错是编译错误。
			typ := reflect.TypeOf(row)
			for _, field := range []string{"Badge", "StateLabel"} {
				if _, ok := typ.FieldByName(field); ok {
					t.Errorf("行投影不该再产出展示字段 %q：档位与文案由模板按事实选（见 site_slots.html）", field)
				}
			}
			// 事实字段必须在：模板靠它们选档，缺了会把「已发布」渲染成「未绑定」。
			for _, field := range []string{"Bound", "PageDeleted", "Published"} {
				if _, ok := typ.FieldByName(field); !ok {
					t.Errorf("行投影缺少事实字段 %q（模板将无从选档）", field)
				}
			}
		})
	}
}

// TestSiteSlotsPageDataHasNoToneFields 页头两个档位同样由模板按计数选。
//
// 判据是 `n != 0`（含负数非零）—— 那属于展示判断，所以 Go 只给计数事实。
func TestSiteSlotsPageDataHasNoToneFields(t *testing.T) {
	slots := &pagecontract.SiteSlotListResp{
		Total: 2, BoundCount: 1,
		Items: []pagecontract.SiteSlotItemResp{
			{Slot: "checkout", Bound: true, Published: true, DraftPath: "/checkout"},
			{Slot: "cart", Bound: true, DraftPath: "/cart"},
		},
	}
	data := siteSlotPageData(nil, "", nil, slots, pageMsg{})
	typ := reflect.TypeOf(*data)
	for _, field := range []string{"UnpublishedTone", "DeletedTone"} {
		if _, ok := typ.FieldByName(field); ok {
			t.Errorf("页面数据不该再产出档位字段 %q：档位由模板按计数事实选（见 site_slots.html）", field)
		}
	}
	for _, field := range []string{"UnpublishedCount", "DeletedCount", "BoundCount", "Total"} {
		if _, ok := typ.FieldByName(field); !ok {
			t.Errorf("页面数据缺少计数事实字段 %q（模板将无从选档）", field)
		}
	}
}
