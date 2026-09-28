// Command executions shows fills (executions) and full order detail.
//
// The actions here are all READ-ONLY: today-executions, history-executions,
// order-detail, max-purchase, margin-ratio, fund-positions and cash-flow.
// None of them place, modify or cancel an order.
//
// The one exception is -action withdraw, which is a WRITE: it maps to the SDK's
// WithdrawOrder (an alias of CancelOrder). It therefore goes through the same
// three-way order gate as cmd/trade — dry run, --confirm-live and
// LONGPORT_MODE=live all required — and it is included here only so the demo
// covers the SDK method. The trade context is created lazily so a dry run never
// opens a websocket.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/trade"
	"github.com/shopspring/decimal"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action      string
	symbol      string
	orderID     string
	price       string
	side        string
	orderType   string
	days        int
	balanceType string
	page        int64
	pageSize    int64
	confirmLive bool
	timeout     time.Duration
)

func main() {
	u := cli.NewUsage("executions", "fills, order detail, buying power and cash flow")
	u.FS.StringVar(&action, "action", "today-executions",
		"today-executions | history-executions | order-detail | max-purchase | "+
			"margin-ratio | fund-positions | cash-flow | withdraw")
	u.FS.StringVar(&symbol, "symbol", "", "symbol filter, e.g. 700.HK (optional for most actions)")
	u.FS.StringVar(&orderID, "order-id", "", "order id (required for order-detail and withdraw)")
	u.FS.StringVar(&price, "price", "",
		"unit price as a decimal string for max-purchase (default 0, i.e. at market)")
	u.FS.StringVar(&side, "side", "Buy", "Buy or Sell (for max-purchase)")
	u.FS.StringVar(&orderType, "type", "LO", "order type for max-purchase: LO, ELO, MO, ...")
	u.FS.IntVar(&days, "days", 7, "lookback window in days for history-executions and cash-flow")
	u.FS.StringVar(&balanceType, "balance-type", "",
		"cash-flow filter: cash, stock or fund (empty = all)")
	u.FS.Int64Var(&page, "page", 0, "cash-flow page index (0 = first)")
	u.FS.Int64Var(&pageSize, "size", 50, "cash-flow page size")
	u.FS.BoolVar(&confirmLive, "confirm-live", false,
		"REQUIRED acknowledgement for -action withdraw; must be combined with LONGPORT_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)

	cli.Run(func(ctx context.Context) error {
		// Lazy: a dry run must not open a websocket.
		var tc *trade.TradeContext
		defer func() {
			if tc != nil {
				if err := tc.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: closing trade context: %v\n", err)
				}
			}
		}()
		connect := func() (*trade.TradeContext, error) {
			if tc != nil {
				return tc, nil
			}
			c, err := trade.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating trade context: %w", err)
			}
			tc = c
			return tc, nil
		}

		switch strings.ToLower(action) {
		case "today-executions":
			c, err := connect()
			if err != nil {
				return err
			}
			return printTodayExecutions(ctx, c)
		case "history-executions":
			c, err := connect()
			if err != nil {
				return err
			}
			return printHistoryExecutions(ctx, c)
		case "order-detail":
			c, err := connect()
			if err != nil {
				return err
			}
			return printOrderDetail(ctx, c)
		case "max-purchase":
			c, err := connect()
			if err != nil {
				return err
			}
			return printMaxPurchase(ctx, c)
		case "margin-ratio":
			c, err := connect()
			if err != nil {
				return err
			}
			return printMarginRatio(ctx, c)
		case "fund-positions":
			c, err := connect()
			if err != nil {
				return err
			}
			return printFundPositions(ctx, c)
		case "cash-flow":
			c, err := connect()
			if err != nil {
				return err
			}
			return printCashFlow(ctx, c)
		case "withdraw":
			return doWithdraw(ctx, cfg, connect)
		default:
			return fmt.Errorf(
				"unknown -action %q: want today-executions, history-executions, "+
					"order-detail, max-purchase, margin-ratio, fund-positions, "+
					"cash-flow or withdraw", action)
		}
	})
}

// ---------------------------------------------------------------- read paths

func printTodayExecutions(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Today's executions (fills)")
	fills, err := tc.TodayExecutions(c, &trade.GetTodayExecutions{Symbol: symbol})
	if err != nil {
		return fmt.Errorf("today executions: %w", err)
	}
	printExecutions(fills)
	return nil
}

