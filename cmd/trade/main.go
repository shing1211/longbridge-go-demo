// Command trade inspects the account and, behind a hard safety gate, places,
// modifies or cancels orders.
//
// SAFETY CONTRACT — read before touching any write path:
//
//   - The default is DRY RUN. Nothing is sent to the exchange.
//   - A write happens only when BOTH of these are true:
//     LONGPORT_DRY_RUN=0   (or false)
//     --confirm-live       passed on the command line
//   - LONGPORT_MODE must also be "live", so a simulated account can never
//     receive an order even if dry run is switched off by accident.
//   - Every write is passed through Config.GuardWrite immediately before the
//     SDK call, and the dry-run path prints the exact request it would send.
//
// Read-only subcommands (balance, positions, orders) never consult the gate.
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
	quantity    uint64
	price       string
	side        string
	orderType   string
	timeInForce string
	remark      string
	historyDays int
	confirmLive bool
	timeout     time.Duration
)

func main() {
	u := cli.NewUsage("trade", "account inspection plus a gated order workflow")
	u.FS.StringVar(&action, "action", "balance",
		"balance | positions | today-orders | history-orders | submit | replace | cancel")
	u.FS.StringVar(&symbol, "symbol", "", "symbol, e.g. 700.HK (required for submit)")
	u.FS.StringVar(&orderID, "order-id", "", "order id (required for replace and cancel)")
	u.FS.Uint64Var(&quantity, "qty", 0, "order quantity in shares (required for submit and replace)")
	u.FS.StringVar(&price, "price", "", "limit price as a decimal string (required for submit and replace)")
	u.FS.StringVar(&side, "side", "Buy", "Buy or Sell")
	u.FS.StringVar(&orderType, "type", "LO", "order type: LO, ELO, MO, AO, ALO, ODD, LIT, MIT, ...")
	u.FS.StringVar(&timeInForce, "tif", "Day", "time in force: Day, GTC, GTD")
	u.FS.StringVar(&remark, "remark", "", "free-text order remark")
	u.FS.IntVar(&historyDays, "days", 7, "lookback window in days for history-orders")
	u.FS.BoolVar(&confirmLive, "confirm-live", false,
		"REQUIRED acknowledgement for any write; must be combined with LONGPORT_DRY_RUN=0")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)

	cli.Run(func(ctx context.Context) error {
		// The trade context is created lazily: a dry run must not open a
		// websocket or touch the network, otherwise "dry" would still make a
		// real authenticated round trip.
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
		case "balance":
			c, err := connect()
			if err != nil {
				return err
			}
			return printBalance(ctx, c)
		case "positions":
			c, err := connect()
			if err != nil {
				return err
			}
			return printPositions(ctx, c)
		case "today-orders":
			c, err := connect()
			if err != nil {
				return err
			}
			return printTodayOrders(ctx, c)
		case "history-orders":
			c, err := connect()
			if err != nil {
				return err
			}
			return printHistoryOrders(ctx, c)
		case "submit":
			return doSubmit(ctx, cfg, connect)
		case "replace":
			return doReplace(ctx, cfg, connect)
		case "cancel":
			return doCancel(ctx, cfg, connect)
		default:
			return fmt.Errorf(
				"unknown -action %q: want balance, positions, today-orders, "+
					"history-orders, submit, replace or cancel", action)
		}
	})
}

// ---------------------------------------------------------------- read paths

func printBalance(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Println("=== Account balance ===")
	accounts, err := tc.AccountBalance(c, &trade.GetAccountBalance{})
	if err != nil {
		return fmt.Errorf("account balance: %w", err)
	}
	for _, a := range accounts {
		fmt.Printf("currency=%s total_cash=%s net_assets=%s buy_power=%s risk_level=%s\n",
			a.Currency, dec(a.TotalCash), dec(a.NetAssets), dec(a.BuyPower), a.RiskLevel)
		fmt.Printf("  margin: init=%s maintenance=%s finance_remaining=%s margin_call=%s\n",
			dec(a.InitMargin), dec(a.MaintenanceMargin),
			dec(a.RemainingFinanceAmount), dec(a.MarginCall))
		if len(a.CashInfos) > 0 {
			fmt.Println("  cash breakdown:")
			for _, ci := range a.CashInfos {
				fmt.Printf("    %-6s available=%-14s withdraw=%-14s frozen=%-12s settling=%s\n",
					ci.Currency, dec(ci.AvailableCash), dec(ci.WithdrawCash),
					dec(ci.FrozenCash), dec(ci.SettlingCash))
			}
		}
	}
	return nil
}

