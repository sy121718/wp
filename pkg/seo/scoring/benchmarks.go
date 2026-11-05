package scoring

type QueryIntent string

const (
	IntentInformational QueryIntent = "informational"
	IntentCommercial    QueryIntent = "commercial"
	IntentTransactional QueryIntent = "transactional"
	IntentLocal         QueryIntent = "local"
	IntentDefinition    QueryIntent = "definition"
)

type lengthBench struct {
	Full    int
	Partial int
}

var contentLengthBenchmarks = map[QueryIntent]lengthBench{
	IntentInformational: {Full: 1500, Partial: 500},
	IntentCommercial:    {Full: 1200, Partial: 400},
	IntentTransactional: {Full: 500, Partial: 200},
	IntentLocal:         {Full: 400, Partial: 150},
	IntentDefinition:    {Full: 800, Partial: 300},
}

const (
	densityFullMin    = 0.5
	densityFullMax    = 2.0
	densityWarnMax    = 3.0
	densityMinAllowed = 0.5
)

const (
	titleWidthIdealMin = 40
	titleWidthIdealMax = 60
	titleWidthHardMax  = 65
	metaLenIdealMin    = 150
	metaLenIdealMax    = 160
)

type internalLinkBench struct {
	MinWords int
	Min      int
	IdealMin int
	IdealMax int
	TooMany  int
}

var internalLinkBenchmarks = []internalLinkBench{
	{MinWords: 0, Min: 2, IdealMin: 2, IdealMax: 4, TooMany: 8},
	{MinWords: 500, Min: 3, IdealMin: 3, IdealMax: 6, TooMany: 12},
	{MinWords: 1000, Min: 4, IdealMin: 5, IdealMax: 10, TooMany: 20},
	{MinWords: 2000, Min: 5, IdealMin: 8, IdealMax: 15, TooMany: 25},
}

var imageWeightKB = map[string]int{
	"hero":       200,
	"content":    150,
	"screenshot": 100,
	"icon":       30,
	"thumb":      50,
}

const (
	paragraphWarnWords = 150
	sentenceWarnWords  = 20
	sentenceWarnCJK    = 40
	paragraphWarnCJK   = 200
)

var gradeBands = []struct {
	Min   int
	Grade string
}{
	{90, "A+"}, {80, "A"}, {70, "B"}, {60, "C"}, {50, "D"}, {0, "F"},
}

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
