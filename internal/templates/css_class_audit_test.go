package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	reAuditClassAttr  = regexp.MustCompile(`class\s*=\s*"([^"]*)"`)
	reAuditJetExpr    = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	reAuditClassName  = regexp.MustCompile(`\.?className\s*=\s*([^;\n]+)`)
	reAuditJSLiteral  = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)
	reAuditJSCompare  = regexp.MustCompile(`[=!]==?\s*'[^']*'|[=!]==?\s*"[^"]*"`)
	reAuditJSGetAttr  = regexp.MustCompile(`getAttribute\(\s*'[^']*'\s*\)|getAttribute\(\s*"[^"]*"\s*\)`)
	reAuditJSOrDeflt  = regexp.MustCompile(`\|\|\s*'[^']*'|\|\|\s*"[^"]*"`)
	reAuditCSSClass   = regexp.MustCompile(`\.([a-zA-Z][a-zA-Z0-9_-]*)`)
	reAuditCSSURL     = regexp.MustCompile(`(?i)url\([^)]*\)`)
	reAuditCSSDQuote  = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	reAuditCSSSQuote  = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	reAuditInlineCSS  = regexp.MustCompile(`(?s)<style>(.*?)</style>`)
	reAuditStaticTok  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	reAuditDynPrefix  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*-$`)
	reAuditLooseToken = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_-]*`)
	reAuditJSConcat   = regexp.MustCompile(`['"]\s*\+|\+\s*['"]|\$\{`)
)

type cssAuditFacts struct {
	defined     map[string][]string
	definedOwn  map[string][]string
	usedStrict  map[string][]string
	looseTokens map[string]bool
	goClasses   map[string][]string
	dynPrefixes map[string][]string
}

func TestCSSClassesUsedButUndefined(t *testing.T) {
	facts := collectCSSAuditFacts(t)

	missing := auditMissing(facts.usedStrict, func(cls string) bool {
		if len(facts.defined[cls]) > 0 {
			return false
		}
		if auditDynamicCovered(cls) {
			return false
		}
		return auditFamilyDefined(facts.defined, cls)
	}, cssUndefinedAllowed)

	if len(missing) > 0 {
		var b strings.Builder
		b.WriteString("这些类被模板/JS 写死使用，但定义侧（static/css、static/vendor、模板内联 <style>）里没有定义 —— 页面不会报错，只会静默失去样式：\n")
		for _, cls := range missing {
			b.WriteString("  · " + cls + "\n")
			for _, site := range head(facts.usedStrict[cls], 3) {
				b.WriteString("      使用点 " + site + "\n")
			}
		}
		b.WriteString("修法：补样式，或确认它是纯结构钩子后加进 cssUndefinedAllowed 并写明理由。")
		t.Error(b.String())
	}

	for _, cls := range staleUndefinedAllowed(facts) {
		t.Errorf("cssUndefinedAllowed 里的 %q 已经不再是「用了但没定义」（类已被定义，或使用点已消失）：清单必须同步收缩，只增不减等于门禁失效", cls)
	}
}

func TestCSSClassesDefinedButUnused(t *testing.T) {
	facts := collectCSSAuditFacts(t)

	unused, prefixCovered := auditUnused(facts.definedOwn, facts.usedStrict, facts.looseTokens, cssUnusedAllowed)
	if len(unused) > 0 {
		var b strings.Builder
		b.WriteString("这些类在定义侧存在，但使用侧（模板 + JS 文本）里以 token 边界找不到任何出现 —— 要么是死样式，要么使用点藏在扫描范围之外：\n")
		for _, cls := range unused {
			b.WriteString("  · " + cls + "  ← " + strings.Join(facts.defined[cls], ", ") + "\n")
		}
		b.WriteString("修法：确认为死样式则删掉 CSS（下一批统一清账），暂不清理则加进 cssUnusedAllowed 并写明理由（如「JS 运行时拼接生成，前缀 is-」）。")
		t.Error(b.String())
	}

	for _, cls := range staleUnusedAllowed(facts) {
		entry := cssUnusedAllowed[cls]
		if strings.Contains(entry, "待清理") {
			t.Errorf("cssUnusedAllowed 里的 %q 标着「待清理」，但它已经不再命中未使用集合（类已被删除，或已经有人用了）：该条目连同 CSS 一起收口", cls)
			continue
		}
		t.Errorf("cssUnusedAllowed 里的 %q 已经不再命中未使用集合（类已被定义处删除，或已经有人用了）：清单必须同步收缩", cls)
	}

	if len(facts.goClasses) < 10 {
		t.Fatalf("Go 侧只扫到 %d 个 class 字面量（预期 ≥10）：扫描口径或路径可能已失效", len(facts.goClasses))
	}
	var goUndefined []string
	for cls := range facts.goClasses {
		if len(facts.defined[cls]) == 0 {
			goUndefined = append(goUndefined, cls)
		}
	}
	sort.Strings(goUndefined)
	t.Logf("Go 侧 class 字面量扫出 %d 个类名（出处 %d 个文件）；其中 %d 个在定义侧没有对应规则（仅提示，不判失败）：%v",
		len(facts.goClasses), len(goFilesOf(facts)), len(goUndefined), goUndefined)

	if len(prefixCovered) > 0 {
		sorted := make([]string, 0, len(prefixCovered))
		for cls := range prefixCovered {
			sorted = append(sorted, cls)
		}
		sort.Strings(sorted)
		t.Logf("以下 %d 个类靠动态证据放行（前缀 %s；拼接点与取值域见 dynamicClassEvidence）：%s",
			len(sorted), strings.Join(sortedPrefixKeys(facts.dynPrefixes), ", "), strings.Join(sorted, ", "))
	}
}

