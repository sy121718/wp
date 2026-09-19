package adminhttp

// admin_err_texts_test.go — ?err= / ?errored= 白名单（adminErrTexts）与写侧生产点的对账。
//
// 为什么这条测试是本任务的一部分而不是附属品：adminPageErrText 未命中落**空串**
// （错误提示的「必须说点什么」由写侧保证）。这条取舍换来了「手拼参数无法伪造系统提示」，
// 代价是**写侧新增文案而忘了登记时静默没有提示** —— 页面上什么都不显示，也没有任何报错。
// 所以必须有一处把「写侧会产出的串」与「读侧认得的串」钉成同一个集合：
//
//   · 候选 → 写侧（TestAdminErrTextsReproducibleFromWriters）：读侧不许凭空多认一句话，
//     多认一句就是多一分伪造面；
//   · 写侧 → 候选（TestAdminErrTextsCoverEveryWriterSource）：写侧每一句话都必须被认得，
//     少认一句就是那条提示静默消失。
//
// 素材一律从写侧的构造函数取（url.Parse 回跳地址 / 直接驱动缺项判定函数），不手抄文案 ——
// 手抄一份就是第二份真相，写侧改了措辞它不会跟着变。写法照 admin_notice_test.go。
//
// 还有两条守卫：候选必须放得进 adminPageErrParamMaxBytes（超限的候选永远命不中）、
// 两次数字归一必须幂等（shell.BulkIDsNoticeTemplate 已归一，FacingNotice 还会再归一）。
// 最后一条启发式源码扫描用来在「写侧新增一个取值来源」时报警 —— 它**不是**覆盖证明，
// 盲区写在那个测试的注释里。

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// adminErrWriterValues 写侧会放进 ?err= / ?errored= 的全部取值（从写侧的构造函数取）。
//
// 清单来自对 internal/module/admin/inbound/http 的 grep 复核（2026-09），逐个写侧生产点：
//
//  1. adminBulkResultURL —— skipped > 0 时 Sprintf(adminBulkPartialTemplate) 进 ?err=
//     （六个列表页名词各一条）；
//  2. adminI18nBulkDeleteResult —— 词条页批量删除「全跳过」与「部分跳过」两个分支，
//     经 adminI18nBackURL(c, "err", …) 回带；
//  3. shell.BulkIDsFacingText —— 批量 id 超限的受控出口（六个列表页的 ?err= 与词条页的
//     ?err= 都是它）；
//  4. adminI18nSaveMissingMsg / adminI18nDeleteMissingMsg → response.TranslateMessage
//     —— 词条表单三个缺项，经 ?errored= 回带；
//  5. adminErrParam —— ?errored= 的另外两条：命中白名单（业务文案）与未命中（ErrInternal 归口）。
func adminErrWriterValues(t *testing.T, c *gin.Context) []string {
	t.Helper()
	values := make([]string, 0, len(adminBulkNouns)+8)

	// 1) 六个列表页的「部分成功」结论（deleted > 0 且 skipped > 0 走 ?err=）。
	for _, noun := range adminBulkNouns {
		loc := adminBulkResultURL("/admin/x", noun, 3, 2)
		parsed, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("写侧回跳地址无法解析：%v", err)
		}
		if got := parsed.Query().Get("err"); got == "" {
			t.Fatalf("有跳过时应走 ?err=（noun=%q）：%s", noun, loc)
		} else {
			values = append(values, got)
		}
	}

	// 2) 词条页批量删除的两个「有跳过」分支（全跳过 / 部分跳过）。
	values = append(values, adminI18nBulkDeleteResult(0, 3), adminI18nBulkDeleteResult(2, 3))

	// 3) 批量 id 超限（shell 的受控出口）。
	values = append(values, shell.BulkIDsFacingText(c, &shell.BulkIDsError{Count: 500, Max: shell.MaxBulkIDs}))

	// 4) 词条表单的缺项（handler 先判缺哪一项，再 TranslateMessage 回带 ?errored=）。
	for _, tc := range []struct{ key, lang, value string }{
		{"", "zh-CN", "x"}, {"a.b", "", "x"}, {"a.b", "zh-CN", "   "},
	} {
		missing := adminI18nSaveMissingMsg(tc.key, tc.lang, tc.value)
		if missing == "" {
			t.Fatalf("保存缺项判定应当命中（%+v）—— 测试素材已经失效", tc)
		}
		values = append(values, response.TranslateMessage(c, missing))
	}
	for _, tc := range []struct{ key, lang string }{{"", "zh-CN"}, {"a.b", ""}} {
		missing := adminI18nDeleteMissingMsg(tc.key, tc.lang)
		if missing == "" {
			t.Fatalf("删除缺项判定应当命中（%+v）—— 测试素材已经失效", tc)
		}
		values = append(values, response.TranslateMessage(c, missing))
	}

	// 5) ?errored= 的两条归口出口：命中白名单（业务文案）与未命中（归口文案）。
	for _, key := range adminenums.AdminFacingMessages {
		values = append(values, adminErrParam(c, errors.New(key)))
	}
	values = append(values, adminErrParam(c, errors.New(pageDBErrText)))

	return values
}

