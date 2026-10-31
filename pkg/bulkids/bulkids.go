package bulkids

import (
	"errors"
	"fmt"
	"strconv"
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
	return fmt.Sprintf(TooManyTemplate, strconv.Itoa(e.Max), strconv.Itoa(e.Count))
}

func (e *IDsError) Unwrap() error { return ErrTooMany }

const MsgTooMany = "shell.err.bulkIdsTooMany"

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