func auditMissing(used map[string][]string, keep func(string) bool, allowed map[string]string) []string {
	var missing []string
	for cls := range used {
		if !keep(cls) {
			continue
		}
		if _, ok := allowed[cls]; ok {
			continue
		}
		missing = append(missing, cls)
	}
	sort.Strings(missing)
	return missing
}

func auditUnused(defined map[string][]string, strict map[string][]string, loose map[string]bool, allowed map[string]string) ([]string, map[string]bool) {
	var unused []string
	prefixCovered := map[string]bool{}
	for cls := range defined {
		if loose[cls] {
			continue
		}
		if len(strict[cls]) > 0 {
			continue
		}
		if auditDynamicCovered(cls) {
			prefixCovered[cls] = true
			continue
		}
		if _, ok := allowed[cls]; ok {
			continue
		}
		unused = append(unused, cls)
	}
	sort.Strings(unused)
	return unused, prefixCovered
}

func goFilesOf(f cssAuditFacts) map[string]bool {
	files := map[string]bool{}
	for _, sites := range f.goClasses {
		for _, s := range sites {
			files[s] = true
		}
	}
	return files
}

func staleUndefinedAllowed(f cssAuditFacts) []string {
	var stale []string
	for cls := range cssUndefinedAllowed {
		switch {
		case len(f.usedStrict[cls]) == 0:
			stale = append(stale, cls)
		case len(f.defined[cls]) > 0:
			stale = append(stale, cls)
		case auditDynamicCovered(cls) || !auditFamilyDefined(f.defined, cls):
			stale = append(stale, cls)
		}
	}
	sort.Strings(stale)
	return stale
}

func staleUnusedAllowed(f cssAuditFacts) []string {
	var stale []string
	for cls := range cssUnusedAllowed {
		switch {
		case len(f.definedOwn[cls]) == 0:
			stale = append(stale, cls)
		case f.looseTokens[cls] || len(f.usedStrict[cls]) > 0:
			stale = append(stale, cls)
		case auditDynamicCovered(cls):
			stale = append(stale, cls)
		}
	}
	sort.Strings(stale)
	return stale
}

func auditDynamicCovered(cls string) bool {
	for prefix, ev := range dynamicClassEvidence {
		if !strings.HasPrefix(cls, prefix) {
			continue
		}
		if len(ev.values) == 0 {
			return true
		}
		suffix := strings.TrimPrefix(cls, prefix)
		for _, v := range ev.values {
			if v == suffix {
				return true
			}
		}
	}
	return false
}

func auditFamilyDefined(defined map[string][]string, cls string) bool {
	for _, parent := range auditParentCandidates(cls) {
		if len(defined[parent]) > 0 {
			return true
		}
	}
	return false
}

func auditParentCandidates(cls string) []string {
	var out []string
	add := func(s string) {
		if s == "" || s == cls || contains(out, s) {
			return
		}
		out = append(out, s)
	}
	if i := strings.Index(cls, "--"); i > 0 {
		add(cls[:i])
	}
	if i := strings.Index(cls, "__"); i > 0 {
		add(cls[:i])
	}
	parts := strings.Split(cls, "-")
	for i := len(parts) - 1; i > 0; i-- {
		add(strings.Join(parts[:i], "-"))
	}
	return out
}

