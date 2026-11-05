package jet

import (
	"errors"
	"strings"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

// FailureDetail 是一条可交模板取词的错误明细。
type FailureDetail struct {
	Key  string
	Args map[string]any
}

// FailureData 是错误事实→模板数据的转换结果。
//
// 它只承载展示所需的事实，不含渲染逻辑：模板选择与响应写出由应用装配。
type FailureData struct {
	Status      int
	Fact        facing.Fact
	Args        []string
	Details     []FailureDetail
	PlainDetail string
	Items       []facing.BlockedItem
}

// BuildFailureData 把错误事实展开成模板可取词的结构。
func BuildFailureData(status int, fact facing.Fact, err error) FailureData {
	data := FailureData{Status: status, Fact: fact}
	if !fact.Business {
		return data
	}
	// 编码串里的位置参数拆给模板取词；编码串本身不是词条键。
	if key, tail, ok := strings.Cut(fact.Key, "|"); ok {
		data.Fact.Key = key
		// fallback 是调用点给的明确消息事实，保留它；清空会让缺词时退成归口文案。
		data.Args = strings.Split(tail, "|")
	}
	parts, _ := i18n.ParseErrorDetails(fact.Tail)
	if len(parts) == 0 && !i18n.EncodedDetailLike(fact.Tail) {
		data.PlainDetail = fact.Tail
	}
	for _, part := range parts {
		args := make(map[string]any, len(part.Args))
		for name, value := range part.Args {
			args[name] = value
		}
		data.Details = append(data.Details, FailureDetail{Key: part.Key, Args: args})
	}
	var blocked facing.BlockedError
	if errors.As(err, &blocked) {
		data.Items = blocked.BlockedItems()
	}
	return data
}
