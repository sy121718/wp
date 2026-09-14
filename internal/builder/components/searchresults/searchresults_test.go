package searchresults

import (
	"strings"
	"testing"
)

func TestBuildViewIncludesLangAndProject(t *testing.T) {
	v := BuildView(&Props{Limit: 5}, "proj-1", "en-US")
	if v.Notice != "" {
		t.Fatalf("unexpected notice: %q", v.Notice)
	}
	for _, want := range []string{"projectId=proj-1", "lang=en-US", "limit=5", "/_fragments/searchResults?"} {
		if !strings.Contains(v.BaseFragmentURL, want) {
			t.Fatalf("BaseFragmentURL %q missing %q", v.BaseFragmentURL, want)
		}
	}
}

func TestBuildViewOmitsEmptyLang(t *testing.T) {
	v := BuildView(&Props{}, "proj-1", "")
	if strings.Contains(v.BaseFragmentURL, "lang=") {
		t.Fatalf("empty lang should not appear in URL: %q", v.BaseFragmentURL)
	}
}

func TestBuildViewMissingProjectNotice(t *testing.T) {
	v := BuildView(&Props{}, "", "zh-CN")
	if v.Notice == "" {
		t.Fatal("missing project should set notice")
	}
}