type dynPrefixEvidence struct {
	site   string
	values []string
	note   string
}

var dynamicClassEvidence = map[string]dynPrefixEvidence{
	"is-": {
		site: "ui/toast.js:61 `'wb-toast is-' + type`；media-admin.js:334 `'media-variant-badge is-' + (v.status || 'pending')`；workbench/methods/canvas.js `'wb-canvas-frame is-' + cfg.bp`；fragments/seo_score.html:33 `is-{{sec.Color}}`",
		values: []string{
			"info", "success", "error",
			"pending", "processing", "ready", "failed",
			"desktop", "tablet", "mobile",
			"green", "lightgreen", "yellow", "red", "red-blocking",
		},
		note: "四个拼接点各自的取值域 —— toast type {info,success,error}（toast.js:15 的注释）；媒体变体 status {pending,processing,ready,failed}（media-admin.js:331 的 texts 映射键，与 media-lib.css:213-216 的四个变体一一对应）；断点 bp {desktop,tablet,mobile}（workbench/methods/state.js:16-18）；SEO 维度色 {green,lightgreen,yellow,red,red-blocking}（workbench.css:790-794 的五个变体，另见 scoring_test.go:70 的 red-blocking）",
	},
	"is-drop-": {
		site:   "workbench/methods/tree.js:106 `classList.add('is-drop-target', 'is-drop-' + placement)`",
		values: []string{"before", "after", "inside"},
		note:   "placement 取值见同文件 111/120 行的 remove 列表",
	},
	"wb-drop-": {
		site:   "workbench/methods/canvas.js:563 `target.classList.add('wb-drop-' + placement)`",
		values: []string{"before", "after", "inside"},
		note:   "取值见 canvas.js:544-545 的 remove 列表",
	},
	"perm-type-": {
		site:   "admin/system/role_permissions.html:100 `class=\"perm-type perm-type-{{r.Type}}\"`",
		values: nil,
		note:   "取值域是权限类型枚举（由 DB 的 permissions.type 驱动，静态枚举不出来）→ 退回前缀放行",
	},

	"badge-": {
		site: "admin/ai/mcp.html:41 `badge-{{.McpTone}}`；admin/ai/provider_card.html:15 `badge-{{.StatusTone}}`；" +
			"admin/ai/provider_card.html:31 与 admin/ai/provider_models.html:19 `badge-{{.ModelsNoticeTone}}`；" +
			"admin/ai/sessions.html:402 `badge-{{e.Tone}}`；" +
			"admin/purchase/purchases.html:236 `badge-{{l.OutstandingTone}}`；" +
			"admin/mail/mail_campaign.html:126 `badge-{{p.StatusTone}}`；" +
			"admin/order/coupons.html:175 `badge-{{cp.StateTone}}`；" +
			"admin/order/orders.html:70 `badge-{{tab.Tone}}`、:158 `badge-{{o.StatusTone}}`、:186 `badge-{{head.StatusTone}}`；" +
			"admin/order/returns.html:61 `badge-{{r.StatusTone}}`、:111 `badge-{{o.StatusTone}}`、:207 `badge-{{tab.Tone}}`、:297 `badge-{{rt.StatusTone}}`；" +
			"admin/page/page_redirects.html:86/87 `badge-{{tone}}`（按计数事实在模板里选档：MultiHopCount > 0 → warn、LoopCount > 0 → danger，否则中性；计数为负同样中性）；" +
			"admin/page/page_translations.html:154 `badge-{{engineTone}}`（按 r.Engine 事实选档：== ai → info，其余走中性；空 engine 不渲染徽标而是中性破折号）；" +
			"admin/page/site_slots.html:50/51 `badge-{{unpublishedTone}}`/`badge-{{deletedTone}}`（页头计数档）、" +
			":155 `badge-{{tone}}`（行状态档：published→ok、unpublished→warn，原先 Go 产的是 badge-success/badge-warning 字面类名，" +
			"ui.css:850/851 与 :834/:840 声明逐字节相同 ⇒ 视觉等价）；" +
			"admin/product/product_detail.html:66 `badge-{{a.VariationTone}}`；" +
			"admin/product/product_pricing.html:136 `badge-{{l.StatusTone}}`；" +
			"admin/product/product_tags.html:145 `badge-{{t.KindTone}}`；" +
			"admin/product/product_translations.html:149 `badge-{{r.EngineTone}}`；" +
			"admin/user/customer_badges.html:22 `badge-{{tone}}`（状态档）、:24 `badge-{{tone}}`（邮箱验证档）、" +
			"admin/user/customer_badges.html:30 `badge-{{tone}}`（筛选口径与 RFM 分段徽章，三页共用）",
		values: []string{"ok", "warn", "danger", "mute", "info"},
		note: "取值域 = 各产出点返回值的并集（语义档，见 AGENTS.md §语义 vs 外观）：" +
			"Go 侧 —— mcpSwitchTone{ok,mute}、providerStatusTone{ok,warn}、noticeTone{ok,warn}、" +
			"recipientStatusTone{warn,mute}、i18n.ContentEngineTone{info,mute}（product 的译文来源档，internal/module/product/inbound/http/product_translations.go 产出）、pricingLineTone（internal/module/product/service/product_pricing_page.go）/tagKindTone/variationTone{info,mute}、" +
			"orderenums.OrderStatusTone{warn,info,ok,danger,mute}、ReturnStatusTone{warn,ok,mute}、" +
			"CouponStateTone{ok,danger,warn,mute}、" +
			"outstandingTone{warn,ok}、" +
			"sessionEventTone{info,ok,warn}；" +
			"模板侧（按事实在模板里选档）—— admin/user/customer_badges.html 一份片段供三页共用：" +
			"customerStatusBadge 的 tone{mute,ok,danger,warn}（按状态值取）、customerVerifyBadge 的 tone{warn,ok}（按邮箱验证事实取）、" +
			"customerFilterTab / customerRfmSegmentTab 经 customerTabAnchor 的 tone{mute,ok,danger,warn,info}（按口径键 / 分段键取）；" +
			"admin/page/site_slots.html 的页头 unpublishedTone{mute,warn} / deletedTone{mute,danger}（按计数 `!= 0` 取）" +
			"与行内 tone{mute,danger,ok,warn}（按 Bound/PageDeleted/Published 事实取）；" +
			"admin/page/page_redirects.html 的 tone{mute,warn}（多跳，按 MultiHopCount > 0 取）与 tone{mute,danger}（成环，按 LoopCount > 0 取）；" +
			"admin/page/page_translations.html 的 engineTone{mute,info}（按 r.Engine == ai 取）。" +
			"两侧并集仍是 {ok,warn,danger,mute,info}：模板侧只能在同一个词表内取档，由 TestDynamicClassToneValuesInVocabulary 钉住" +
			"（自造档在 ui.css 没有 `.badge-<档>` 别名，页面不报错、只是静默失色）。" +
			"注：ui.css 另有 `.badge-success`(:834)/`.badge-warning`(:840) 两个既有类名，与 `.badge-ok`(:850)/`.badge-warn`(:851) " +
			"声明逐字节相同；迁移到模板时统一取词表内的 ok/warn，站点证据见本条目 site。" +
			"颜色映射在 ui.css 的 `.badge-<tone>` 别名（.badge-ok/.badge-warn 是新增别名，其余沿用既有类名）",
	},
}