// TestAdminErrTextsReproducibleFromWriters 候选 → 写侧：读侧认的每一句话，写侧都真的产出过。
func TestAdminErrTextsReproducibleFromWriters(t *testing.T) {
	c := newPageErrContext(t)
	writers := adminErrWriterValues(t, c)
	cands := adminErrTexts(c)
	if len(cands) == 0 {
		t.Fatal("候选为空 —— 白名单没建起来，?err= 会永远落空串")
	}
	for _, cand := range cands {
		hit := false
		for _, v := range writers {
			if shell.FacingNotice(v, []string{cand}) != "" {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("候选 %q 在写侧任何生产点都复现不出来 —— 读侧凭空多认了一句话（伪造面）", cand)
		}
	}
}

// TestAdminErrTextsCoverEveryWriterSource 写侧 → 候选：写侧产出的每一句话都必须被认出来。
func TestAdminErrTextsCoverEveryWriterSource(t *testing.T) {
	c := newPageErrContext(t)
	values := adminErrWriterValues(t, c)
	if len(values) < len(adminBulkNouns)+5 {
		t.Fatalf("写侧素材只有 %d 条 —— 清单漏了生产点，这条用例已经失去意义", len(values))
	}
	for _, raw := range values {
		if got := adminPageErrText(c, raw); got != raw {
			t.Errorf("写侧文案应被受控出口原样放行，实际 %q（raw=%q）—— 这条提示会在页面上消失", got, raw)
		}
	}
}

// TestAdminErrTextsRejectUnregisteredCopy 反向：没登记在写侧的文案仍然落空串。
//
// 与上一条成对：只钉「写侧的认得」会漏掉「白名单退化成全部放行」，只钉「伪造的拒掉」会漏掉
// 「写侧的悄悄消失」。两条都要在。
func TestAdminErrTextsRejectUnregisteredCopy(t *testing.T) {
	c := newPageErrContext(t)
	for _, raw := range []string{
		"系统维护中，请稍后重试",
		"角色编码已存在了",                    // 近似但不等于任何候选
		"已删除 3 个用户",                   // 名词不在 adminBulkNouns 里
		"已删除 3 个角色，2 个未能删除（受保护）",      // 少半句
		"已删除 3 个角色，2 个未能删除（受保护或被引用）。", // 多一个句号
	} {
		if got := adminPageErrText(c, raw); got != "" {
			t.Errorf("未登记的文案应落空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestAdminErrTextsFitMaxBytes 候选必须放得进 adminPageErrParamMaxBytes。
//
// 超限的候选永远不可能被命中（原始参数先被 adminPageErrParamClean 截到 200 字节，
// 截断后的串必然对不上任何候选）—— 表现是「写侧发了提示、页面上什么都不显示」。
// 这条把「将来加一句更长的文案」从静默失败变成红灯。
//
// 诚实说明：本包单跑时 i18n 未初始化，译文回退成 key（ASCII，天然够短），所以这里钉住的
// 是「当前语言下候选集合不超限」这个运行时事实；中文原文与英文译文的长度由 adminenums
// 与词条库管，本测试看不到。
func TestAdminErrTextsFitMaxBytes(t *testing.T) {
	c := newPageErrContext(t)
	for _, cand := range adminErrTexts(c) {
		if len(cand) > adminPageErrParamMaxBytes {
			t.Errorf("候选 %q 有 %d 字节，超过 adminPageErrParamMaxBytes=%d —— 它永远命不中，提示会静默消失",
				cand, len(cand), adminPageErrParamMaxBytes)
		}
	}
}

// TestAdminErrTextsAcceptsBulkIDsNotice 两次数字归一不能互相破坏。
//
// shell.BulkIDsNoticeTemplate(c) 返回的是**已归一**（占位 "0"）的串，而 shell.FacingNotice
// 内部还会对候选再做一次归一。归一是幂等的（"0" 本身是数字，再归一还是 "0"），这条用写侧
// 真实产出把它钉住 —— 归一一旦不幂等，超限提示就会从「请分批进行」退化成「什么都不显示」。
func TestAdminErrTextsAcceptsBulkIDsNotice(t *testing.T) {
	c := newPageErrContext(t)
	for _, count := range []int{500, 3, 200} {
		text := shell.BulkIDsFacingText(c, &shell.BulkIDsError{Count: count, Max: shell.MaxBulkIDs})
		if got := adminPageErrText(c, text); got != text {
			t.Fatalf("批量上限提示应被读侧认出，got %q（写侧文案 %q，count=%d）", got, text, count)
		}
	}
}

// —— 写侧取值来源的启发式扫描 ——

// adminErrHintProducerAllowlist 写侧 ?err= / ?errored= 取值表达式的允许来源（首标识符 → 理由）。
var adminErrHintProducerAllowlist = map[string]string{
	"fmt.Sprintf":               "adminBulkResultURL 用它把 adminBulkPartialTemplate 填成整句",
	"shell.BulkIDsFacingText":   "批量 id 超限的受控出口",
	"adminErrParam":             "命中白名单 / 未命中归口的统一出口",
	"adminI18nBulkDeleteResult": "词条页批量删除的结论文案",
	"response.TranslateMessage": "词条表单缺项文案的直接翻译",
}

// adminErrHintValueRe 抓「?err= / ?errored= 的取值表达式」的首标识符。
//
// 两种写法：列表页的字符串拼接（path + "?err=" + url.QueryEscape(EXPR…)）
// 与词条页的 adminI18nBackURL(c, "err"|"errored", EXPR)。\s 含换行，跨行调用也认。
var adminErrHintValueRe = regexp.MustCompile(
	`(?:\?err="\s*\+\s*url\.QueryEscape\(\s*([A-Za-z_][A-Za-z0-9_.]*)` +
		`|(?:adminI18nBackURL\(\s*c\s*,\s*"(?:err|errored)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)))`)

// adminErrHintLocalOrigin 解析一跳局部中转：ident := X( / ident = X( 时返回 X。
// 只解析一跳 —— 多跳的写法看不见（盲区已写在下面那条测试的注释里）。
func adminErrHintLocalOrigin(src, ident string) (string, bool) {
	re := regexp.MustCompile(`(?:^|[^\w.])` + regexp.QuoteMeta(ident) + `\s*:?=\s*([A-Za-z_][A-Za-z0-9_.]*)`)
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		if m[1] != ident {
			return m[1], true
		}
	}
	return "", false
}

// TestAdminErrHintSourcesAreKnownProducers 写侧 ?err= / ?errored= 的取值来源必须是已知生产点。
//
// **这是启发式，门禁绿 ≠ 问题不存在**。它按源码文本抓两种写法，盲区明写在这里：
//
//	· 只认「"?err=" + url.QueryEscape(...)」与「adminI18nBackURL(c, "err"|"errored", ...)」
//	  两种形状；第三种写法（先算好整个 Location 再 Redirect、把取值藏进辅助函数）看不到；
//	· 局部变量中转只解析**一跳**（msg = adminI18nBulkDeleteResult(...) 认得，再套一层不认得）；
//	· 只扫本包的 .go（非测试）文件，别的模块新开 ?err= 出口看不到；
//	· 抓到的标识符只比对**名字**，有人把别的东西命名成 shell.BulkIDsFacingText 照样能过。
//
// 它的价值是「新增一个来源时有人会去看一眼」，不是「证明不存在未知来源」。
// 真正的覆盖判据是上面两条对账用例（写侧实际产出的串必须被认得）。
func TestAdminErrHintSourcesAreKnownProducers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败：%v", err)
	}
	scanned, hits := 0, 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("读取 %s 失败：%v", f, rerr)
		}
		scanned++
		src := string(raw)
		for _, m := range adminErrHintValueRe.FindAllStringSubmatch(src, -1) {
			expr := m[1]
			if expr == "" {
				expr = m[2]
			}
			hits++
			if _, ok := adminErrHintProducerAllowlist[expr]; ok {
				continue
			}
			if via, ok := adminErrHintLocalOrigin(src, expr); ok {
				if _, ok2 := adminErrHintProducerAllowlist[via]; ok2 {
					continue
				}
				t.Errorf("%s：?err= 的取值来自局部变量 %s，它由 %s 产出 —— 该生产者不在已知清单里", f, expr, via)
				continue
			}
			t.Errorf("%s：?err= 的取值来自未知表达式 %q —— 新增来源时请同时登记进 adminErrTexts 与"+
				"本测试的清单（internal/module/admin/inbound/http/admin_err_texts_test.go）", f, expr)
		}
	}
	if scanned == 0 || hits == 0 {
		t.Fatalf("没扫到任何写侧取值（scanned=%d hits=%d）—— 目录或写法变了，这条启发式会静默失效",
			scanned, hits)
	}
}
