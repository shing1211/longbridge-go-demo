// Command content reads the Longbridge research/community surface — topics and
// news for a symbol, one topic's full record, the replies on a topic, your own
// topics — and, behind its own safety gate, publishes a topic or a reply under
// your account.
//
// # SAFETY — READ THIS BEFORE THE WRITE ACTIONS
//
// ContentContext has seven methods. Five are reads (Topics, News,
// TopicDetail, MyTopics, ListTopicReplies) and are unguarded by design. The
// other two were verified against the SDK source in
// github.com/longbridge/openapi-go v0.25.2 and both publish content that is
// attributed to the account and visible to other users:
//
//	CreateTopic       POST /v1/content/topics
//	CreateTopicReply  POST /v1/content/topics/{id}/comments
//
// Publishing is not idempotent and there is no delete-topic method in the SDK,
// so a published post or reply cannot be retracted from here. They therefore
// have their own gate, checked inside this package (see contentGate) rather
// than in the shared guard file:
//
//  1. LONGPORT_CONTENT_DRY_RUN=0 (or false), AND
//  2. --confirm-live-content on the command line, AND
//  3. LONGPORT_MODE=live
//
// All three are required and each one alone still refuses. A refusal prints the
// exact body that WOULD be sent, prefixed [DRY-RUN], makes no network call at
// all, and exits 3 — distinct from 0 ("nothing happened, on purpose") and from
// 1/2 (errors).
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/content"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// exitBlocked is the exit code for "the safety gate refused", matching the
// sibling Tiger project's convention and internal/config.ExitBlocked. It is
// duplicated locally so this gate keeps working regardless of what the shared
// config package exports.
const exitBlocked = 3

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

	// write-path flags
	title     string
	body      string
	tickers   string
	hashtags  string
	replyToID string
	confirm   bool
)

func main() {
	u := cli.NewUsage("content",
		"read research content; gated publishing of topics and replies")
	u.FS.StringVar(&action, "action", "topics",
		"topics (-symbol) | news (-symbol) | detail (-topic-id) | replies (-reply-topic) | "+
			"mine | create-topic | reply")
	u.FS.StringVar(&symbol, "symbol", "700.HK", "security symbol for -action topics and news")
	u.FS.StringVar(&topicID, "topic-id", "", "topic id, required for -action detail")
	u.FS.StringVar(&replyTopic, "reply-topic", "", "topic id, required for -action replies and reply")
	u.FS.StringVar(&topicType, "topic-type", "", "filter for -action mine; topic type for -action create-topic")
	u.FS.IntVar(&page, "page", 1, "1-based page for -action replies and mine")
	u.FS.IntVar(&size, "size", 20, "page size for -action replies (1-50) and mine (1-500)")
	u.FS.IntVar(&limit, "limit", 20, "how many rows to print for topics and news")
	u.FS.StringVar(&title, "title", "", "topic title, for -action create-topic (required when -topic-type article)")
	u.FS.StringVar(&body, "body", "", "post body, REQUIRED for -action create-topic and -action reply")
	u.FS.StringVar(&tickers, "tickers", "",
		"comma-separated CODE.MARKET tickers for -action create-topic (max 10)")
	u.FS.StringVar(&hashtags, "hashtags", "",
		"comma-separated hashtags for -action create-topic (max 5)")
	u.FS.StringVar(&replyToID, "reply-to-id", "",
		"for -action reply: id of the reply to nest under (empty = top-level)")
	u.FS.BoolVar(&confirm, "confirm-live-content", false,
		"REQUIRED acknowledgement for publishing; must be combined with "+
			"LONGPORT_CONTENT_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	// Local switch, so the shared loader cannot validate it; do it here.
	if err := validateContentDryRun(); err != nil {
		cli.Fail(err)
	}
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr,
		"[config] content_dry_run=%v (separate gate: LONGPORT_CONTENT_DRY_RUN "+
			"+ --confirm-live-content + LONGPORT_MODE=live)\n", contentDryRun())

	cli.Run(func(ctx context.Context) error {
		// The content package has no Close(); it is a thin HTTP client. The
		// context is still built lazily so a blocked publish never reaches the
		// network.
		var cc *content.ContentContext
		connect := func() (*content.ContentContext, error) {
			if cc != nil {
				return cc, nil
			}
			c, err := content.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating content context: %w", err)
			}
			cc = c
			return cc, nil
		}

		switch strings.ToLower(strings.TrimSpace(action)) {
		case "topics":
			return printTopics(ctx, connect)
		case "news":
			return printNews(ctx, connect)
		case "detail":
			if strings.TrimSpace(topicID) == "" {
				return fmt.Errorf("-topic-id is required for -action detail")
			}
			return printDetail(ctx, connect)
		case "replies":
			if strings.TrimSpace(replyTopic) == "" {
				return fmt.Errorf("-reply-topic is required for -action replies")
			}
			return printReplies(ctx, connect)
		case "mine", "my-topics":
			return printMine(ctx, connect)
		case "create-topic", "create":
			return doCreateTopic(ctx, cfg, connect)
		case "reply":
			return doReply(ctx, cfg, connect)
		default:
			return fmt.Errorf(
				"unknown -action %q: want topics, news, detail, replies, mine, create-topic or reply",
				action)
		}
	})
}

