package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// The -size bounds are the interesting validation in this command, because they
// are the SDK's own documented ranges checked locally: content/requests.go says
// MyTopicsOptions.Size is "default 50, range 1-500" and ListTopicRepliesOptions.Size
// is "default 20, range 1-50". Letting the API reject an out-of-range size would
// mean a 400 whose message names neither the flag nor the limit.
//
// Both checks live inside the print functions, after the required-flag check and
// before connect(), so the tests hand in a connect func that records being
// called. A bound that is checked too late shows up as a connect attempt.

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stderr
	os.Stderr = f
	fn()
	os.Stderr = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stderr: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(b)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stdout
	os.Stdout = f
	fn()
	os.Stdout = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stdout: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(b)
}

type contentState struct {
	symbol     string
	topicID    string
	replyTopic string
	topicType  string
	page       int
	size       int
	limit      int
	title      string
	body       string
	tickers    string
	hashtags   string
	replyToID  string
	confirm    bool
}

func setContentFlags(t *testing.T, s contentState) {
	t.Helper()
	prev := contentState{symbol, topicID, replyTopic, topicType, page, size, limit,
		title, body, tickers, hashtags, replyToID, confirm}
	symbol, topicID, replyTopic, topicType = s.symbol, s.topicID, s.replyTopic, s.topicType
	page, size, limit = s.page, s.size, s.limit
	title, body, tickers, hashtags, replyToID = s.title, s.body, s.tickers, s.hashtags, s.replyToID
	confirm = s.confirm
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		symbol, topicID, replyTopic, topicType = prev.symbol, prev.topicID, prev.replyTopic, prev.topicType
		page, size, limit = prev.page, prev.size, prev.limit
		title, body, tickers, hashtags, replyToID = prev.title, prev.body, prev.tickers, prev.hashtags, prev.replyToID
		confirm = prev.confirm
	})
}

func validContentFlags(t *testing.T) {
	t.Helper()
	setContentFlags(t, contentState{
		symbol:     "700.HK",
		replyTopic: "12345",
		topicType:  "",
		page:       1,
		size:       20,
		limit:      20,
	})
}

func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

// errConnectProvesValidationRan stands in for a successful SDK context. A test
// that receives this error back knows the local validation let the call through.
var errConnectProvesValidationRan = errors.New("connect reached")

type failConnect struct{ calls int }

func (c *failConnect) connect() (*content.ContentContext, error) {
	c.calls++
	return nil, errConnectProvesValidationRan
}

func (c *failConnect) reached() bool { return c.calls > 0 }

// ------------------------------------------------------------------ -size on replies

// content.ListTopicRepliesOptions documents Size as "default 20, range 1-50".
// Both boundaries are accepted, and one step outside either is refused locally.
func TestPrintReplies_SizeBoundsMatchTheSDKDocumentedRangeOf1To50(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		wantPass bool
	}{
		{"the documented default", 20, true},
		{"the lower boundary", 1, true},
		{"one above the lower boundary", 2, true},
		{"the upper boundary", 50, true},
		{"one below the lower boundary", 0, false},
		{"one above the upper boundary", 51, false},
		{"far above the upper boundary", 500, false},
		{"negative", -1, false},
		{"the minimum int", -2147483648, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("size=%d", tt.size), func(t *testing.T) {
			validContentFlags(t)
			size = tt.size
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = printReplies(context.Background(), c.connect) })

			if !tt.wantPass {
				if err == nil {
					t.Fatalf("printReplies(-size %d) = nil; the API would reject it with a message "+
						"that names neither the flag nor the 1-50 limit", tt.size)
				}
				if !strings.Contains(err.Error(), "-size") {
					t.Errorf("error %q does not name -size", err)
				}
				if !strings.Contains(err.Error(), "1 and 50") {
					t.Errorf("error %q does not state the limit; the user cannot tell what to type instead", err)
				}
				if !strings.Contains(err.Error(), fmt.Sprint(tt.size)) {
					t.Errorf("error %q does not echo the value that was rejected", err)
				}
				if c.reached() {
					t.Errorf("connect() was called with an out-of-range -size %d; the bound is checked "+
						"before any client exists", tt.size)
				}
				return
			}
			// Reaching connect is the proof the bound was accepted locally.
			if !c.reached() {
				t.Errorf("printReplies(-size %d) never reached connect(); a value inside the "+
					"documented 1-50 range was refused", tt.size)
			}
			if !errors.Is(err, errConnectProvesValidationRan) {
				t.Errorf("printReplies(-size %d) = %v, want the sentinel proving validation passed", tt.size, err)
			}
		})
	}
}

