# sdk-coverage Specification

## Purpose

The point of a demonstration built on someone else's client library is to have used all of it, and an untested corner is where a real integration bug hides. This capability fixes that every exported method of the interface library is referenced from this project's command code, and — just as important — that the check which enforces this proves only reference, never live behaviour, so nobody reads a green result as a claim about the API.

## Requirements

### Requirement: Every exported context method is referenced from command code

Every exported method of every context type the interface library exposes SHALL be referenced from code under the command directory. Twelve context types are in scope and all 153 of their exported methods are referenced.

#### Scenario: A method with no call site fails the check

- **WHEN** an exported context method has no reference anywhere under the command directory
- **THEN** the check fails and names the uncovered method

#### Scenario: The check spans every context type

- **WHEN** the check enumerates the exported methods
- **THEN** it enumerates methods of all twelve context types, including the one whose methods are declared on a differently named receiver than the rest

#### Scenario: A new context method is detected

- **WHEN** the interface library gains an exported context method
- **THEN** the check fails until the new method is referenced from command code

#### Scenario: The current state is complete

- **WHEN** the check runs against the interface library version this project depends on
- **THEN** it reports all 153 methods covered and none uncovered

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

### Requirement: The coverage check is a build target

The coverage check SHALL be reachable as a named build target, and SHALL be part of the aggregate verification target, so that a coverage regression is caught by the same command that catches a test or build regression.

#### Scenario: A coverage regression fails the build

- **WHEN** a method loses its last reference and the coverage target is run
- **THEN** the target exits non-zero and prints the uncovered method

#### Scenario: The aggregate verification target includes it

- **WHEN** the aggregate verification target is listed
- **THEN** the coverage target appears among its prerequisites