func TestDynamicClassEvidenceIsCurrent(t *testing.T) {
	facts := collectCSSAuditFacts(t)
	for prefix, ev := range dynamicClassEvidence {
		if len(facts.dynPrefixes[prefix]) == 0 {
			t.Errorf("dynamicClassEvidence 里的前缀 %q 在本次扫描里找不到任何拼接点（登记的证据：%s）：证据已失效，请更新或删除该条", prefix, ev.site)
		}
	}
	for prefix := range facts.dynPrefixes {
		if _, ok := dynamicClassEvidence[prefix]; !ok {
			t.Errorf("扫描到动态前缀 %q（来源 %v）却没有证据条目：请先写明拼接点与取值域，再决定放行范围",
				prefix, facts.dynPrefixes[prefix])
		}
	}
}

var reToneBadgeVar = regexp.MustCompile(`badge-\{\{([a-z][A-Za-z0-9_]*)\}\}`)

var reToneAssign = regexp.MustCompile(`\{\{[^}]*\b([a-z][A-Za-z0-9_]*)\s*:?=\s*"([^"]*)"`)

func TestDynamicClassToneValuesInVocabulary(t *testing.T) {
	vocab := map[string]bool{}
	for _, v := range dynamicClassEvidence["badge-"].values {
		vocab[v] = true
	}
	if len(vocab) == 0 {
		t.Fatal("badge- 的取值域是空的：本判据没有词表可比")
	}

	checked := 0
	for _, p := range walkFiles(t, ".", func(p string) bool {
		return strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".jet")
	}) {
		r := rel(t, p)
		lines := strings.Split(readFile(t, p), "\n")

		assigned := map[string]map[string]int{}
		for i, line := range lines {
			for _, m := range reToneAssign.FindAllStringSubmatch(line, -1) {
				if assigned[m[1]] == nil {
					assigned[m[1]] = map[string]int{}
				}
				if _, seen := assigned[m[1]][m[2]]; !seen {
					assigned[m[1]][m[2]] = i + 1
				}
			}
		}

		for i, line := range lines {
			for _, m := range reToneBadgeVar.FindAllStringSubmatch(line, -1) {
				got, ok := assigned[m[1]]
				if !ok {
					continue
				}
				for value, at := range got {
					checked++
					if !vocab[value] {
						t.Errorf("%s:%d 用 `badge-{{%s}}` 取档，但该变量在 %s:%d 被赋成词表外的 %q："+
							"ui.css 里没有 `.badge-%s` 别名，这一档会静默失色。词表与分工见 AGENTS.md",
							r, i+1, m[1], r, at, value, value)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Errorf("没扫到任何「模板局部 tone 变量」的取值（checked=0）：该形态要么消失了、要么写法变了，" +
			"本判据正在静默失效 —— 请更新扫描正则，而不是把这条保护删掉")
	}
}

func sortedPrefixKeys(prefixes map[string][]string) []string {
	out := make([]string, 0, len(prefixes))
	for p := range prefixes {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func head(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[:n]
}

func collectCSSAuditFacts(t *testing.T) cssAuditFacts {
	t.Helper()
	f := cssAuditFacts{
		defined:     map[string][]string{},
		definedOwn:  map[string][]string{},
		usedStrict:  map[string][]string{},
		looseTokens: map[string]bool{},
		goClasses:   map[string][]string{},
		dynPrefixes: map[string][]string{},
	}
	addDefined := func(file, src string, own bool) {
		for _, cls := range auditCSSClasses(src) {
			if !contains(f.defined[cls], file) {
				f.defined[cls] = append(f.defined[cls], file)
			}
			if own && !contains(f.definedOwn[cls], file) {
				f.definedOwn[cls] = append(f.definedOwn[cls], file)
			}
		}
	}

	for _, p := range mustGlob(t, "static/css/*.css") {
		addDefined(rel(t, p), readFile(t, p), true)
	}

	filepath.WalkDir("static/vendor", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".css") {
			return nil
		}
		addDefined(rel(t, p), readFile(t, p), false)
		return nil
	})

	tplFiles := walkFiles(t, ".", func(p string) bool {
		return strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".jet")
	})
	jsFiles := walkFiles(t, "static/js", func(p string) bool {
		return strings.HasSuffix(p, ".js") && !strings.HasSuffix(p, ".min.js") && !strings.HasSuffix(p, ".test.cjs")
	})

	for _, p := range append(append([]string{}, tplFiles...), jsFiles...) {
		src := readFile(t, p)
		for _, tok := range reAuditLooseToken.FindAllString(src, -1) {
			f.looseTokens[tok] = true
		}
	}

	for _, p := range tplFiles {
		src := readFile(t, p)
		for _, m := range reAuditInlineCSS.FindAllStringSubmatch(src, -1) {
			addDefined(rel(t, p)+" (内联 <style>)", reAuditJetExpr.ReplaceAllString(m[1], " "), true)
		}
	}

	for _, p := range walkFiles(t, filepath.Join("..", "..", "internal"), func(p string) bool {
		slashed := filepath.ToSlash(p)
		return strings.HasSuffix(slashed, ".go") && !strings.HasSuffix(slashed, "_test.go") &&
			!strings.HasPrefix(slashed, "../../internal/templates/")
	}) {
		for _, cls := range auditGoClassLiterals(readFile(t, p)) {
			f.looseTokens[cls] = true
			if site := rel(t, p); !contains(f.goClasses[cls], site) {
				f.goClasses[cls] = append(f.goClasses[cls], site)
			}
		}
	}

	for _, p := range tplFiles {
		r := rel(t, p)
		if !strings.HasPrefix(r, "admin/") && !strings.HasPrefix(r, "workbench/") {
			continue
		}
		lines := strings.Split(readFile(t, p), "\n")
		for i, line := range lines {
			for _, m := range reAuditClassAttr.FindAllStringSubmatch(line, -1) {
				auditClassValue(m[1], false, r, i+1, line, f)
			}
		}
	}
	for _, p := range jsFiles {
		r := rel(t, p)
		lines := strings.Split(readFile(t, p), "\n")
		for i, line := range lines {
			for _, m := range reAuditClassAttr.FindAllStringSubmatch(line, -1) {
				auditClassValue(m[1], true, r, i+1, line, f)
			}
			auditJSCalls(line, r, i+1, f)
			for _, m := range reAuditClassName.FindAllStringSubmatch(line, -1) {
				auditJSLiterals(m[1], r, i+1, line, f)
			}
		}
	}
	for cls := range f.defined {
		sort.Strings(f.defined[cls])
	}
	for cls := range f.definedOwn {
		sort.Strings(f.definedOwn[cls])
	}
	return f
}

var (
	reGoClassRaw = regexp.MustCompile(`class="([^"]*)"`)
	reGoClassEsc = regexp.MustCompile(`class=\\"([^\\"]*)\\"`)
)

func auditGoClassLiterals(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{reGoClassRaw, reGoClassEsc} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			val := m[1]
			if strings.ContainsAny(val, "+$") || strings.Contains(val, "`") ||
				strings.Contains(val, "{{") || strings.Contains(val, "%") {
				continue
			}
			for _, tok := range strings.Fields(val) {
				if reAuditStaticTok.MatchString(tok) && !seen[tok] {
					seen[tok] = true
					out = append(out, tok)
				}
			}
		}
	}
	return out
}

