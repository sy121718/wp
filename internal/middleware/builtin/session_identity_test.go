package builtin

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCurrentUserIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		set      bool
		value    any
		wantRaw  int64
		wantID   uint64
		wantText string
	}{
		{name: "missing"},
		{name: "positive", set: true, value: int64(42), wantRaw: 42, wantID: 42, wantText: "42"},
		{name: "zero", set: true, value: int64(0)},
		{name: "negative", set: true, value: int64(-42), wantRaw: -42},
		{name: "maximum", set: true, value: int64(math.MaxInt64), wantRaw: math.MaxInt64, wantID: math.MaxInt64, wantText: "9223372036854775807"},
		{name: "minimum", set: true, value: int64(math.MinInt64), wantRaw: math.MinInt64},
		{name: "int", set: true, value: int(42)},
		{name: "uint64", set: true, value: uint64(42)},
		{name: "string", set: true, value: "42"},
		{name: "nil value", set: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.set {
				c.Set("user_id", tt.value)
			}
			if got := GetUserID(c); got != tt.wantRaw {
				t.Errorf("GetUserID() = %d, want %d", got, tt.wantRaw)
			}
			if got := CurrentUserID(c); got != tt.wantID {
				t.Errorf("CurrentUserID() = %d, want %d", got, tt.wantID)
			}
			if got := CurrentUserIDText(c); got != tt.wantText {
				t.Errorf("CurrentUserIDText() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestCurrentUserIdentityNilContext(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		read func(*gin.Context)
	}{
		{name: "GetUserID", read: func(c *gin.Context) { GetUserID(c) }},
		{name: "CurrentUserID", read: func(c *gin.Context) { CurrentUserID(c) }},
		{name: "CurrentUserIDText", read: func(c *gin.Context) { CurrentUserIDText(c) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("nil context must preserve the existing panic")
				}
			}()
			tt.read(nil)
		})
	}
}
