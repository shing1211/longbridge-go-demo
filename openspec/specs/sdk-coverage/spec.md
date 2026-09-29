# sdk-coverage Specification

## Purpose

The point of a demonstration built on someone else's client library is to have used all of it, and an untested corner is where a real integration bug hides. This capability fixes that every exported method of the interface library is referenced from this project's command code, and — just as important — that the check which enforces this proves only reference, never live behaviour, so nobody reads a green result as a claim about the API.

## Requirements

### Requirement: Every exported method on an exported type is referenced or justified

Every exported method declared on an exported type the interface library exposes SHALL either be referenced from code under the command or shared-internal directories or carry exactly one allow-list entry. The check SHALL span the whole exported surface, not only the command-context types, and the scanned total SHALL be 206 type-and-method pairs.

#### Scenario: A method with no call site fails the check

- **WHEN** an exported method has no reference anywhere under the command or shared-internal directories and no allow-list entry
- **THEN** the check fails and names the method as neither referenced nor allow-listed

#### Scenario: The check spans every exported type

- **WHEN** the check enumerates the exported methods
- **THEN** it enumerates methods of every exported type, including the twelve command-context types, the bare status and mode enums, the configuration and token types, the error type, and the request types whose only method returns their own fields

#### Scenario: A new context method is detected

- **WHEN** the interface library gains an exported method on an exported type
- **THEN** the check fails until the new method is referenced from command code or allow-listed with a reason

#### Scenario: A method that gains a call site fails the check

- **WHEN** an allow-listed method gains a reference from command code
- **THEN** the check fails and reports the method as now covered so that its allow-list entry is deleted

#### Scenario: The allow-list is an exact set

- **WHEN** an allow-list entry names a method the check did not scan, or carries a reason word outside the fixed vocabulary
- **THEN** the check fails and names the offending entry, so the list cannot rot into a standing exemption

#### Scenario: The current state is complete

- **WHEN** the check runs against the interface library version this project depends on
- **THEN** it reports 184 of the 206 methods referenced from command or shared-internal code, 22 justified on the allow-list, and none in neither state

### Requirement: The scan is receiver-name-agnostic and the total is pinned

The check SHALL identify a method without depending on the identifier a receiver happens to be named, and SHALL accept a method declared on a value as readily as on a pointer. The scanned total SHALL be compared against a pinned count of 206 and the check SHALL fail when the two differ, so a pattern that stops matching a class of method fails instead of quietly shrinking the denominator.

#### Scenario: A receiver named something unexpected is still counted

- **WHEN** a method is declared on a receiver whose identifier is not the one most methods use, including a receiver that is left unnamed
- **THEN** it still appears in the scanned total

#### Scenario: A value-receiver method is still counted

- **WHEN** a method is declared on a value rather than a pointer
- **THEN** it still appears in the scanned total

#### Scenario: A pattern that counts too few methods fails

- **WHEN** the check scans a total other than 206
- **THEN** it fails and reports the expected and the scanned total, naming a moved library version or a regressed pattern as the two possible causes

### Requirement: An allow-list entry states one reason from a fixed vocabulary

Every allow-list entry SHALL name exactly one reason, and the reason SHALL be one of `internal`, `unimportable` or `not-used`. The current 22 entries SHALL consist of 19 `internal`, 1 `unimportable` and 2 `not-used`.

#### Scenario: A reason word outside the vocabulary is refused

- **WHEN** an allow-list entry carries a reason word that is not `internal`, `unimportable` or `not-used`
- **THEN** the check fails and prints the three accepted reason words

#### Scenario: The three reasons stay distinct

- **WHEN** an allow-list entry is justified
- **THEN** its reason distinguishes one that a higher-level command already reaches indirectly, one that cannot be imported at all by this project, and one that is importable and user-facing but deliberately not reached

### Requirement: The coverage check is static only

The check SHALL be a static reference search and SHALL make no network request. It proves a method is referenced from command code; it does not prove the method was exercised against a live account, and no artifact may state that it does.

#### Scenario: The check runs with no credentials and no network

- **WHEN** the check runs on a machine with no credentials configured
- **THEN** it completes and reports the same result it would report with credentials present

#### Scenario: A reference is not evidence of a live call

- **WHEN** an artifact reports a covered method
- **THEN** it does not claim the method was exercised against a live account

#### Scenario: Coverage is not described as end-to-end validation

- **WHEN** an artifact states the coverage figure
- **THEN** it does not present that figure as evidence that the corresponding API surface works

#### Scenario: A pass is not described as receiver-accurate

- **WHEN** an artifact reports the check as passing
- **THEN** it describes the result as no method being both unreferenced and unjustified, and does not claim each method was called on its own type

### Requirement: The status section prints the trade-status predicates

The market-status section of the market-data command SHALL print, for each market it reports, a block of trade-status predicate results in addition to the status table, and that block SHALL be derived only from the status value the response carried. A market for which the response carried no status SHALL be printed as an absent status with no predicate result.

#### Scenario: The predicate block lists the predicates for a reported status

- **WHEN** the command prints a market whose status the response supplied
- **THEN** it prints the status code and name, a separate block for the delayed status, and one line per trade-status predicate grouped by the question each predicate answers

#### Scenario: An absent status prints no predicate

- **WHEN** the response supplies no status for a market
- **THEN** the command prints that no status was supplied together with the code, and prints no predicate value for that market

#### Scenario: The predicates are verified without a request

- **WHEN** the test suite asserts what each trade-status predicate returns
- **THEN** it does so from statuses built locally, with no client, no fixture and no network request, and without any observed market data

#### Scenario: The predicate rendering is not presented as an observed response

- **WHEN** an artifact shows the predicate block
- **THEN** it identifies the values as computed from a status value rather than as a response observed from the brokerage

### Requirement: The coverage check is a build target

The coverage check SHALL be reachable as a named build target, and SHALL be part of the aggregate verification target, so that a coverage regression is caught by the same command that catches a test or build regression.

#### Scenario: A coverage regression fails the build

- **WHEN** a method loses its last reference and the coverage target is run
- **THEN** the target exits non-zero and prints the uncovered method

#### Scenario: The aggregate verification target includes it

- **WHEN** the aggregate verification target is listed
- **THEN** the coverage target appears among its prerequisites
