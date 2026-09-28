// Command watch subscribes to the realtime quote stream and prints quotes,
// ticks, order-book depth and broker queues until interrupted.
//
// It shuts down gracefully on SIGINT/SIGTERM: it unsubscribes, closes the quote
// context, and exits 0. It is read-only and places no orders.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/longbridge/openapi-go/quote"
	"github.com/longbridge/openapi-go/trade"
	"github.com/shopspring/decimal"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	symbols   string
	wantQuote bool
	wantTrade bool
	wantDepth bool
	brokerQ   bool
	orderPush bool
	topics    string
	interval  time.Duration
	timeout   time.Duration
)

func main() {
	u := cli.NewUsage("watch", "realtime quote stream (quotes, ticks, depth, brokers)")
	u.FS.StringVar(&symbols, "symbols", "700.HK,AAPL.US", "comma-separated symbols to subscribe to")
	u.FS.BoolVar(&wantQuote, "quote", true, "stream last-done quotes")
	u.FS.BoolVar(&wantTrade, "trade", true, "stream individual ticks")
	u.FS.BoolVar(&wantDepth, "depth", true, "stream order-book depth")
	u.FS.BoolVar(&brokerQ, "brokers", false, "stream broker queues")
	u.FS.BoolVar(&orderPush, "orders", false,
		"stream ORDER updates over the trade websocket instead of quote data (see -topics)")
	u.FS.StringVar(&topics, "topics", "trade",
		"comma-separated trade topics for -orders, e.g. trade, order")
	u.FS.DurationVar(&interval, "interval", 2*time.Second,
		"minimum gap between depth prints, to keep the output readable")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "HTTP timeout for the initial subscription")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)

	// The order stream is a different websocket on a different context, and it
	// needs no symbols, so it takes a wholly separate path rather than being
	// folded into the quote subscription below.
	if orderPush {
		cli.Run(func(parent context.Context) error { return runOrderStream(parent, cfg) })
		return
	}

	list := splitList(symbols)
	if len(list) == 0 {
		cli.Fail(fmt.Errorf("-symbols is empty"))
	}

	var subTypes []quote.SubType
	if wantQuote {
		subTypes = append(subTypes, quote.SubTypeQuote)
	}
	if wantTrade {
		subTypes = append(subTypes, quote.SubTypeTrade)
	}
	if wantDepth {
		subTypes = append(subTypes, quote.SubTypeDepth)
	}
	if brokerQ {
		subTypes = append(subTypes, quote.SubTypeBrokers)
	}
	if len(subTypes) == 0 {
		cli.Fail(fmt.Errorf("nothing to stream: enable at least one of -quote, -trade, -depth, -brokers"))
	}

	cli.Run(func(parent context.Context) error {
		qc, err := quote.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating quote context: %w", err)
		}
		defer func() {
			if err := qc.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: closing quote context: %v\n", err)
			}
		}()

		// Serialise writes to stdout: push callbacks arrive on SDK goroutines.
		var outMu sync.Mutex
		emit := func(format string, args ...any) {
			outMu.Lock()
			defer outMu.Unlock()
			fmt.Printf(format+"\n", args...)
		}

		// depth throttling state, guarded by outMu.
		var lastDepth time.Time

		qc.OnQuote(func(q *quote.PushQuote) {
			emit("%s QUOTE  last=%s open=%s high=%s low=%s vol=%d seq=%d",
				q.Symbol, dec(q.LastDone), dec(q.Open), dec(q.High), dec(q.Low),
				q.Volume, q.Sequence)
		})

		qc.OnTrade(func(t *quote.PushTrade) {
			for _, tk := range t.Trade {
				emit("%s TICK   price=%s vol=%d at=%s",
					t.Symbol, tk.Price, tk.Volume, cli.FmtTime(tk.Timestamp))
			}
		})

		qc.OnDepth(func(d *quote.PushDepth) {
			// The book changes many times a second; throttle so the terminal
			// stays readable.
			outMu.Lock()
			now := time.Now()
			if now.Sub(lastDepth) < interval {
				outMu.Unlock()
				return
			}
			lastDepth = now
			outMu.Unlock()

			best := func(side []*quote.Depth) string {
				if len(side) == 0 {
					return "-"
				}
				return fmt.Sprintf("%s x%d", dec(side[0].Price), side[0].Volume)
			}
			emit("%s DEPTH  bid=%-18s ask=%-18s seq=%d",
				d.Symbol, best(d.Bid), best(d.Ask), d.Sequence)
		})

		qc.OnBrokers(func(b *quote.PushBrokers) {
			fmt.Fprintf(os.Stderr, "%s BROKERS push received (seq=%d, %d ask / %d bid levels)\n",
				b.Symbol, b.Sequence, len(b.AskBrokers), len(b.BidBrokers))
		})

		subCtx, cancel := context.WithTimeout(parent, timeout)
		if err := qc.Subscribe(subCtx, list, subTypes, true); err != nil {
			cancel()
			return fmt.Errorf("subscribing to %s: %w", strings.Join(list, ","), err)
		}
		cancel()

		subs, err := qc.Subscriptions(parent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: reading subscriptions: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "subscribed: %v\n", subs)
		}
		fmt.Fprintf(os.Stderr, "streaming; press Ctrl-C to stop.\n")

		// parent is cancelled by cli.Run on SIGINT/SIGTERM.
		<-parent.Done()
		fmt.Fprintln(os.Stderr, "\nshutting down...")

		// Unsubscribe with a fresh context: parent is already cancelled.
		quitCtx, quitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer quitCancel()
		if err := qc.Unsubscribe(quitCtx, true, nil, nil); err != nil {
			fmt.Fprintf(os.Stderr, "warning: unsubscribing: %v\n", err)
		}
		return nil
	})
}

