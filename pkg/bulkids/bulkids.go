package bulkids

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const MaxIDs = 200

var ErrTooMany = errors.New("bulkids: bulk ids exceeded")

type IDsError struct {
	Count int
	Max   int
}

const TooManyTemplate = "一次最多操作 %s 项，当前 %s 项，请分批进行"

func (e *IDsError) Error() string {
	key, _ := e.Facing()
	return key
}

func (e *IDsError) Unwrap() error { return ErrTooMany }

// Facing 只交词条与数量参数，文案由 JSON 出口按请求语言生成。
func (e *IDsError) Facing() (string, string) {
	return fmt.Sprintf("%s|%d|%d", MsgTooMany, e.Max, e.Count), ""
}

func (e *IDsError) HTTPStatus() int { return http.StatusBadRequest }

// MsgTooMany 是批量上限的共享词条 key（迁移 720 已把旧点分 key 改名为短 key）。
const MsgTooMany = "bulkIdsTooMany"

func Normalize(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) > MaxIDs {
		return nil, &IDsError{Count: len(out), Max: MaxIDs}
	}
	return out, nil
}
