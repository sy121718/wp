package publishrecord

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	presentationcontract "go_wp/internal/module/presentation/contract"
	json "go_wp/pkg/jsonx"
)

type attemptPortStub struct {
	row presentationcontract.PublishAttemptRow
}

func (s *attemptPortStub) PublishAttempts(context.Context, string, string, string, int) (presentationcontract.PublishAttemptPage, error) {
	return presentationcontract.PublishAttemptPage{Rows: []presentationcontract.PublishAttemptRow{s.row}, Total: 1}, nil
}

func TestPublishAttemptAdapterReturnsFactsWithoutInternalError(t *testing.T) {
	created := time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)
	port := NewWithAttempts(&attemptPortStub{row: presentationcontract.PublishAttemptRow{
		ID: 7, CreatedAt: created, Outcome: "failed", ErrorText: "private SQL driver credentials",
		Langs: []string{"zh-CN", "en-US"}, DurationMs: 1234, Attempts: 2,
	}})
	result, err := port.PublishAttempts(context.Background(), "project-1", "product-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || !result.Rows[0].CreatedAt.TimeOrZero().Equal(created) || result.Rows[0].DurationMs != 1234 || result.Rows[0].Attempts != 2 || len(result.Rows[0].Langs) != 2 {
		t.Fatalf("原始发布事实应保持不变：%+v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private", "SQL", "credentials", "errorText", "createdAtText", "attemptsText", "durationText", "langsText"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("JSON 不得携带内部异常或展示副本 %q：%s", forbidden, raw)
		}
	}
	if !strings.Contains(string(raw), `"createdAt":`+strconv.FormatInt(created.UnixMilli(), 10)) {
		t.Fatalf("时刻应使用 unix 毫秒：%s", raw)
	}
}