func printPositions(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var syms []string
	if symbol != "" {
		syms = []string{symbol}
	}

	fmt.Println("=== Stock positions ===")
	channels, err := tc.StockPositions(c, syms)
	if err != nil {
		return fmt.Errorf("stock positions: %w", err)
	}
	total := 0
	for _, ch := range channels {
		fmt.Printf("-- channel %s\n", ch.AccountChannel)
		for _, p := range ch.Positions {
			total++
			fmt.Printf("   %-12s %-24s qty=%-10s available=%-10s cost=%-12s %s\n",
				p.Symbol, truncate(p.SymbolName, 24), p.Quantity,
				p.AvailableQuantity, dec(p.CostPrice), p.Market)
		}
	}
	if total == 0 {
		fmt.Println("   (no open positions)")
	}
	return nil
}

func printTodayOrders(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Println("=== Today's orders ===")
	orders, err := tc.TodayOrders(c, &trade.GetTodayOrders{})
	if err != nil {
		return fmt.Errorf("today orders: %w", err)
	}
	printOrders(orders)
	return nil
}

func printHistoryOrders(ctx context.Context, tc *trade.TradeContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	end := time.Now()
	start := end.AddDate(0, 0, -historyDays)

	fmt.Printf("=== Orders from %s to %s ===\n",
		start.Format("2006-01-02"), end.Format("2006-01-02"))
	orders, hasMore, err := tc.HistoryOrders(c, &trade.GetHistoryOrders{
		StartAt: start.Unix(),
		EndAt:   end.Unix(),
	})
	if err != nil {
		return fmt.Errorf("history orders: %w", err)
	}
	printOrders(orders)
	if hasMore {
		fmt.Println("(more results available on the server; narrow -days to page through)")
	}
	return nil
}

func printOrders(orders []*trade.Order) {
	if len(orders) == 0 {
		fmt.Println("   (no orders)")
		return
	}
	fmt.Printf("%-22s %-12s %-6s %-8s %-9s %-10s %-10s %-20s %s\n",
		"ORDER_ID", "SYMBOL", "SIDE", "TYPE", "STATUS", "PRICE", "QTY", "SUBMITTED", "REMARK")
	for _, o := range orders {
		fmt.Printf("%-22s %-12s %-6s %-8s %-9s %-10s %-10s %-20s %s\n",
			o.OrderId, o.Symbol, o.Side, o.OrderType, o.Status,
			dec(o.Price), o.Quantity, o.SubmittedAt, o.Remark)
		if o.Msg != "" {
			fmt.Printf("    message: %s\n", o.Msg)
		}
	}
}

// ---------------------------------------------------------------- write paths
//
// Each of these is preceded by exactly one guard call and one dry-run branch.
// If you add a new write operation, copy this shape verbatim.

func doSubmit(ctx context.Context, cfg *appcfg.Config, connect func() (*trade.TradeContext, error)) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action submit")
	}
	if quantity == 0 {
		return fmt.Errorf("-qty must be greater than 0 for -action submit")
	}
	ot, err := parseOrderType(orderType)
	if err != nil {
		return err
	}
	os_, err := parseSide(side)
	if err != nil {
		return err
	}
	tif, err := parseTimeInForce(timeInForce)
	if err != nil {
		return err
	}
	px := decimal.Zero
	if price != "" {
		px, err = decimal.NewFromString(price)
		if err != nil {
			return fmt.Errorf("parsing -price %q: %w", price, err)
		}
	}

	order := &trade.SubmitOrder{
		Symbol:            symbol,
		OrderType:         ot,
		Side:              os_,
		SubmittedQuantity: quantity,
		SubmittedPrice:    px,
		TimeInForce:       tif,
		Remark:            remark,
	}

	// Print what we intend to send BEFORE the guard, so a dry run is
	// informative rather than just a refusal.
	describe("SubmitOrder", map[string]string{
		"symbol":     order.Symbol,
		"order_type": string(order.OrderType),
		"side":       string(order.Side),
		"quantity":   fmt.Sprint(order.SubmittedQuantity),
		"price":      order.SubmittedPrice.String(),
		"time_force": string(order.TimeInForce),
		"remark":     order.Remark,
	})

	if !gate(cfg, confirmLive, "submit an order") {
		return nil
	}

	tc, err := connect()
	if err != nil {
		return err
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := tc.SubmitOrder(c, order)
	if err != nil {
		return fmt.Errorf("submitting order for %s: %w", symbol, err)
	}
	fmt.Printf("order accepted, order_id=%s\n", id)
	return nil
}

