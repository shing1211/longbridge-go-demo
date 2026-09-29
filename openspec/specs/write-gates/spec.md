# write-gates Specification

## Purpose

Six kinds of mutation are reachable from this project's binaries — order placement and cancellation, watchlist group editing, recurring investment plans, price alerts, share lists, and content publishing — and each is protected by its own gate with its own default and its own switch. This capability fixes the exact conditions under which each gate opens, that a refusal is silent about nothing, and that a refused write never creates a client, opens a connection or issues a request.

## Requirements

### Requirement: The order gate requires dry run off, an explicit flag, and live mode

Order writes SHALL be sent only when `LONGPORT_DRY_RUN=0` (or `false`), `--confirm-live` is passed, and `LONGPORT_MODE=live`. All three are required and none alone is sufficient. A refusal SHALL exit `3`.

#### Scenario: The default refuses an order write

- **WHEN** an order write is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that no order was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm-live` is passed while `LONGPORT_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_DRY_RUN=0` is set while `--confirm-live` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm-live`

#### Scenario: Both switches without live mode still refuses

- **WHEN** `LONGPORT_DRY_RUN=0` and `--confirm-live` are both set but `LONGPORT_MODE` is not `live`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_MODE=live`

#### Scenario: All three switches open the gate

- **WHEN** `LONGPORT_DRY_RUN=0`, `--confirm-live` and `LONGPORT_MODE=live` are all in effect
- **THEN** the gate is open and the write is authorised

#### Scenario: A refused order write creates no client

- **WHEN** an order write is refused by the gate
- **THEN** no client is constructed and no request is issued, and the request that would have been sent is printed instead

### Requirement: The watchlist gate requires dry run off and an explicit flag

Watchlist group writes SHALL be sent only when `LONGPORT_WATCHLIST_DRY_RUN=0` (or `false`) and `--confirm` is passed. `LONGPORT_MODE=live` SHALL NOT be required. A refusal SHALL exit `3`. Listing the saved groups SHALL NOT be gated by either switch: it SHALL return its normal result whatever state the gate is in.

#### Scenario: The default refuses a watchlist write

- **WHEN** a watchlist write is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that no change was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm` is passed while `LONGPORT_WATCHLIST_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_WATCHLIST_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_WATCHLIST_DRY_RUN=0` is set while `--confirm` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm`

#### Scenario: Both switches open the gate in either mode

- **WHEN** `LONGPORT_WATCHLIST_DRY_RUN=0` and `--confirm` are both set, in either mode
- **THEN** the gate is open and the write is authorised

#### Scenario: A refused watchlist write creates no client

- **WHEN** a watchlist write is refused by the gate
- **THEN** no client is constructed and no request is issued, and the request that would have been sent is printed instead

#### Scenario: Listing the saved groups is not gated

- **WHEN** `-action list` is run with `LONGPORT_WATCHLIST_DRY_RUN` unset, `1`, `0` or `false`, and with or without `--confirm`
- **THEN** the read proceeds and reports the saved groups as usual, no refusal is printed, and the process does not exit `3`

### Requirement: The recurring investment gate requires dry run off, an explicit flag, and live mode

Recurring investment plan writes SHALL be sent only when `LONGPORT_DCA_DRY_RUN=0` (or `false`), `--confirm-live-dca` is passed, and `LONGPORT_MODE=live`. A refusal SHALL exit `3`.

#### Scenario: The default refuses a plan write

- **WHEN** a plan write is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that nothing was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm-live-dca` is passed while `LONGPORT_DCA_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_DCA_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_DCA_DRY_RUN=0` is set while `--confirm-live-dca` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm-live-dca`

#### Scenario: Both switches without live mode still refuses

- **WHEN** `LONGPORT_DCA_DRY_RUN=0` and `--confirm-live-dca` are both set but `LONGPORT_MODE` is not `live`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_MODE=live`

#### Scenario: All three switches open the gate

- **WHEN** `LONGPORT_DCA_DRY_RUN=0`, `--confirm-live-dca` and `LONGPORT_MODE=live` are all in effect
- **THEN** the gate is open and the write is authorised

### Requirement: The price alert gate requires dry run off, an explicit flag, and live mode

Price alert writes SHALL be sent only when `LONGPORT_ALERT_DRY_RUN=0` (or `false`), `--confirm-live-alert` is passed, and `LONGPORT_MODE=live`. A refusal SHALL exit `3`.

#### Scenario: The default refuses an alert write

- **WHEN** an alert write is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that nothing was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm-live-alert` is passed while `LONGPORT_ALERT_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_ALERT_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_ALERT_DRY_RUN=0` is set while `--confirm-live-alert` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm-live-alert`