// The two ranges differ, and one of them is 50 while the other is 500. A
// copy-paste that swapped them would accept -size 400 on replies and reject
// -size 100 on mine; this asserts each action against its own limit.
func TestPrintRepliesAndPrintMine_UseDifferentAndCorrectSizeLimits(t *testing.T) {
	// 100 is inside mine's range and outside replies'.
	validContentFlags(t)
	size = 100
	c := &failConnect{}
	captureStdout(t, func() { _ = printReplies(context.Background(), c.connect) })
	if c.reached() {
		t.Error("printReplies accepted -size 100; the replies range is 1-50, not 1-500")
	}
	c2 := &failConnect{}
	captureStdout(t, func() { _ = printMine(context.Background(), c2.connect) })
	if !c2.reached() {
		t.Error("printMine refused -size 100; the mine range is 1-500")
	}

	// 50 is inside both, so it must be accepted by both.
	validContentFlags(t)
	size = 50
	c3 := &failConnect{}
	captureStdout(t, func() { _ = printReplies(context.Background(), c3.connect) })
	if !c3.reached() {
		t.Error("printReplies refused -size 50, which is its documented upper boundary")
	}
}

// The SDK's own comment on ListTopicRepliesOptions.Size is "range 1-50" and on
// MyTopicsOptions.Size is "range 1-500". If the SDK ever changes a documented
// range, this test is where the demo's copy of it should fail.
func TestPrintRepliesAndPrintMine_BoundsAgreeWithTheSDKDocumentedRanges(t *testing.T) {
	tests := []struct {
		action string
		lo, hi int
		run    func(*failConnect) error
	}{
		{"replies", 1, 50, func(c *failConnect) error {
			return printReplies(context.Background(), c.connect)
		}},
		{"mine", 1, 500, func(c *failConnect) error {
			return printMine(context.Background(), c.connect)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			for _, want := range []int{tt.lo, tt.hi} {
				validContentFlags(t)
				c := &failConnect{}
				page = 1
				size = want
				captureStdout(t, func() { _ = tt.run(c) })
				if !c.reached() {
					t.Errorf("%s refused -size %d, but the SDK documents %d-%d for this option",
						tt.action, want, tt.lo, tt.hi)
				}
			}
			for _, bad := range []int{tt.lo - 1, tt.hi + 1} {
				validContentFlags(t)
				c := &failConnect{}
				size = bad
				var err error
				captureStdout(t, func() { err = tt.run(c) })
				if err == nil {
					t.Errorf("%s accepted -size %d, one step outside the documented %d-%d",
						tt.action, bad, tt.lo, tt.hi)
				}
				if c.reached() {
					t.Errorf("%s connected with an out-of-range -size %d", tt.action, bad)
				}
			}
		})
	}
}

// ------------------------------------------------------------------ -page

func TestPrintRepliesAndPrintMine_PageMustBeAtLeastOne(t *testing.T) {
	for _, tt := range []struct {
		name string
		run  func(*failConnect) error
	}{
		{"replies", func(c *failConnect) error { return printReplies(context.Background(), c.connect) }},
		{"mine", func(c *failConnect) error { return printMine(context.Background(), c.connect) }},
	} {
		for _, p := range []int{0, -1, -100} {
			t.Run(fmt.Sprintf("%s/page=%d", tt.name, p), func(t *testing.T) {
				validContentFlags(t)
				page = p
				c := &failConnect{}
				var err error
				captureStdout(t, func() { err = tt.run(c) })
				if err == nil {
					t.Fatalf("%s accepted -page %d", tt.name, p)
				}
				if !strings.Contains(err.Error(), "-page") {
					t.Errorf("error %q does not name -page", err)
				}
				if c.reached() {
					t.Errorf("%s connected with -page %d", tt.name, p)
				}
			})
		}
	}
}

