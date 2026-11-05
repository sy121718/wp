package seo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const FeedFileName = "feed.xml"

const DefaultFeedLimit = 20

const (
	feedEnabledEnv = "GO_WP_SEO_FEED"
	feedLimitEnv   = "GO_WP_SEO_FEED_LIMIT"
)

func FeedEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(feedEnabledEnv))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

func FeedLimit() int {
	raw := strings.TrimSpace(os.Getenv(feedLimitEnv))
	if raw == "" {
		return DefaultFeedLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return DefaultFeedLimit
	}
	return n
}

type FeedChannel struct {
	Title       string
	Link        string
	Description string
	Language    string
}

type FeedItem struct {
	Title       string
	Link        string
	Description string
	PubDate     time.Time
}

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title,omitempty"`
	Link        string  `xml:"link"`
	Description string  `xml:"description,omitempty"`
	PubDate     string  `xml:"pubDate,omitempty"`
	GUID        rssGUID `xml:"guid"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

const rfc822 = "Mon, 02 Jan 2006 15:04:05 -0700"

func BuildRSS(ch FeedChannel, items []FeedItem, limit int) (string, error) {
	sorted := normalizeFeedItems(items, limit)
	ch = normalizeFeedChannel(ch)

	out := rssChannel{
		Title:       ch.Title,
		Link:        ch.Link,
		Description: ch.Description,
		Language:    ch.Language,
	}
	for _, it := range sorted {
		node := rssItem{
			Title:       it.Title,
			Link:        it.Link,
			Description: it.Description,
			GUID:        rssGUID{IsPermaLink: "true", Value: it.Link},
		}
		if !it.PubDate.IsZero() {
			node.PubDate = it.PubDate.UTC().Format(rfc822)
		}
		out.Items = append(out.Items, node)
	}

	if max := maxFeedTime(sorted); !max.IsZero() {
		out.LastBuildDate = max.UTC().Format(rfc822)
	}

	body, err := xml.MarshalIndent(rssRoot{Version: "2.0", Channel: out}, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(body) + "\n", nil
}

func normalizeFeedChannel(ch FeedChannel) FeedChannel {
	ch.Title = strings.TrimSpace(ch.Title)
	ch.Link = strings.TrimSpace(ch.Link)
	ch.Description = strings.TrimSpace(ch.Description)
	ch.Language = strings.TrimSpace(ch.Language)
	if ch.Title == "" {
		ch.Title = ch.Link
	}
	if ch.Description == "" {
		ch.Description = ch.Title
	}
	return ch
}

func normalizeFeedItems(items []FeedItem, limit int) []FeedItem {
	out := make([]FeedItem, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		link := strings.TrimSpace(it.Link)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		it.Link = link
		it.Title = strings.TrimSpace(it.Title)
		it.Description = strings.TrimSpace(it.Description)
		out = append(out, it)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && feedItemLess(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func feedItemLess(a, b FeedItem) bool {
	at, bt := a.PubDate.UTC(), b.PubDate.UTC()
	if !at.Equal(bt) {
		if at.IsZero() {
			return false
		}
		if bt.IsZero() {
			return true
		}
		return at.After(bt)
	}
	return a.Link < b.Link
}

func maxFeedTime(items []FeedItem) time.Time {
	var max time.Time
	for _, it := range items {
		if it.PubDate.After(max) {
			max = it.PubDate
		}
	}
	return max
}

func WriteFeed(dir string, ch FeedChannel, items []FeedItem, limit int) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	body, err := BuildRSS(ch, items, limit)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, FeedFileName), []byte(body))
}

func RemoveFeed(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	err := os.Remove(filepath.Join(dir, FeedFileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
