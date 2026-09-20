package pipeline

// publication_plan.go — 发布计划（PublicationPlan）：把「这次发布依据哪几种语言、
// 默认语言是谁」从可编辑配置里**冻结**出来（审计 I18N-01）。
//
// 缺陷现象（审计 I18N-01）：手工 Page 的 hreflang 互指集合是**构建期**从站点语言清单
// （project_locales）推导出来的，而清单是可编辑配置。产物一旦建成，任何一次重放 / 重建
// 都会用**当时的**清单重算一遍：
//
//   - 组件升级后的批量重建（page.RebuildStale 逐语言重编译）；
//   - 灾难恢复的按元数据重建（page.RebuildArtifact）；
//   - 发布前的确定性复构建（Publish 的 built != staged 校验）；
//   - 误删产物文件后的恢复重建。
//
// 于是**同一份冻结源文档**在配置改动之后会产出另一份互指链接：既有产物的 hreflang
// 凭空变了（少一条、多一条、x-default 换人），而线上路径一个都没动 —— 没有任何
// 报错，只有搜索引擎看到的两份互相矛盾的站点声明。
//
// 计划的职责就是把这份输入固化成**发布事实**：构建/发布时冻结一次并落库
// （page_publication_plans，迁移 308），此后所有重建入口一律以冻结值为准，
// 不再回读 project_locales。改配置只在**下一次发布决策**（页面草稿被改动后的
// 重新构建/发布）里生效 —— 那是新产物，不是既有产物。
//
// 与 Manifest 的关系：Manifest.SiteLangs / Manifest.SiteDefaultLang 记的是
// 「**这份产物**的冻结输入」（事后审计用，随产物走）；本计划记的是
// 「这个页面+语言**当前应当依据的**冻结输入」（重建入口用，随页面走）。
// 两者由同一次编译器装配同时写出，不可能各说各话。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// PublicationPlan 一次发布冻结的站点语言输入。
type PublicationPlan struct {
	// SiteLangs 站点启用语言（默认语言在前）。
	//
	// 顺序即语义：DefaultLocale 的调用方与 LocaleView 都依赖「默认语言在前」，
	// 因此 Normalize 只去重、不排序 —— 排序会把默认语言挪到中间，
	// 而 hreflang 的输出顺序会跟着变（产物字节随之变化）。
	SiteLangs []string `json:"siteLangs"`
	// DefaultLang 站点默认语言（完整语言码）。
	//
	// 单独冻结而不是从 SiteLangs 推：默认语言同时决定 x-default 指向哪一条互指、
	// 以及「默认语言无前缀」规则下哪一条不带前缀。is_default 是清单里的一个可改
	// 布尔位，改动它会让既有产物的 x-default 换目标 —— 与语言集合本身无关。
	DefaultLang string `json:"defaultLang"`
}

// Empty 计划是否为空（两个输入都没有）。
//
// 空计划按「没有冻结」处理，不能用它去覆盖现场解析：它会让 SiteCompileOptions
// 拿到一份空语言表，产物直接失去全部互指。
func (p PublicationPlan) Empty() bool {
	return len(p.normalizedLangs()) == 0 && strings.TrimSpace(p.DefaultLang) == ""
}

// Normalize 规范化：语言码去空白、去重保序、默认语言去空白。
//
// 不排序：SiteLangs 的顺序是**发布事实的一部分**（默认语言在前由上游保证，
// 参与 hreflang 输出顺序）。排序会让「同一条计划」在不同写入路径上产出不同字节。
func (p PublicationPlan) Normalize() PublicationPlan {
	return PublicationPlan{
		SiteLangs:   p.normalizedLangs(),
		DefaultLang: strings.TrimSpace(p.DefaultLang),
	}
}

// normalizedLangs 去空白 + 去重（保序）后的语言表。
func (p PublicationPlan) normalizedLangs() []string {
	if len(p.SiteLangs) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.SiteLangs))
	seen := make(map[string]bool, len(p.SiteLangs))
	for _, raw := range p.SiteLangs {
		lang := strings.TrimSpace(raw)
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		out = append(out, lang)
	}
	return out
}

// Hash 计划的内容指纹（规范化后序列化再哈希，稳定可比较）。
//
// 用途是「这条计划到底有没有变」的判定与日志留痕，不参与产物 hash ——
// 产物 hash 走 Manifest 的逐字段序列化。两者分开的理由：计划里任何字段的**语义**
// 变化都必须改变产物字节，而指纹只负责回答「换没换」。
func (p PublicationPlan) Hash() string {
	data, err := json.Marshal(p.Normalize())
	if err != nil {
		// 计划只含 string / []string，Marshal 不会失败；真失败了也不该 panic 掉构建链。
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// String 计划的可读形式（日志 / 错误文案用）。
func (p PublicationPlan) String() string {
	n := p.Normalize()
	if n.Empty() {
		return "(未冻结)"
	}
	return fmt.Sprintf("siteLangs=%s default=%s", strings.Join(n.SiteLangs, ","), n.DefaultLang)
}