func auditClassValue(val string, isJS bool, file string, line int, raw string, f cssAuditFacts) {
	if isJS && reAuditJSConcat.MatchString(val) {
		if m := reAuditStaticPrefix(val); m != "" {
			auditAddPrefix(f, m, file, line, raw)
		}
		return
	}
	if isJS {
		val = stripJSCodeNoise(val)
	}
	val = strings.ReplaceAll(reAuditJetExpr.ReplaceAllString(val, "\x00"), "\t", " ")
	for _, tok := range strings.Fields(val) {
		if strings.Contains(tok, "\x00") {
			if m := reAuditStaticPrefix(strings.SplitN(tok, "\x00", 2)[0]); m != "" {
				auditAddPrefix(f, m, file, line, raw)
			}
			continue
		}
		if reAuditStaticTok.MatchString(tok) {
			auditAddUse(f, tok, file, line, raw)
		}
	}
}

func auditJSCalls(line, file string, lineNo int, f cssAuditFacts) {
	for _, kw := range []string{"classList.add(", "classList.remove(", "classList.toggle("} {
		idx := 0
		for {
			i := strings.Index(line[idx:], kw)
			if i < 0 {
				break
			}
			open := idx + i + len(kw) - 1
			auditJSLiterals(balancedArg(line[open:]), file, lineNo, line, f)
			idx = open + 1
		}
	}
}

