# exit-codes Specification

## Purpose

Every binary in this project terminates with one of exactly four exit statuses, and the status alone is enough for a wrapping script to tell a completed action from a declined one. This capability fixes what each status means and, just as importantly, what it does not mean: a refusal must never collapse into success, a missing credential must never look like a usage mistake, and a bad command-line argument must never be reported as an environment problem.

## Requirements

### Requirement: Four distinct exit statuses

Every binary SHALL exit with one of exactly four statuses, and no two meanings SHALL share a status: `0` success, `1` an ordinary failure, `2` missing credentials, `3` a safety refusal.

#### Scenario: Success exits 0

- **WHEN** a command completes without error
- **THEN** the process exits `0` and writes nothing to standard error

#### Scenario: An ordinary failure exits 1

- **WHEN** a command fails for a reason that is neither missing credentials nor a safety refusal, including a rejected API call
- **THEN** the process exits `1` and prints one line beginning with `error: ` to standard error

#### Scenario: Missing credentials exit 2

- **WHEN** a command is started with any required credential absent
- **THEN** the process exits `2` and the message names every absent credential at once

#### Scenario: A safety refusal exits 3

- **WHEN** a safety guard refuses a write
- **THEN** the process exits `3` and the message states that nothing was sent

#### Scenario: The four statuses are pairwise distinct

- **WHEN** success, an ordinary failure, missing credentials and a safety refusal are each produced
- **THEN** the four observed statuses are `0`, `1`, `2` and `3` respectively, with no two colliding

### Requirement: Missing credentials are the only source of exit 2

Exit `2` SHALL be produced by a missing-credential condition and by nothing else, so that a script reading `2` knows only that the environment needs fixing.

#### Scenario: Every absent credential is named in one report

- **WHEN** none of `LONGBRIDGE_APP_KEY`, `LONGBRIDGE_APP_SECRET` and `LONGBRIDGE_ACCESS_TOKEN` is resolvable
- **THEN** the process exits `2` and the single message lists all three variable names, not just the first one found

#### Scenario: A wrapped missing-credential error is still 2

- **WHEN** a missing-credential error is returned wrapped with extra context
- **THEN** the process exits `2` and the message retains the caller's context prefix

#### Scenario: A blank value counts as absent

- **WHEN** a credential variable is set to whitespace only
- **THEN** it is reported as missing and the process exits `2`

### Requirement: A bad flag value exits 1

A flag the command cannot use SHALL exit `1`, and SHALL NOT be reported as either a missing credential or a safety refusal.

#### Scenario: An unknown flag exits 1

- **WHEN** a command is invoked with a flag name it does not define
- **THEN** the process exits `1`

#### Scenario: An unusable flag value exits 1

- **WHEN** a command is invoked with a flag value it cannot use
- **THEN** the process exits `1`, not `2` and not `3`

#### Scenario: A present but blank string flag is a usage error

- **WHEN** a required string flag is supplied with a whitespace-only value
- **THEN** the command fails naming that flag, and exits `1` before any safety guard is consulted

### Requirement: A guard refusal exits 3

A safety guard that declines a write SHALL exit `3`, SHALL NOT exit `0`, and SHALL NOT exit `1`.

#### Scenario: The refusal is distinguishable from a completed write

- **WHEN** a guard refuses a write
- **THEN** the process exits `3`, so a wrapping script cannot confuse a declined write with a placed order

#### Scenario: A refusal with no message is still 3

- **WHEN** a guard refuses without supplying a reason
- **THEN** the process still exits `3`

#### Scenario: A refusal survives being wrapped for context

- **WHEN** a guard refusal is returned wrapped with extra context
- **THEN** the process still exits `3` and the message includes both the context and the reason

#### Scenario: Text that merely mentions a refusal is still 1

- **WHEN** an ordinary failure's message happens to contain the same words a refusal would use
- **THEN** the process exits `1`, because the status is decided by what kind of failure it is and not by its text