#### Scenario: Both switches without live mode still refuses

- **WHEN** `LONGPORT_ALERT_DRY_RUN=0` and `--confirm-live-alert` are both set but `LONGPORT_MODE` is not `live`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_MODE=live`

#### Scenario: All three switches open the gate

- **WHEN** `LONGPORT_ALERT_DRY_RUN=0`, `--confirm-live-alert` and `LONGPORT_MODE=live` are all in effect
- **THEN** the gate is open and the write is authorised

### Requirement: The share list gate requires dry run off, an explicit flag, and live mode

Share list writes SHALL be sent only when `LONGPORT_SHARELIST_DRY_RUN=0` (or `false`), `--confirm-live-sharelist` is passed, and `LONGPORT_MODE=live`. A refusal SHALL exit `3`.

#### Scenario: The default refuses a share list write

- **WHEN** a share list write is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that nothing was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm-live-sharelist` is passed while `LONGPORT_SHARELIST_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_SHARELIST_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_SHARELIST_DRY_RUN=0` is set while `--confirm-live-sharelist` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm-live-sharelist`

#### Scenario: Both switches without live mode still refuses

- **WHEN** `LONGPORT_SHARELIST_DRY_RUN=0` and `--confirm-live-sharelist` are both set but `LONGPORT_MODE` is not `live`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_MODE=live`

#### Scenario: All three switches open the gate

- **WHEN** `LONGPORT_SHARELIST_DRY_RUN=0`, `--confirm-live-sharelist` and `LONGPORT_MODE=live` are all in effect
- **THEN** the gate is open and the write is authorised

### Requirement: The content publishing gate requires dry run off, an explicit flag, and live mode

Content publishing SHALL be sent only when `LONGPORT_CONTENT_DRY_RUN=0` (or `false`), `--confirm-live-content` is passed, and `LONGPORT_MODE=live`. A refusal SHALL exit `3`.

#### Scenario: The default refuses a publish

- **WHEN** a publish is attempted with no switches set
- **THEN** it is refused, the process exits `3`, and the message states that nothing was sent

#### Scenario: The flag alone refuses

- **WHEN** `--confirm-live-content` is passed while `LONGPORT_CONTENT_DRY_RUN` is unset or `1`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_CONTENT_DRY_RUN`

#### Scenario: Dry run off alone refuses

- **WHEN** `LONGPORT_CONTENT_DRY_RUN=0` is set while `--confirm-live-content` is absent
- **THEN** the write is refused, the process exits `3`, and the message names `--confirm-live-content`

#### Scenario: Both switches without live mode still refuses

- **WHEN** `LONGPORT_CONTENT_DRY_RUN=0` and `--confirm-live-content` are both set but `LONGPORT_MODE` is not `live`
- **THEN** the write is refused, the process exits `3`, and the message names `LONGPORT_MODE=live`

#### Scenario: All three switches open the gate

- **WHEN** `LONGPORT_CONTENT_DRY_RUN=0`, `--confirm-live-content` and `LONGPORT_MODE=live` are all in effect
- **THEN** the gate is open and the write is authorised

### Requirement: The six gates are independent and default to closed

Each of the six gates SHALL be refused by its own switch alone, SHALL default to closed when its switch is unset or cannot be parsed, and SHALL NOT be opened by another gate's switch. An unusable value for any of the six switches SHALL be reported at startup rather than being treated as closed silently.

#### Scenario: No gate is open by default

- **WHEN** the process is started with no gate switch set
- **THEN** all six gates are closed

#### Scenario: An unset switch means closed

- **WHEN** a gate's dry-run variable is absent from the environment
- **THEN** that gate is closed, because an absent opt-in is not consent

#### Scenario: An unparseable switch value means closed

- **WHEN** a gate's dry-run variable is set to a value that is not a recognised boolean literal
- **THEN** that gate is closed rather than being read as opted in

#### Scenario: Only an exact falsy literal opens a dry-run switch

- **WHEN** a gate's dry-run variable is set to a recognised boolean
- **THEN** only `0` and `false` open the switch, and `1` and `true` leave it closed

#### Scenario: One gate's switch does not open another gate

- **WHEN** every other gate's dry-run variable is set to `0`
- **THEN** the remaining gate is still closed

#### Scenario: Two gates never share a switch

- **WHEN** the six gates' switches are enumerated
- **THEN** no two gates share a dry-run variable and no two share a confirmation flag

#### Scenario: An unusable switch value is reported at startup

- **WHEN** any of the six dry-run variables is set to a value that is not a recognised boolean literal
- **THEN** the command fails during startup naming that variable, and no command is run
