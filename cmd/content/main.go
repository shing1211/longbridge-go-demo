// Command content reads the Longbridge research/community surface: discussion
// topics and news for a symbol, the full record of one topic, the replies on a
// topic, and your own topics.
//
// It is entirely READ-ONLY.
//
// # WHAT IS DELIBERATELY NOT HERE
//
// ContentContext has seven exported methods. The five read methods (Topics,
// News, TopicDetail, MyTopics, ListTopicReplies) are implemented here.
// CreateTopic and CreateTopicReply both publish content under your account —
// they are writes, not reads, and would need their own gate. They are named in
// the note this command prints at startup so the omission is visible.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/content"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action     string
	symbol     string
	topicID    string
	replyTopic string
	topicType  string
	page       int
	size       int
	limit      int
	timeout    time.Duration
)

func main() {
	u := cli.NewUsage("content", "read-only research content: topics, news, topic detail, replies, my topics")
	u.FS.StringVar(&action, "action", "topics",
		"topics (-symbol) | news (-symbol) | detail (-topic-id) | replies (-reply-topic) | mine")
	u.FS.StringVar(&symbol, "symbol", "700.HK", "security symbol for -action topics and news")
	u.FS.StringVar(&topicID, "topic-id", "", "topic id, required for -action detail")
	u.FS.StringVar(&replyTopic, "reply-topic", "", "topic id, required for -action replies")
	u.FS.StringVar(&topicType, "topic-type", "", "filter for -action mine: article, post (empty = all)")
	u.FS.IntVar(&page, "page", 1, "1-based page for -action replies and mine")
	u.FS.IntVar(&size, "size", 20, "page size for -action replies (1-50) and mine (1-500)")
	u.FS.IntVar(&limit, "limit", 20, "how many rows to print for topics and news")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only; an open order gate here would mean the
	// environment is wrong, not that we should start writing.
	if err := cfg.GuardWrite("run the content reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: content is read-only but the order gate is open"))
	}
	fmt.Fprintf(os.Stderr,
		"[content] read-only: Topics, News, TopicDetail, MyTopics, ListTopicReplies only. "+
			"Not implemented (they publish under your account and would need a third write gate): "+
			"CreateTopic, CreateTopicReply.\n")

	cli.Run(func(ctx context.Context) error {
		// The content package has no Close(); it is a thin HTTP client.
		cc, err := content.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating content context: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(action)) {
		case "topics":
			return printTopics(ctx, cc)
		case "news":
			return printNews(ctx, cc)
		case "detail":
			if strings.TrimSpace(topicID) == "" {
				return fmt.Errorf("-topic-id is required for -action detail")
			}
			return printDetail(ctx, cc)
		case "replies":
			if strings.TrimSpace(replyTopic) == "" {
				return fmt.Errorf("-reply-topic is required for -action replies")
			}
			return printReplies(ctx, cc)
		case "mine", "my-topics":
			return printMine(ctx, cc)
		default:
			return fmt.Errorf("unknown -action %q: want topics, news, detail, replies or mine", action)
		}
	})
}

func printTopics(ctx context.Context, cc *content.ContentContext) error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("-symbol is required for -action topics")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Topics %s", symbol))
	items, err := cc.Topics(c, symbol)
	if err != nil {
		return fmt.Errorf("topics for %s: %w", symbol, err)
	}
	if len(items) == 0 {
		fmt.Println("   (no topics returned)")
		return nil
	}
	fmt.Printf("%-22s %-8s %-8s %-8s %-46s %s\n",
		"PUBLISHED", "COMMENTS", "LIKES", "SHARES", "TITLE", "URL")
	for _, it := range limitSlice(items, limit) {
		fmt.Printf("%-22s %-8d %-8d %-8d %-46s %s\n",
			fmtTime(it.PublishedAt), it.CommentsCount, it.LikesCount, it.SharesCount,
			cli.Truncate(it.Title, 46), cli.OrDash(it.Url))
	}
	fmt.Printf("\n(%d of %d topics; use -action detail -topic-id <ID> for the full record)\n",
		shown(limit, len(items)), len(items))
	return nil
}

func printNews(ctx context.Context, cc *content.ContentContext) error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("-symbol is required for -action news")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("News %s", symbol))
	items, err := cc.News(c, symbol)
	if err != nil {
		return fmt.Errorf("news for %s: %w", symbol, err)
	}
	if len(items) == 0 {
		fmt.Println("   (no news returned)")
		return nil
	}
	fmt.Printf("%-22s %-8s %-8s %-8s %-46s %s\n",
		"PUBLISHED", "COMMENTS", "LIKES", "SHARES", "TITLE", "URL")
	for _, it := range limitSlice(items, limit) {
		fmt.Printf("%-22s %-8d %-8d %-8d %-46s %s\n",
			fmtTime(it.PublishedAt), it.CommentsCount, it.LikesCount, it.SharesCount,
			cli.Truncate(it.Title, 46), cli.OrDash(it.Url))
	}
	fmt.Printf("\n(%d of %d news items)\n", shown(limit, len(items)), len(items))
	return nil
}