// ----------------------------------------------------------------- read paths

func printTopics(ctx context.Context, connect func() (*content.ContentContext, error)) error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("-symbol is required for -action topics")
	}
	cc, err := connect()
	if err != nil {
		return err
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

func printNews(ctx context.Context, connect func() (*content.ContentContext, error)) error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("-symbol is required for -action news")
	}
	cc, err := connect()
	if err != nil {
		return err
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

func printDetail(ctx context.Context, connect func() (*content.ContentContext, error)) error {
	cc, err := connect()
	if err != nil {
		return err
	}
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

func printReplies(ctx context.Context, connect func() (*content.ContentContext, error)) error {
	if page < 1 {
		return fmt.Errorf("-page must be at least 1, got %d", page)
	}
	if size < 1 || size > 50 {
		// The SDK documents 1-50 for this option; check it here rather than
		// letting the API reject it.
		return fmt.Errorf("-size must be between 1 and 50 for -action replies, got %d", size)
	}
	cc, err := connect()
	if err != nil {
		return err
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

func printMine(ctx context.Context, connect func() (*content.ContentContext, error)) error {
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
	cc, err := connect()
	if err != nil {
		return err
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

// ---------------------------------------------------------------- write paths
//
// Print the exact body, ask the gate, then connect. Same order, always.

func doCreateTopic(ctx context.Context, cfg *appcfg.Config,
	connect func() (*content.ContentContext, error)) error {

	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("-body is required for -action create-topic (plain text for post, Markdown for article)")
	}
	tt := strings.ToLower(strings.TrimSpace(topicType))
	switch tt {
	case "", "post", "article":
	default:
		return fmt.Errorf("unknown -topic-type %q: want article, post or empty", topicType)
	}
	if tt == "article" && strings.TrimSpace(title) == "" {
		return fmt.Errorf("-title is required for -action create-topic when -topic-type article")
	}
	tks := splitList(tickers)
	if len(tks) > 10 {
		return fmt.Errorf("-tickers allows at most 10 entries, got %d", len(tks))
	}
	tags := splitList(hashtags)
	if len(tags) > 5 {
		return fmt.Errorf("-hashtags allows at most 5 entries, got %d", len(tags))
	}

	// Only the fields the SDK actually serialises are listed; it omits title,
	// topic_type, tickers and hashtags when they are empty.
	fields := [][2]string{{"body", body}}
	if strings.TrimSpace(title) != "" {
		fields = append(fields, [2]string{"title", title})
	}
	if tt != "" {
		fields = append(fields, [2]string{"topic_type", tt})
	}
	if len(tks) > 0 {
		fields = append(fields, [2]string{"tickers", strings.Join(tks, ",")})
	}
	if len(tags) > 0 {
		fields = append(fields, [2]string{"hashtags", strings.Join(tags, ",")})
	}

	describe("CreateTopic", "POST /v1/content/topics", fields,
		"PUBLIC AND IRREVERSIBLE: this is published under your account and "+
			"visible to other users. The SDK exposes no delete-topic method, so "+
			"it cannot be retracted from this demo.")
	if !contentGate(cfg, "publish a new topic") {
		return nil
	}

	cc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := cc.CreateTopic(c, &content.CreateTopicOptions{
		Title:     title,
		Body:      body,
		TopicType: tt,
		Tickers:   tks,
		Hashtags:  tags,
	})
	if err != nil {
		return fmt.Errorf("publishing topic: %w", err)
	}
	fmt.Printf("topic published, id=%s\n", cli.OrDash(id))
	return nil
}

func doReply(ctx context.Context, cfg *appcfg.Config,
	connect func() (*content.ContentContext, error)) error {

	if strings.TrimSpace(replyTopic) == "" {
		return fmt.Errorf("-reply-topic is required for -action reply")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("-body is required for -action reply (plain text; Markdown is not rendered)")
	}

	fields := [][2]string{
		{"topic_id", replyTopic},
		{"body", body},
	}
	// The SDK omits reply_to_id when it is empty or the literal "0".
	if strings.TrimSpace(replyToID) != "" && strings.TrimSpace(replyToID) != "0" {
		fields = append(fields, [2]string{"reply_to_id", replyToID})
	}

	describe("CreateTopicReply", "POST /v1/content/topics/{id}/comments", fields,
		"PUBLIC AND IRREVERSIBLE: the reply is published under your account. "+
			"The SDK's rate limit makes the first 3 replies per topic free, then "+
			"throttles progressively (3s, 5s, 8s, 13s, 21s, 34s, 55s cap).")
	if !contentGate(cfg, fmt.Sprintf("reply to topic %s", replyTopic)) {
		return nil
	}

	cc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r, err := cc.CreateTopicReply(c, replyTopic, &content.CreateReplyOptions{
		Body:      body,
		ReplyToID: replyToID,
	})
	if err != nil {
		return fmt.Errorf("replying to topic %s: %w", replyTopic, err)
	}
	if r == nil {
		fmt.Printf("reply to topic %s accepted\n", replyTopic)
		return nil
	}
	fmt.Printf("reply published to topic %s, id=%s created %s\n",
		replyTopic, cli.OrDash(r.ID), fmtTime(r.CreatedAt))
	return nil
}

// ------------------------------------------------------------------- the gate

// contentGate is the choke point for both publishing calls. It returns true
// only when the caller may proceed; on refusal it lists every unsatisfied
// condition and exits 3 without a single request being made.
//
// The three conditions are independent, so no one of them can be satisfied on
// its own. Mode is required because publishing speaks publicly as the account
// owner — it is the closest this demo comes to a social action, which is
// exactly why it needs all three.
func contentGate(cfg *appcfg.Config, actionDesc string) bool {
	var reasons []string
	if !confirm {
		reasons = append(reasons, "missing --confirm-live-content on the command line")
	}
	if contentDryRun() {
		reasons = append(reasons,
			"LONGPORT_CONTENT_DRY_RUN is on (default 1; set it to 0 to allow publishing)")
	}
	if cfg.Mode != appcfg.ModeLive {
		reasons = append(reasons, fmt.Sprintf(
			"LONGPORT_MODE=%s (publishing requires LONGPORT_MODE=live)", cfg.Mode))
	}
	if len(reasons) == 0 {
		return true
	}

	fmt.Fprintf(os.Stderr, "\n[DRY-RUN] BLOCKED: refusing to %s.\n", actionDesc)
	fmt.Fprintf(os.Stderr, "[DRY-RUN] Unsatisfied condition(s):\n")
	for _, r := range reasons {
		fmt.Fprintf(os.Stderr, "[DRY-RUN]   - %s\n", r)
	}
	fmt.Fprintf(os.Stderr,
		"[DRY-RUN] NOTHING was sent to Longbridge. All three are required:\n"+
			"[DRY-RUN]   LONGPORT_CONTENT_DRY_RUN=0  +  --confirm-live-content  +  LONGPORT_MODE=live\n")
	os.Exit(exitBlocked)
	return false // unreachable; keeps vet happy about the control flow
}

// contentDryRun reports whether publishing is blocked. Local to this package
// so the shared guard file needs no change; an unparseable value fails safe.
func contentDryRun() bool {
	v, ok := os.LookupEnv("LONGPORT_CONTENT_DRY_RUN")
	if !ok {
		return true
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return true
	}
	return b
}

func validateContentDryRun() error {
	v, ok := os.LookupEnv("LONGPORT_CONTENT_DRY_RUN")
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
		return fmt.Errorf("invalid LONGPORT_CONTENT_DRY_RUN=%q: want 1, true, 0 or false", v)
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// describe prints the exact JSON body the SDK would post, on [DRY-RUN]-prefixed
// lines so a captured block is never mistaken for a real publication.
func describe(op, http string, fields [][2]string, warning string) {
	fmt.Printf("\n[DRY-RUN] --- %s (%s) request that would be sent ---\n", op, http)
	for _, kv := range fields {
		fmt.Printf("[DRY-RUN]   %-12s %s\n", kv[0], cli.OrDash(kv[1]))
	}
	if warning != "" {
		fmt.Printf("[DRY-RUN]   WARNING        %s\n", warning)
	}
}

func limitSlice[T any](items []T, n int) []T {
	if n > 0 && len(items) > n {
		return items[:n]
	}
	return items
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