func balancedArg(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return s[1:]
}

func auditJSLiterals(expr string, file string, line int, raw string, f cssAuditFacts) {
	expr = stripJSCodeNoise(expr)
	if reAuditJSConcat.MatchString(expr) {

		for _, m := range reAuditJSLiteral.FindAllStringSubmatch(expr, -1) {
			lit := m[1]
			if m[2] != "" {
				lit = m[2]
			}
			for _, tok := range strings.Fields(lit) {
				if reAuditDynPrefix.MatchString(tok) {
					auditAddPrefix(f, tok, file, line, raw)
				} else if reAuditStaticTok.MatchString(tok) && strings.HasPrefix(tok, "is-") {
					auditAddUse(f, tok, file, line, raw)
				}
			}
		}
		return
	}
	for _, m := range reAuditJSLiteral.FindAllStringSubmatch(expr, -1) {
		lit := m[1]
		if m[2] != "" {
			lit = m[2]
		}
		for _, tok := range strings.Fields(lit) {
			if reAuditStaticTok.MatchString(tok) {
				auditAddUse(f, tok, file, line, raw)
			}
		}
	}
}

func stripJSCodeNoise(s string) string {
	s = reAuditJSGetAttr.ReplaceAllString(s, " ")
	s = reAuditJSCompare.ReplaceAllString(s, " ")
	return reAuditJSOrDeflt.ReplaceAllString(s, " ")
}

