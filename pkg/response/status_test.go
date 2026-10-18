package response

import (
	"errors"
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

// ErrorStatus 的分档口径：声明处给过就用它，业务错误 400，其余 500。
func TestErrorStatusUsesDeclaredStatus(t *testing.T) {
	business := facing.New("inventory.err.invalidParam", "参数不合法")
	notFound := facing.WithStatus(facing.New("inventory.err.warehouseNotFound", "仓库不存在"), http.StatusNotFound)
	conflict := facing.WithStatus(facing.New("inventory.err.stockInsufficient", "库存不足"), http.StatusConflict)
	internal := errors.New("pq: relation inventory_stocks does not exist")

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"business default", business, http.StatusBadRequest},
		{"declared 404", notFound, http.StatusNotFound},
		{"declared 409", conflict, http.StatusConflict},
		{"wrapped declared", errors.Join(errors.New("outer"), conflict), http.StatusConflict},
		{"internal", internal, http.StatusInternalServerError},
		{"nil", nil, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorStatus(tt.err); got != tt.want {
				t.Fatalf("ErrorStatus = %d，want %d", got, tt.want)
			}
		})
	}
}