// The page check runs before the size check, so a request that is wrong in both
// ways names -page. Getting the order backwards would report -size and send the
// user looking at the wrong flag.
func TestPrintRepliesAndPrintMine_PageIsCheckedBeforeSize(t *testing.T) {
	for _, tt := range []struct {
		name string
		run  func(*failConnect) error
	}{
		{"replies", func(c *failConnect) error { return printReplies(context.Background(), c.connect) }},
		{"mine", func(c *failConnect) error { return printMine(context.Background(), c.connect) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			validContentFlags(t)
			page, size = 0, 9999
			var err error
			captureStdout(t, func() { err = tt.run(&failConnect{}) })
			if err == nil {
				t.Fatal("no error for a request that is wrong in both ways")
			}
			if !strings.Contains(err.Error(), "-page") {
				t.Errorf("error %q names -size where -page is checked first", err)
			}
		})
	}
}

// ------------------------------------------------------------------ -topic-type

func TestPrintMine_TopicTypeVocabulary(t *testing.T) {
	tests := []struct {
		in       string
		wantPass bool
	}{
		{"", true}, // no filter
		{"article", true},
		{"post", true},
		{"ARTICLE", true},
		{"  Post  ", true},
		{"\narticle\n", true},
		{"articles", false},
		{"video", false},
		{"comment", false},
		{"news", false},
		{"a-z", false},
		{"article-post", false},
		{"1", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			validContentFlags(t)
			topicType = tt.in
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = printMine(context.Background(), c.connect) })
			if !tt.wantPass {
				if err == nil {
					t.Fatalf("printMine(-topic-type %q) = nil", tt.in)
				}
				if !strings.Contains(err.Error(), "-topic-type") {
					t.Errorf("error %q does not name -topic-type", err)
				}
				if c.reached() {
					t.Errorf("printMine connected with an unknown -topic-type %q", tt.in)
				}
				return
			}
			if !c.reached() {
				t.Errorf("printMine(-topic-type %q) never reached connect()", tt.in)
			}
		})
	}
}

// ------------------------------------------------------------------ write paths

