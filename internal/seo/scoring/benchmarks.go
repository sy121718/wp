package scoring

// benchmarks.go — SEO 评分基准表（docs/02-E1-seo-scoring-rules.md §2 的数据化）。
//
// 规则即数据：调整阈值只改本文件，不改检查逻辑。

// QueryIntent 查询意图（决定内容长度分档）。
type QueryIntent string

const (
	IntentInformational QueryIntent = "informational"
	IntentCommercial    QueryIntent = "commercial"
	IntentTransactional QueryIntent = "transactional"
	IntentLocal         QueryIntent = "local"
	IntentDefinition    QueryIntent = "definition"
)

// lengthBench 内容长度分档：满分 / 部分分 / 差。
type lengthBench struct {
	Full    int
	Partial int
}

// contentLengthBenchmarks 按查询意图的内容长度基准（词/字数）。
var contentLengthBenchmarks = map[QueryIntent]lengthBench{
	IntentInformational: {Full: 1500, Partial: 500},
	IntentCommercial:    {Full: 1200, Partial: 400},
	IntentTransactional: {Full: 500, Partial: 200},
	IntentLocal:         {Full: 400, Partial: 150},
	IntentDefinition:    {Full: 800, Partial: 300},
}

// 关键词密度区间（百分比）。
const (
	densityFullMin    = 0.5
	densityFullMax    = 2.0
	densityWarnMax    = 3.0 // 超过视为 stuffing
	densityMinAllowed = 0.5
)

// 标题 / 元描述长度（字符）。
const (
	titleLenIdealMin = 50
	titleLenIdealMax = 60
	titleLenHardMax  = 65
	metaLenIdealMin  = 150
	metaLenIdealMax  = 160
)

// internalLinkBench 内链基准（按篇幅）。
type internalLinkBench struct {
	MinWords int
	Min      int
	IdealMin int
	IdealMax int
	TooMany  int
}

// internalLinkBenchmarks 内链数量基准表（按篇幅升序匹配）。
var internalLinkBenchmarks = []internalLinkBench{
	{MinWords: 0, Min: 2, IdealMin: 2, IdealMax: 4, TooMany: 8},
	{MinWords: 500, Min: 3, IdealMin: 3, IdealMax: 6, TooMany: 12},
	{MinWords: 1000, Min: 4, IdealMin: 5, IdealMax: 10, TooMany: 20},
	{MinWords: 2000, Min: 5, IdealMin: 8, IdealMax: 15, TooMany: 25},
}

// imageWeightKB 图片体积上限（KB，按类型）。
var imageWeightKB = map[string]int{
	"hero":       200,
	"content":    150,
	"screenshot": 100,
	"icon":       30,
	"thumb":      50,
}

// 段落 / 句子长度告警线。
const (
	paragraphWarnWords = 150
	sentenceWarnWords  = 20
	sentenceWarnCJK    = 40 // 中文按字数
	paragraphWarnCJK   = 200
)

// 评分等级（总分）。
var gradeBands = []struct {
	Min   int
	Grade string
}{
	{90, "A+"}, {80, "A"}, {70, "B"}, {60, "C"}, {50, "D"}, {0, "F"},
}

// 单项色标（侧栏红黄绿圆点）。
func colorOf(percent int) string {
	switch {
	case percent >= 90:
		return "green"
	case percent >= 70:
		return "lightgreen"
	case percent >= 40:
		return "yellow"
	case percent > 0:
		return "red"
	default:
		return "red-blocking"
	}
}
