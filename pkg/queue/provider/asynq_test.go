package queueprovider

import (
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

func TestParseConfigUsesCustomQueueWeights(t *testing.T) {
	cfg := viper.New()
	cfg.Set("queue.concurrency", 5)
	cfg.Set("queue.queues", map[string]int{
		"critical": 10,
		"default":  2,
		"low":      1,
	})

	parsed := parseConfig(cfg)

	if parsed.Concurrency != 5 {
		t.Fatalf("Concurrency 不正确: got=%d want=%d", parsed.Concurrency, 5)
	}

	want := map[string]int{
		"critical": 10,
		"default":  2,
		"low":      1,
	}
	if !reflect.DeepEqual(parsed.Queues, want) {
		t.Fatalf("队列权重不正确: got=%v want=%v", parsed.Queues, want)
	}
}