func TestWriteActions_RefuseMissingFlagsBeforeAnySDKContextIsCreated(t *testing.T) {
	tests := []struct {
		name    string
		set     func(t *testing.T)
		run     func(*failConnect) error
		wantErr string
	}{
		{
			name: "create-topic without -body",
			set:  func(t *testing.T) { validContentFlags(t); body = "" },
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-body",
		},
		{
			name: "create-topic with a whitespace-only -body",
			set:  func(t *testing.T) { validContentFlags(t); body = "   " },
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-body",
		},
		{
			name: "create-topic article without -title",
			set: func(t *testing.T) {
				validContentFlags(t)
				body, topicType, title = "Markdown body", "article", ""
			},
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-title",
		},
		{
			name: "create-topic with an unknown -topic-type",
			set: func(t *testing.T) {
				validContentFlags(t)
				body, topicType = "text", "video"
			},
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-topic-type",
		},
		{
			name: "create-topic with more than ten -tickers",
			set: func(t *testing.T) {
				validContentFlags(t)
				body = "text"
				tickers = "1.HK,2.HK,3.HK,4.HK,5.HK,6.HK,7.HK,8.HK,9.HK,10.HK,11.HK"
			},
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-tickers",
		},
		{
			name: "create-topic with more than five -hashtags",
			set: func(t *testing.T) {
				validContentFlags(t)
				body = "text"
				hashtags = "a,b,c,d,e,f"
			},
			run: func(c *failConnect) error {
				return doCreateTopic(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-hashtags",
		},
		{
			name: "reply without -reply-topic",
			set:  func(t *testing.T) { validContentFlags(t); body = "Agreed."; replyTopic = "" },
			run: func(c *failConnect) error {
				return doReply(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-reply-topic",
		},
		{
			name: "reply without -body",
			set:  func(t *testing.T) { validContentFlags(t); body = "" },
			run: func(c *failConnect) error {
				return doReply(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set(t)
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = tt.run(c) })
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the flag error: an unusable flag must be reported "+
					"as such even when the gate is shut. got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.reached() {
				t.Errorf("connect() was called; validation must precede any client")
			}
		})
	}
}

// Ten tickers is the SDK's documented maximum (CreateTopicOptions.Tickers:
// "optional, max 10"), so ten must be accepted and eleven refused.
func TestDoCreateTopic_TickerAndHashtagCountsMatchTheSDKDocumentedMaximums(t *testing.T) {
	tests := []struct {
		name     string
		tickers  string
		hashtags string
		wantPass bool
	}{
		{"no tickers and no hashtags", "", "", true},
		{"ten tickers", "1.HK,2.HK,3.HK,4.HK,5.HK,6.HK,7.HK,8.HK,9.HK,10.HK", "", true},
		{"eleven tickers", "1.HK,2.HK,3.HK,4.HK,5.HK,6.HK,7.HK,8.HK,9.HK,10.HK,11.HK", "", false},
		{"five hashtags", "", "a,b,c,d,e", true},
		{"six hashtags", "", "a,b,c,d,e,f", false},
		// Ten entries with stray commas and spaces are ten entries, not twenty.
		{"ten tickers with padding", " 1.HK , 2.HK ,3.HK,4.HK,5.HK,6.HK,7.HK,8.HK,9.HK,10.HK", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validContentFlags(t)
			body, tickers, hashtags, topicType = "text", tt.tickers, tt.hashtags, ""
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = doCreateTopic(context.Background(), blockedCfg(), c.connect) })
			if !tt.wantPass {
				if err == nil {
					t.Fatalf("doCreateTopic accepted %d tickers / %d hashtags, past the SDK's documented "+
						"maxima of 10 and 5", len(splitList(tt.tickers)), len(splitList(tt.hashtags)))
				}
				if c.reached() {
					t.Error("connect() was called despite the count check failing")
				}
				return
			}
			if err == nil || !isBlocked(err) {
				t.Fatalf("doCreateTopic = %v, want a *config.BlockedError", err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
		})
	}
}

// A publish that passes every check is refused by the gate, with no network
// call. The preview names the endpoint and says the post is public and
// unretractable, because that is the whole risk of the action.
func TestDoCreateTopic_APublishIsRefusedByTheGateWithNoNetworkCall(t *testing.T) {
	validContentFlags(t)
	body, title, topicType = "A body", "A title", "article"
	tickers, hashtags = "700.HK,9988.HK", "hk,tech"
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doCreateTopic(context.Background(), blockedCfg(), c.connect) })
	if err == nil {
		t.Fatal("doCreateTopic() = nil with the gate shut")
	}
	if !isBlocked(err) {
		t.Errorf("doCreateTopic() = %v, want a *config.BlockedError so the exit status is %d", err, appcfg.ExitBlocked)
	}
	if c.reached() {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	for _, want := range []string{
		"[DRY-RUN]", "CreateTopic", "POST /v1/content/topics",
		"body", "A body", "title", "A title", "topic_type", "article",
		"tickers", "700.HK,9988.HK", "hashtags", "hk,tech",
		"PUBLIC AND IRREVERSIBLE", "no delete-topic method",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
		}
	}
}

// The SDK omits reply_to_id when it is empty or the literal "0", so the preview
// must omit it too — otherwise the dry run shows a field the real request does
// not carry.
func TestDoReply_PreviewOmitsReplyToIDWhenItIsEmptyOrZero(t *testing.T) {
	for _, tt := range []struct {
		name     string
		replyTo  string
		wantShow bool
	}{
		{"empty means top-level", "", false},
		{"the literal zero also means top-level", "0", false},
		{"whitespace-only is also empty", "   ", false},
		{"a real id is shown", "555", true},
		{"a padded real id is shown", " 555 ", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			validContentFlags(t)
			body, replyToID = "Agreed.", tt.replyTo
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = doReply(context.Background(), blockedCfg(), c.connect) })
			if err == nil || !isBlocked(err) {
				t.Fatalf("doReply() = %v, want a *config.BlockedError", err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
			shown := strings.Contains(out, "reply_to_id")
			if shown != tt.wantShow {
				t.Errorf("preview reply_to_id present = %v, want %v.\npreview was:\n%s", shown, tt.wantShow, out)
			}
			for _, want := range []string{
				"CreateTopicReply", "POST /v1/content/topics/{id}/comments",
				"topic_id", "12345", "Agreed.", "PUBLIC AND IRREVERSIBLE", "rate limit",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
				}
			}
		})
	}
}

