// Command sharelist reads the Longbridge share-list surface: your own
// sharelists, the detail of one sharelist including its constituents, and the
// platform's popular sharelists.
//
// It is entirely READ-ONLY.
//
// # WHAT IS DELIBERATELY NOT HERE
//
// SharelistContext has eight exported methods. Only the three read methods
// (List, Detail, Popular) are implemented here. The other five — Create,
// Delete, AddSecurities, RemoveSecurities and SortSecurities — all mutate
// server-side state belonging to the account and are not idempotent, exactly
// like the watchlist writes. Covering them would mean adding a third write
// gate, which is out of scope for this demo. They are named in the note this
// command prints at startup so the omission is visible rather than silent.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/longbridge/openapi-go/sharelist"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action     string
	count      int
	detailID   int64
	popular    bool
	stockLimit int
	timeout    time.Duration
)

func main() {
	u := cli.NewUsage("sharelist", "read-only share lists: your lists, one list's detail, popular lists")
	u.FS.StringVar(&action, "action", "list",
		"list (your sharelists) | detail (-id) | popular (trending sharelists)")
	u.FS.IntVar(&count, "count", 20, "how many sharelists to return (list and popular)")
	u.FS.Int64Var(&detailID, "id", 0, "sharelist id, required for -action detail")
	u.FS.BoolVar(&popular, "popular", false, "alias for -action popular")
	u.FS.IntVar(&stockLimit, "stock-limit", 20, "how many constituent rows to print per sharelist")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only; an open order gate here would mean the
	// environment is wrong, not that we should start writing.
	if err := cfg.GuardWrite("run the sharelist reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: sharelist is read-only but the order gate is open"))
	}
	fmt.Fprintf(os.Stderr,
		"[sharelist] read-only: List, Detail and Popular only. Not implemented (they mutate "+
			"server state and would need a third write gate): Create, Delete, AddSecurities, "+
			"RemoveSecurities, SortSecurities.\n")

	cli.Run(func(ctx context.Context) error {
		// The sharelist package has no Close(); it is a thin HTTP client.
		sc, err := sharelist.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating sharelist context: %w", err)
		}

		act := action
		if popular {
			act = "popular"
		}

		switch act {
		case "list":
			return printLists(ctx, sc, "Your sharelists", false)
		case "popular":
			return printLists(ctx, sc, "Popular sharelists", true)
		case "detail":
			if detailID == 0 {
				return fmt.Errorf("-id is required for -action detail")
			}
			return printDetail(ctx, sc)
		default:
			return fmt.Errorf("unknown -action %q: want list, detail or popular", action)
		}
	})
}

func printLists(ctx context.Context, sc *sharelist.SharelistContext, title string, pop bool) error {
	if count < 1 {
		return fmt.Errorf("-count must be at least 1, got %d", count)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("%s (count %d)", title, count))
	var (
		res *sharelist.SharelistList
		err error
	)
	if pop {
		res, err = sc.Popular(c, uint32(count))
		if err != nil {
			return fmt.Errorf("popular sharelists: %w", err)
		}
	} else {
		res, err = sc.List(c, uint32(count))
		if err != nil {
			return fmt.Errorf("sharelist list: %w", err)
		}
	}
	if res == nil {
		fmt.Println("   (no sharelist response)")
		return nil
	}
	printSharelists(res.Sharelists)
	if len(res.SubscribedSharelists) > 0 {
		cli.Section("Subscribed sharelists")
		printSharelists(res.SubscribedSharelists)
	}
	if res.TailMark != "" {
		fmt.Printf("\ntail_mark: %s\n", res.TailMark)
	}
	return nil
}

func printSharelists(lists []sharelist.SharelistInfo) {
	if len(lists) == 0 {
		fmt.Println("   (none)")
		return
	}
	fmt.Printf("%-10s %-28s %-40s %-12s %-9s %-8s %-6s %s\n",
		"ID", "NAME", "DESCRIPTION", "SUBS", "YTD%", "CHG%", "SUB'D", "TYPE")
	for _, s := range lists {
		fmt.Printf("%-10d %-28s %-40s %-12d %-9s %-8s %-6v %s\n",
			s.ID, cli.Truncate(s.Name, 28), cli.Truncate(s.Description, 40),
			s.SubscribersCount, cli.Dec(s.ThisYearChg), cli.Dec(s.Chg),
			s.Subscribed, sharelistTypeName(s.SharelistType))
	}
	fmt.Printf("\n(%d sharelists; use -action detail -id <ID> for constituents)\n", len(lists))
}

func printDetail(ctx context.Context, sc *sharelist.SharelistContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Sharelist %d", detailID))
	res, err := sc.Detail(c, detailID)
	if err != nil {
		return fmt.Errorf("sharelist detail %d: %w", detailID, err)
	}
	if res == nil {
		fmt.Println("   (no sharelist returned)")
		return nil
	}
	s := res.Sharelist
	fmt.Printf("name        %s\n", cli.OrDash(s.Name))
	fmt.Printf("type        %s\n", sharelistTypeName(s.SharelistType))
	if s.IndustryCode != "" {
		fmt.Printf("industry    %s\n", s.IndustryCode)
	}
	fmt.Printf("description %s\n", cli.OrDash(s.Description))
	fmt.Printf("cover       %s\n", cli.OrDash(s.Cover))
	fmt.Printf("subscribers %d   subscribed %v   owner %v\n",
		s.SubscribersCount, s.Subscribed, res.Scopes.IsSelf)
	fmt.Printf("created     %s\n", fmtTime(s.CreatedAt))
	fmt.Printf("edited      %s\n", fmtTime(s.EditedAt))
	fmt.Printf("chg%%        %s   ytd%% %s\n", cli.Dec(s.Chg), cli.Dec(s.ThisYearChg))

	if len(s.Stocks) == 0 {
		fmt.Println("\n   (no constituents)")
		return nil
	}
	shown := s.Stocks
	if stockLimit > 0 && len(shown) > stockLimit {
		shown = shown[:stockLimit]
	}
	cli.Section(fmt.Sprintf("Constituents (%d of %d)", len(shown), len(s.Stocks)))
	fmt.Printf("%-12s %-26s %-12s %-12s %-9s %-8s %s\n",
		"SYMBOL", "NAME", "LAST", "CHG%", "MKT", "STATUS", "DELAYED")
	for _, st := range shown {
		fmt.Printf("%-12s %-26s %-12s %-12s %-9s %-8s %s\n",
			st.Symbol, cli.Truncate(st.Name, 26), cli.Dec4(st.LastDone),
			cli.Dec(st.Change), cli.OrDash(st.Market), intStr(st.TradeStatus),
			boolStr(st.Latency))
	}
	if len(shown) < len(s.Stocks) {
		fmt.Printf("\n(%d of %d constituents; raise -stock-limit for more)\n", len(shown), len(s.Stocks))
	}
	return nil
}

func sharelistTypeName(t sharelist.SharelistType) string {
	switch t {
	case sharelist.SharelistTypeRegular:
		return "regular"
	case sharelist.SharelistTypeOfficial:
		return "official"
	case sharelist.SharelistTypeIndustry:
		return "industry"
	}
	return fmt.Sprintf("unknown(%d)", int32(t))
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func intStr(v *int32) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *v)
}

func boolStr(v *bool) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", *v)
}