func doReplace(ctx context.Context, cfg *appcfg.Config, connect func() (*trade.TradeContext, error)) error {
	if orderID == "" {
		return fmt.Errorf("-order-id is required for -action replace")
	}
	if quantity == 0 {
		return fmt.Errorf("-qty must be greater than 0 for -action replace")
	}
	px := decimal.Zero
	if price != "" {
		var err error
		px, err = decimal.NewFromString(price)
		if err != nil {
			return fmt.Errorf("parsing -price %q: %w", price, err)
		}
	}

	replace := &trade.ReplaceOrder{
		OrderId:  orderID,
		Quantity: quantity,
		Price:    px,
		Remark:   remark,
	}

	describe("ReplaceOrder", map[string]string{
		"order_id": replace.OrderId,
		"quantity": fmt.Sprint(replace.Quantity),
		"price":    replace.Price.String(),
		"remark":   replace.Remark,
	})

	if !gate(cfg, confirmLive, "replace an order") {
		return nil
	}

	tc, err := connect()
	if err != nil {
		return err
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := tc.ReplaceOrder(c, replace); err != nil {
		return fmt.Errorf("replacing order %s: %w", orderID, err)
	}
	fmt.Printf("order %s replaced\n", orderID)
	return nil
}

func doCancel(ctx context.Context, cfg *appcfg.Config, connect func() (*trade.TradeContext, error)) error {
	if orderID == "" {
		return fmt.Errorf("-order-id is required for -action cancel")
	}

	describe("CancelOrder", map[string]string{"order_id": orderID})

	if !gate(cfg, confirmLive, "cancel an order") {
		return nil
	}

	tc, err := connect()
	if err != nil {
		return err
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := tc.CancelOrder(c, orderID); err != nil {
		return fmt.Errorf("cancelling order %s: %w", orderID, err)
	}
	fmt.Printf("order %s cancel requested\n", orderID)
	return nil
}

// guard is the single choke point for writes. It returns nil only when a real
// submission is authorised; any error must be treated as "do not call the SDK".
func guard(cfg *appcfg.Config, confirmed bool, action string) error {
	if !confirmed {
		return fmt.Errorf("missing --confirm-live")
	}
	return cfg.GuardWrite(action)
}

// gate prints the request, evaluates the guard, and reports why a write was
// blocked. It returns true when the caller may proceed.
//
// The reason is printed to stderr on purpose: a silent refusal looks like a
// crash, and "nothing happened" is exactly the message a safety gate must not
// leave unexplained.
func gate(cfg *appcfg.Config, confirmed bool, action string) bool {
	if err := guard(cfg, confirmed, action); err != nil {
		fmt.Fprintf(os.Stderr, "\nBLOCKED: %v\n", err)
		fmt.Fprintf(os.Stderr, "DRY RUN: nothing was sent to Longbridge.\n")
		return false
	}
	return true
}

func describe(op string, fields map[string]string) {
	fmt.Printf("\n--- %s request ---\n", op)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	// Deterministic output for diffing and review.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		fmt.Printf("  %-12s %s\n", k, fields[k])
	}
}

// ------------------------------------------------------------------- helpers

func parseOrderType(s string) (trade.OrderType, error) {
	// OrderType values are already upper-case in the SDK (LO, ELO, ...).
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

// parseTimeInForce maps the user-facing value to the SDK's TimeType. The SDK
// constants are mixed case ("Day", "GTC", "GTD"), so compare case-insensitively
// but return the canonical SDK value.
func parseTimeInForce(s string) (trade.TimeType, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DAY":
		return trade.TimeTypeDay, nil
	case "GTC":
		return trade.TimeTypeGTC, nil
	case "GTD":
		return trade.TimeTypeGTD, nil
	}
	return "", fmt.Errorf("unknown -tif %q: want Day, GTC or GTD", s)
}

func dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(2)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