func printHistoryExecutions(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	cli.Section(fmt.Sprintf("Executions from %s to %s",
		start.Format("2006-01-02"), end.Format("2006-01-02")))
	fills, err := tc.HistoryExecutions(c, &trade.GetHistoryExecutions{
		Symbol:  symbol,
		StartAt: start,
		EndAt:   end,
	})
	if err != nil {
		return fmt.Errorf("history executions: %w", err)
	}
	printExecutions(fills)
	return nil
}

func printExecutions(fills []*trade.Execution) {
	if len(fills) == 0 {
		fmt.Println("   (no executions in this window)")
		return
	}
	fmt.Printf("%-22s %-22s %-12s %-10s %-11s %s\n",
		"ORDER_ID", "TRADE_ID", "SYMBOL", "QUANTITY", "PRICE", "FILLED_AT")
	for _, f := range fills {
		fmt.Printf("%-22s %-22s %-12s %-10s %-11s %s\n",
			f.OrderId, f.TradeId, f.Symbol, f.Quantity, cli.Dec4(f.Price),
			f.TradeDoneAt.Local().Format("2006-01-02 15:04:05"))
	}
}

func printOrderDetail(ctx context.Context, tc *trade.TradeContext) error {
	if orderID == "" {
		return fmt.Errorf("-order-id is required for -action order-detail")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Order %s", orderID))
	d, err := tc.OrderDetail(c, orderID)
	if err != nil {
		return fmt.Errorf("order detail for %s: %w", orderID, err)
	}
	fmt.Printf("status=%s symbol=%s name=%s side=%s type=%s tif=%s\n",
		d.Status, d.Symbol, cli.Truncate(d.StockName, 24), d.Side, d.OrderType, d.TimeInForce)
	fmt.Printf("quantity=%d executed_quantity=%d price=%s executed_price=%s last_done=%s\n",
		d.Quantity, d.ExecutedQuantity, cli.Dec4(d.Price), cli.Dec4(d.ExecutedPrice),
		cli.Dec4(d.LastDone))
	fmt.Printf("submitted_at=%s updated_at=%s currency=%s\n",
		cli.OrDash(d.SubmittedAt), cli.OrDash(d.UpdatedAt), cli.OrDash(d.Currency))
	if d.Remark != "" {
		fmt.Printf("remark=%s\n", d.Remark)
	}
	if d.Msg != "" {
		fmt.Printf("message=%s\n", d.Msg)
	}
	// Conditional-order fields are only meaningful for LIT/MIT orders.
	if d.TriggerStatus != "" || d.TriggerPrice != nil {
		fmt.Printf("trigger: status=%s price=%s at=%s\n",
			cli.OrDash(string(d.TriggerStatus)), cli.Dec4(d.TriggerPrice),
			cli.OrDash(d.TriggerAt))
	}
	if d.TrailingAmount != nil || d.TrailingPercent != nil {
		fmt.Printf("trailing: amount=%s percent=%s limit_offset=%s\n",
			cli.Dec4(d.TrailingAmount), cli.Dec4(d.TrailingPercent),
			cli.Dec4(d.LimitOffset))
	}
	if d.OutsideRth != "" {
		fmt.Printf("outside_rth=%s expire_date=%s\n",
			cli.OrDash(string(d.OutsideRth)), cli.OrDash(d.ExpireDate))
	}
	// Commission-free and platform-deduction status decide the final cost.
	if d.FreeStatus != "" || d.DeductionsStatus != "" {
		fmt.Printf("commission_free: status=%s amount=%s currency=%s\n",
			cli.OrDash(string(d.FreeStatus)), cli.Dec4(d.FreeAmount),
			cli.OrDash(d.FreeCurrency))
		fmt.Printf("deductions: status=%s amount=%s currency=%s\n",
			cli.OrDash(string(d.DeductionsStatus)), cli.Dec4(d.DeductionsAmount),
			cli.OrDash(d.DeductionsCurrency))
		fmt.Printf("platform_deducted: status=%s amount=%s currency=%s\n",
			cli.OrDash(string(d.PlatformDeductedStatus)),
			cli.Dec4(d.PlatformDeductedAmount),
			cli.OrDash(d.PlatformDeductedCurrency))
	}
	if ch := d.ChargeDetail; len(ch.Items) > 0 {
		fmt.Printf("charges: total=%s %s\n", ch.TotalAmount.StringFixed(2), cli.OrDash(ch.Currency))
		for _, item := range ch.Items {
			fmt.Printf("  %-12s %s\n", string(item.Code), cli.Truncate(item.Name, 40))
		}
	}
	// The history block is a single snapshot of the order's latest state, not
	// a list: the SDK models it as one OrderHistoryDetail.
	h := d.History
	fmt.Println("  history snapshot:")
	fmt.Printf("    time=%s status=%s price=%s quantity=%d msg=%s\n",
		cli.OrDash(h.Time), h.Status, h.Price.StringFixed(4), h.Quantity,
		cli.Truncate(h.Msg, 40))
	return nil
}

func printMaxPurchase(ctx context.Context, tc *trade.TradeContext) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action max-purchase")
	}
	px, err := decimal.NewFromString(orZeroPrice(price))
	if err != nil {
		return fmt.Errorf("parsing -price %q: %w", price, err)
	}
	ot, err := parseOrderType(orderType)
	if err != nil {
		return err
	}
	os_, err := parseSide(side)
	if err != nil {
		return err
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Max purchase quantity for %s", symbol))
	resp, err := tc.EstimateMaxPurchaseQuantity(c, &trade.GetEstimateMaxPurchaseQuantity{
		Symbol:    symbol,
		OrderType: ot,
		Price:     px,
		Currency:  "HKD",
		Side:      os_,
	})
	if err != nil {
		return fmt.Errorf("estimate max purchase quantity: %w", err)
	}
	fmt.Printf("cash_max_qty=%d margin_max_qty=%d\n", resp.CashMaxQty, resp.MarginMaxQty)
	return nil
}

