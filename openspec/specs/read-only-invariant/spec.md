# read-only-invariant Specification

## Purpose

Eight of the binaries in this project issue no writes at all, and an open order gate in one of them is never a legitimate state — there is no write for the gate to permit. This capability fixes that those binaries assert the order gate is closed before they do anything, that the assertion is present in every one of them rather than inherited, and that a violation is reported as the misconfiguration it is instead of being dressed up as a safety refusal.

## Requirements

### Requirement: Eight read-only binaries assert the order gate is closed at startup

The binaries `quote`, `watch`, `warrant`, `reference`, `portfolio`, `fundamentals`, `market` and `screener` SHALL each assert at startup that the order gate is still closed, and SHALL each carry that assertion exactly once, as a top-level step of their own startup rather than nested inside the work they perform.

#### Scenario: All eight carry the assertion

- **WHEN** the sources of the eight read-only binaries are inspected
- **THEN** each contains the assertion exactly once, in each case as a top-level step and not inside the block that does the work

#### Scenario: The assertion runs after the state is shown and before any request

- **WHEN** a read-only binary starts with the order gate open
- **THEN** the configuration summary is printed before the assertion fails, so the mode and dry-run state that tripped it are visible

#### Scenario: Each binary asserts on the configuration it just loaded

- **WHEN** the assertion is inspected in any of the eight binaries
- **THEN** it is evaluated against the loaded configuration, not against a hand-built stand-in

#### Scenario: Each binary reports its own name

- **WHEN** the assertion fails in any of the eight binaries
- **THEN** the message names that binary, because the name is what the reader sees

#### Scenario: Each binary declares a distinct action phrase

- **WHEN** the action phrase each binary passes to the assertion is enumerated
- **THEN** the eight phrases are all distinct, because the phrase is the text that would have been printed had a write been attempted

#### Scenario: The assertion is visible in the source

- **WHEN** the source of any of the eight binaries is read
- **THEN** a comment directly above the assertion states that the check exists for safety, so a reader learns why a reader checks a write gate

### Requirement: Every command directory is classified as read-only or writing

Every binary in this project SHALL be recorded as either issuing no writes or issuing writes, so that a new binary cannot be added without someone stating which kind it is, and a recorded entry cannot drift from the directories that exist.

#### Scenario: A new unclassified binary fails the check

- **WHEN** a command directory exists that is recorded in neither list
- **THEN** the check fails and names the unclassified directory

#### Scenario: A recorded entry with no directory fails the check

- **WHEN** a list names a binary whose directory no longer exists
- **THEN** the check fails, so a stale entry cannot silently stop covering a real binary

#### Scenario: A writing binary does not carry the read-only assertion

- **WHEN** a binary that issues writes carries the read-only assertion
- **THEN** the check fails, because such a binary has to be able to run with the order gate open

### Requirement: An open order gate in a read-only binary exits 1

A read-only binary that finds the order gate open SHALL exit `1` and SHALL NOT exit `3`: nothing was refused, nothing was sent, and nothing was attempted, so this is a misconfiguration and not a safety refusal.

#### Scenario: The open-gate misconfiguration exits 1

- **WHEN** a read-only binary is started with the order gate open
- **THEN** the process exits `1`

#### Scenario: The message says the order gate is open

- **WHEN** a read-only binary refuses to start
- **THEN** it prints one line beginning with `error: ` naming the binary and stating that it is read-only but the order gate is open

#### Scenario: The report is on standard error only

- **WHEN** a read-only binary refuses to start
- **THEN** the report is written to standard error and nothing is written to standard output

#### Scenario: The exit status is identical for all eight

- **WHEN** each of the eight read-only binaries is started with the order gate open
- **THEN** each exits `1` and each prints exactly the same shape of message with its own name substituted
