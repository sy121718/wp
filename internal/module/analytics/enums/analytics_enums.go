package analyticsenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrInvalidParam = "ErrInvalidParam"

	ErrInvalidRange = "ErrInvalidRange"

	ErrAnalyticsInternal = "ErrAnalyticsInternal"
)

// StatusOf 声明每个 analytics 业务错误对应的 HTTP 状态码。
//
// 为什么状态码写在这里而不是读词库的 http_code：词库是**文案**的事实源，状态码是
// **协议**的事实源。声明在 Go 里，出口只认这一份。
//
//	400 参数不对，改输入再来
//	500 我们这边出错，用户改什么都没用
//
// 未登记的 key 返回 0，表示「这个 key 没声明状态」，由出口按归口处理。
func StatusOf(key string) int {
	switch key {
	case ErrInvalidParam, ErrInvalidRange:
		return http.StatusBadRequest

	case ErrAnalyticsInternal:
		return http.StatusInternalServerError
	}
	return 0
}

// Err 构造业务错误，并把声明里的状态码一起带上。
//
// 状态码是错误事实的一部分，不写在调用点 —— 之前 handler 用
// response.ErrorWithMessage(c, 400, err.Error()) 把状态码钉在分支上，还把裸 key
// 当 message 交给前端。现在状态在声明处给，出口对它只调 ErrorStatusAuto。
func Err(key string) error {
	err := facing.New(key)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(err, status)
	}
	return err
}

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	base := fmt.Errorf("%w: %s", Err(key), detail)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(base, status)
	}
	return base
}

const (
	MsgCollectAccepted = "MsgCollectAccepted"
)