func printMarginRatio(ctx context.Context, tc *trade.TradeContext) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action margin-ratio")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Margin ratio for %s", symbol))
	r, err := tc.MarginRatio(c, symbol)
	if err != nil {
		return fmt.Errorf("margin ratio for %s: %w", symbol, err)
	}
	fmt.Printf("im_factor=%s mm_factor=%s fm_factor=%s\n",
		cli.Dec4(r.ImFactor), cli.Dec4(r.MmFactor), cli.Dec4(r.FmFactor))
	return nil
}

func printFundPositions(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var syms []string
	if symbol != "" {
		syms = []string{symbol}
	}

	cli.Section("Fund positions")
	channels, err := tc.FundPositions(c, syms)
	if err != nil {
		return fmt.Errorf("fund positions: %w", err)
	}
	total := 0
	for _, ch := range channels {
		fmt.Printf("-- channel %s\n", ch.AccountChannel)
		for _, p := range ch.Positions {
			total++
			fmt.Printf("   %-14s %-22s units=%-12s nav=%-12s cost_nav=%-12s %s\n",
				p.Symbol, cli.Truncate(p.SymbolName, 22), cli.Dec4(p.HoldingUnits),
				cli.Dec4(p.CurrentNetAssetValue), cli.Dec4(p.CostNetAssetValue),
				cli.OrDash(p.Currency))
		}
	}
	if total == 0 {
		fmt.Println("   (no fund positions)")
	}
	return nil
}