// runOrderStream subscribes to the account's order feed on the TRADE
// websocket and prints every order state change until interrupted.
//
// This is a separate context from the quote stream: order pushes arrive on
// wss://openapi-trade..., not the quote socket, so they cannot share a
// QuoteContext. It is still strictly read-only — subscribing to a push feed
// mutates nothing and places nothing.
func runOrderStream(parent context.Context, cfg *appcfg.Config) error {
	list := splitList(topics)
	if len(list) == 0 {
		return fmt.Errorf("-topics is empty")
	}

	tc, err := trade.NewFromCfg(cfg.SDK)
	if err != nil {
		return fmt.Errorf("creating trade context: %w", err)
	}
	defer func() {
		if err := tc.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: closing trade context: %v\n", err)
		}
	}()

	// Push callbacks arrive on SDK goroutines, so serialise stdout writes.
	var outMu sync.Mutex
	emit := func(format string, args ...any) {
		outMu.Lock()
		defer outMu.Unlock()
		fmt.Printf(format+"\n", args...)
	}

	tc.OnTrade(func(e *trade.PushEvent) {
		d := e.Data
		if d == nil {
			emit("ORDER event=%s (no payload)", cli.OrDash(e.Event))
			return
		}
		emit("%s ORDER %-20s status=%-16s side=%-5s type=%-6s qty=%-8s price=%-10s executed_qty=%-8s executed_price=%-10s updated=%s",
			d.Symbol, d.OrderId, d.Status, d.Side, d.OrderType,
			cli.OrDash(decStr(d.Quantity)), cli.Dec4(d.Price),
			cli.OrDash(decStr(d.ExecutedQuantity)), cli.Dec4(d.ExecutedPrice),
			cli.OrDash(d.UpdatedAt))
		if d.Msg != "" {
			emit("        msg: %s", d.Msg)
		}
	})

	subCtx, cancel := context.WithTimeout(parent, timeout)
	res, err := tc.Subscribe(subCtx, list)
	cancel()
	if err != nil {
		return fmt.Errorf("subscribing to %s: %w", strings.Join(list, ","), err)
	}
	fmt.Fprintf(os.Stderr, "subscribed: current=%v success=%v\n", res.Current, res.Success)
	// Report per-topic failures: a partial subscribe still "succeeds" overall,
	// and silently streaming nothing is the confusing outcome.
	for _, f := range res.Fail {
		fmt.Fprintf(os.Stderr, "topic %q refused: %s\n", f.Topic, f.Reason)
	}
	fmt.Fprintf(os.Stderr, "streaming order updates; press Ctrl-C to stop.\n")

	<-parent.Done()
	fmt.Fprintln(os.Stderr, "\nshutting down...")

	// Unsubscribe with a fresh context: parent is already cancelled.
	quitCtx, quitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer quitCancel()
	if _, err := tc.Unsubscribe(quitCtx, list); err != nil {
		fmt.Fprintf(os.Stderr, "warning: unsubscribing: %v\n", err)
	}
	return nil
}

// decStr renders an optional decimal as a plain (non-padded) string, for
// quantity-like fields where trailing zeros would be noise.
func decStr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
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

func dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(2)
}