func printDetail(ctx context.Context, cc *content.ContentContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Topic %s", topicID))
	t, err := cc.TopicDetail(c, topicID)
	if err != nil {
		return fmt.Errorf("topic detail %s: %w", topicID, err)
	}
	if t == nil {
		fmt.Println("   (no topic returned)")
		return nil
	}
	fmt.Printf("title        %s\n", cli.OrDash(t.Title))
	fmt.Printf("type         %s\n", cli.OrDash(t.TopicType))
	fmt.Printf("author       %s (%s)\n", cli.OrDash(t.Author.Name), cli.OrDash(t.Author.MemberID))
	fmt.Printf("created      %s\n", fmtTime(t.CreatedAt))
	fmt.Printf("updated      %s\n", fmtTime(t.UpdatedAt))
	fmt.Printf("engagement   likes %d  comments %d  views %d  shares %d\n",
		t.LikesCount, t.CommentsCount, t.ViewsCount, t.SharesCount)
	if len(t.Tickers) > 0 {
		fmt.Printf("tickers      %s\n", strings.Join(t.Tickers, ", "))
	}
	if len(t.Hashtags) > 0 {
		fmt.Printf("hashtags     %s\n", strings.Join(t.Hashtags, ", "))
	}
	if t.DetailURL != "" {
		fmt.Printf("url          %s\n", t.DetailURL)
	}
	if len(t.Images) > 0 {
		fmt.Printf("images       %d\n", len(t.Images))
	}
	if strings.TrimSpace(t.Description) != "" {
		fmt.Printf("\n-- description --\n%s\n", t.Description)
	}
	if strings.TrimSpace(t.Body) != "" {
		fmt.Printf("\n-- body --\n%s\n", t.Body)
	}
	return nil
}

func printReplies(ctx context.Context, cc *content.ContentContext) error {
	if page < 1 {
		return fmt.Errorf("-page must be at least 1, got %d", page)
	}
	if size < 1 || size > 50 {
		// The SDK documents 1-50 for this option; check it here rather than
		// letting the API reject it.
		return fmt.Errorf("-size must be between 1 and 50 for -action replies, got %d", size)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Replies on topic %s (page %d, size %d)", replyTopic, page, size))
	opts := &content.ListTopicRepliesOptions{Page: page, Size: size}
	replies, err := cc.ListTopicReplies(c, replyTopic, opts)
	if err != nil {
		return fmt.Errorf("replies on topic %s: %w", replyTopic, err)
	}
	if len(replies) == 0 {
		fmt.Println("   (no replies on this page)")
		return nil
	}
	fmt.Printf("%-22s %-24s %-8s %-8s %-18s %s\n",
		"CREATED", "AUTHOR", "LIKES", "COMMENTS", "REPLY_TO", "BODY")
	for _, r := range limitSlice(replies, limit) {
		fmt.Printf("%-22s %-24s %-8d %-8d %-18s %s\n",
			fmtTime(r.CreatedAt), cli.Truncate(r.Author.Name, 24), r.LikesCount,
			r.CommentsCount, cli.Truncate(r.ReplyToID, 18), cli.Truncate(oneLine(r.Body), 60))
	}
	fmt.Printf("\n(%d of %d replies; page through with -page)\n", shown(limit, len(replies)), len(replies))
	return nil
}

func printMine(ctx context.Context, cc *content.ContentContext) error {
	if page < 1 {
		return fmt.Errorf("-page must be at least 1, got %d", page)
	}
	if size < 1 || size > 500 {
		return fmt.Errorf("-size must be between 1 and 500 for -action mine, got %d", size)
	}
	tt := strings.ToLower(strings.TrimSpace(topicType))
	if tt != "" && tt != "article" && tt != "post" {
		return fmt.Errorf("unknown -topic-type %q: want article, post or empty", topicType)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("My topics (page %d, size %d, type %q)", page, size, tt))
	opts := &content.MyTopicsOptions{Page: page, Size: size, TopicType: tt}
	topics, err := cc.MyTopics(c, opts)
	if err != nil {
		return fmt.Errorf("my topics: %w", err)
	}
	if len(topics) == 0 {
		fmt.Println("   (no topics of yours on this page)")
		return nil
	}
	fmt.Printf("%-22s %-10s %-8s %-8s %-10s %-8s %s\n",
		"CREATED", "TYPE", "LIKES", "COMMENTS", "VIEWS", "SHARES", "TITLE")
	for _, t := range limitSlice(topics, limit) {
		fmt.Printf("%-22s %-10s %-8d %-8d %-10d %-8d %s\n",
			fmtTime(t.CreatedAt), cli.OrDash(t.TopicType), t.LikesCount,
			t.CommentsCount, t.ViewsCount, t.SharesCount, cli.Truncate(t.Title, 60))
	}
	fmt.Printf("\n(%d of %d topics; page through with -page)\n", shown(limit, len(topics)), len(topics))
	return nil
}

// ------------------------------------------------------------------ helpers

// limitSlice caps a printout at n elements, where n <= 0 means "all".
func limitSlice[T any](items []T, n int) []T {
	if n > 0 && len(items) > n {
		return items[:n]
	}
	return items
}

func shown(limitN, total int) int {
	if limitN > 0 && limitN < total {
		return limitN
	}
	return total
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}