func TestGate_RefusalIsExplainedOnStderrAndReturnsABlockedError(t *testing.T) {
	validContentFlags(t)
	var err error
	out := captureStderr(t, func() { err = gate(blockedCfg(), "publish a new topic") })
	if err == nil {
		t.Fatal("gate() = nil; a blocked publish must be distinguishable from a published one")
	}
	if !isBlocked(err) {
		t.Errorf("gate() = %v, want a *config.BlockedError", err)
	}
	for _, want := range []string{
		"[DRY-RUN] BLOCKED:", "publish a new topic",
		"LONGPORT_CONTENT_DRY_RUN", "--confirm-live-content", "LONGPORT_MODE=live or =paper",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q.\nblock was:\n%s", want, out)
		}
	}
}

// ------------------------------------------------------------------ helpers

func TestLimitSlice_ClipsOnlyWhenTheLimitIsSmaller(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	tests := []struct {
		name  string
		n     int
		want  []int
		count int
	}{
		{"under the limit", 10, []int{1, 2, 3, 4, 5}, 5},
		{"exactly the limit", 5, []int{1, 2, 3, 4, 5}, 5},
		{"one under the limit", 4, []int{1, 2, 3, 4}, 4},
		{"zero means all", 0, []int{1, 2, 3, 4, 5}, 5},
		{"negative means all", -1, []int{1, 2, 3, 4, 5}, 5},
		{"one over the length", 6, []int{1, 2, 3, 4, 5}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := limitSlice(items, tt.n)
			if len(got) != tt.count {
				t.Fatalf("limitSlice(%v, %d) returned %d items, want %d", items, tt.n, len(got), tt.count)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("limitSlice[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
	// An empty input must stay empty rather than becoming nil-vs-empty confusion.
	if got := limitSlice([]int(nil), 3); len(got) != 0 {
		t.Errorf("limitSlice(nil, 3) = %v, want empty", got)
	}
}

func TestShown_ReportsHowManyRowsAreOnScreen(t *testing.T) {
	tests := []struct {
		limit, total, want int
		note               string
	}{
		{20, 5, 5, "fewer rows than the limit"},
		{20, 20, 20, "exactly the limit"},
		{20, 21, 20, "one row clipped"},
		{5, 100, 5, "heavily clipped"},
		{0, 7, 7, "zero means all"},
		{-1, 7, 7, "negative means all"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d/%d", tt.limit, tt.total)+"/"+tt.note, func(t *testing.T) {
			if got := shown(tt.limit, tt.total); got != tt.want {
				t.Errorf("shown(%d, %d) = %d, want %d", tt.limit, tt.total, got, tt.want)
			}
		})
	}
}

func TestSplitList_SplitsOnCommasAndDropsBlanks(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,", nil},
		{"700.HK", []string{"700.HK"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got := splitList(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitList(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitList(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestOneLine_CollapsesNewlinesAndRunsOfWhitespace(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"one line", "one line"},
		{"two\nlines", "two lines"},
		{"trailing\n", "trailing"},
		{"\nleading", "leading"},
		{"  lots   of \t space  ", "lots of space"},
		{"a\r\nb", "a b"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			if got := oneLine(tt.in); got != tt.want {
				t.Errorf("oneLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFmtTime_ZeroTimeRendersAsAbsent(t *testing.T) {
	if got := fmtTime(time.Time{}); got != "-" {
		t.Errorf("fmtTime(zero) = %q, want \"-\"", got)
	}
	when := time.Date(2025, 3, 1, 12, 30, 45, 0, time.UTC)
	if got := fmtTime(when); got != "2025-03-01 12:30:45" {
		t.Errorf("fmtTime = %q, want the formatted value", got)
	}
}