func reAuditStaticPrefix(val string) string {
	head := val
	if i := strings.Index(val, "+"); i >= 0 {
		head = val[:i]
	}
	tokens := strings.Fields(head)
	if len(tokens) == 0 {
		return ""
	}
	last := tokens[len(tokens)-1]
	if i := strings.LastIndexAny(last, "'\""); i >= 0 {
		if i+1 < len(last) {
			last = last[i+1:]
		} else if j := strings.IndexAny(last, "'\""); j > 0 {
			last = last[:j]
		}
	}
	if last == "" {
		return ""
	}
	if strings.HasSuffix(last, "-") {
		if reAuditDynPrefix.MatchString(last) {
			return last
		}
		return ""
	}
	if len(tokens) == 1 && reAuditStaticTok.MatchString(last) && strings.Contains(val, "+") {
		return last + "-"
	}
	return ""
}

func auditAddUse(f cssAuditFacts, cls, file string, line int, raw string) {
	site := file + ":" + strconv.Itoa(line) + ": " + strings.TrimSpace(raw)
	if !contains(f.usedStrict[cls], site) && len(f.usedStrict[cls]) < 8 {
		f.usedStrict[cls] = append(f.usedStrict[cls], site)
	}
}

func auditAddPrefix(f cssAuditFacts, prefix, file string, line int, raw string) {
	site := file + ":" + strconv.Itoa(line) + ": " + strings.TrimSpace(raw)
	if !contains(f.dynPrefixes[prefix], site) {
		f.dynPrefixes[prefix] = append(f.dynPrefixes[prefix], site)
	}
}

func auditCSSClasses(src string) []string {
	s := stripCSSComments(src)
	s = reAuditCSSURL.ReplaceAllString(s, " ")
	s = reAuditCSSDQuote.ReplaceAllString(s, " ")
	s = reAuditCSSSQuote.ReplaceAllString(s, " ")
	seen := map[string]bool{}
	var out []string
	for _, m := range reAuditCSSClass.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s 失败: %v", pattern, err)
	}
	if len(paths) == 0 {
		t.Fatalf("glob %s 没有任何文件：扫描范围写错了会让本门禁静默空转", pattern)
	}
	return paths
}

func walkFiles(t *testing.T, root string, keep func(string) bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "static/vendor" || p == "static/js/vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if keep(p) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

func rel(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.Rel(".", path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

var cssUndefinedAllowed = map[string]string{
	"wb-confirm-ok": "功能钩子（**不能只看类名有没有样式**）：static/js/ui/confirm.js:45 用 `dlg.querySelector('.wb-confirm-ok')` 查询它来接管确认按钮的提交；同元素另有 btn / btn-primary 基座类给外观，但真正让确认框能用的是这个查询 —— 属「无样式但有人在查」的类，**勿删**",
}

var cssUnusedAllowed = map[string]string{
	"sky-image-missing":   "站点产物侧：workbench.css:536 注释「媒体缺失占位（媒体库已删除时渲染，不阻塞编译）」—— 消费者是构建期产出的占位片段，本仓模板/JS 静态扫不到是正常的（同族实例见 internal/builder/components/socialbuttons/socialbuttons.jet 渲染的 .sky-social-fallback），**不是死样式，勿删**",
	"sky-social-fallback": "站点产物侧：由构建期组件 internal/builder/components/socialbuttons/socialbuttons.jet:1 渲染（`<span class=\"sky-social-fallback\">`），消费者在前台产物里，本仓模板/JS 零命中是正常的，**不是死样式，勿删**",
	"sky-video-missing":   "站点产物侧：workbench.css:536 注释「媒体缺失占位（媒体库已删除时渲染，不阻塞编译）」—— 消费者是构建期产出的占位片段，本仓模板/JS 静态扫不到是正常的，**不是死样式，勿删**",
	"trix-active":         "运行时触发：Trix（vendor static/vendor/trix/trix.umd.js）在工具栏按钮激活时自行加该类；rich-editor.css:165/173 的 `trix-toolbar .trix-button.trix-active` 与 vendor 行为配对，本仓静态零命中是正常的，**不是死样式，勿删**",
}