func printCashFlow(ctx context.Context, tc *trade.TradeContext) error {
	if days <= 0 {
		return fmt.Errorf("-days must be greater than 0, got %d", days)
	}
	bt, err := parseBalanceType(balanceType)
	if err != nil {
		return err
	}
	if page < 0 {
		return fmt.Errorf("-page must not be negative, got %d", page)
	}
	if pageSize <= 0 {
		return fmt.Errorf("-size must be greater than 0, got %d", pageSize)
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	cli.Section(fmt.Sprintf("Cash flow from %s to %s",
		start.Format("2006-01-02"), end.Format("2006-01-02")))
	rows, err := tc.CashFlow(c, &trade.GetCashFlow{
		StartAt:      start.Unix(),
		EndAt:        end.Unix(),
		BusinessType: bt,
		Symbol:       symbol,
		Page:         page,
		Size:         pageSize,
	})
	if err != nil {
		return fmt.Errorf("cash flow: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("   (no cash-flow records in this window)")
		return nil
	}
	fmt.Printf("%-20s %-8s %-7s %-13s %-5s %-12s %s\n",
		"BUSINESS_TIME", "DIRECTION", "TYPE", "BALANCE", "CCY", "SYMBOL", "DESCRIPTION")
	for _, r := range rows {
		fmt.Printf("%-20s %-8s %-7s %-13s %-5s %-12s %s\n",
			cli.OrDash(r.BusinessTime), directionName(r.Direction),
			balanceTypeName(r.BusinessType), cli.Dec(r.Balance),
			cli.OrDash(r.Currency), cli.OrDash(r.Symbol),
			cli.Truncate(r.Description, 40))
	}
	return nil
}

// ---------------------------------------------------------------- write path
//
// Exactly one guarded write, mirroring cmd/trade's shape: print, gate, then
// connect. It must not be reachable without all three switches.

func doWithdraw(ctx context.Context, cfg *appcfg.Config, connect func() (*trade.TradeContext, error)) error {
	if orderID == "" {
		return fmt.Errorf("-order-id is required for -action withdraw")
	}

	fmt.Printf("\n--- WithdrawOrder request ---\n  %-12s %s\n", "order_id", orderID)
	fmt.Println("  (WithdrawOrder is an alias of CancelOrder in the SDK: it closes an open order.)")

	// Same three-way gate as cmd/trade. This is an order write, not a watchlist
	// write, so it deliberately does NOT use GuardWatchlist.
	if !confirmLive {
		fmt.Fprintf(os.Stderr, "\nBLOCKED: missing --confirm-live\n")
		fmt.Fprintf(os.Stderr, "DRY RUN: nothing was sent to Longbridge.\n")
		return nil
	}
	if err := cfg.GuardWrite("withdraw an order"); err != nil {
		fmt.Fprintf(os.Stderr, "\nBLOCKED: %v\n", err)
		fmt.Fprintf(os.Stderr, "DRY RUN: nothing was sent to Longbridge.\n")
		return nil
	}

	tc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := tc.WithdrawOrder(c, orderID); err != nil {
		return fmt.Errorf("withdrawing order %s: %w", orderID, err)
	}
	fmt.Printf("order %s withdraw requested\n", orderID)
	return nil
}

// ------------------------------------------------------------------- helpers

// orZeroPrice lets -price default to 0, which is what a market order wants.
func orZeroPrice(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

func parseOrderType(s string) (trade.OrderType, error) {
	t := trade.OrderType(strings.ToUpper(strings.TrimSpace(s)))
	switch t {
	case trade.OrderTypeLO, trade.OrderTypeELO, trade.OrderTypeMO,
		trade.OrderTypeAO, trade.OrderTypeALO, trade.OrderTypeODD,
		trade.OrderTypeLIT, trade.OrderTypeMIT, trade.OrderTypeTSLPAMT,
		trade.OrderTypeTSLPPCT, trade.OrderTypeTSMAMT, trade.OrderTypeTSMPCT,
		trade.OrderTypeSLO:
		return t, nil
	}
	return "", fmt.Errorf("unknown -type %q", s)
}

func parseSide(s string) (trade.OrderSide, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy":
		return trade.OrderSideBuy, nil
	case "sell":
		return trade.OrderSideSell, nil
	}
	return "", fmt.Errorf("unknown -side %q: want Buy or Sell", s)
}

func parseBalanceType(s string) (trade.BalanceType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return trade.BalanceTypeUnknown, nil
	case "cash":
		return trade.BalanceTypeCash, nil
	case "stock":
		return trade.BalanceTypeStock, nil
	case "fund":
		return trade.BalanceTypeFund, nil
	}
	return 0, fmt.Errorf("unknown -balance-type %q: want cash, stock or fund", s)
}

// directionName and balanceTypeName map the two int32 enums in trade.CashFlow
// to names. Both are bare ints with no String() method, so printing them raw
// would tell the reader nothing.
func directionName(d trade.OfDirection) string {
	switch d {
	case trade.OfDirectionOut:
		return "out"
	case trade.OfDirectionIn:
		return "in"
	case trade.OfDirectionUnkown:
		return "unknown"
	}
	return fmt.Sprintf("unknown(%d)", int(d))
}

func balanceTypeName(b trade.BalanceType) string {
	switch b {
	case trade.BalanceTypeCash:
		return "cash"
	case trade.BalanceTypeStock:
		return "stock"
	case trade.BalanceTypeFund:
		return "fund"
	case trade.BalanceTypeUnknown:
		return "-"
	}
	return fmt.Sprintf("unknown(%d)", int(b))
}
