# verification-honesty Specification

## Purpose

No successful authenticated response from the Longbridge **live** API has ever been observed in this project: it was written without a working access token, so every request it has ever made against the live endpoints was rejected. That said, a **paper (simulated) account** was tested end-to-end and returned a genuine successful response — order ID `1289784973955432448` — confirming the write-path wiring is live. The paper account's successes are real API responses observed by this project. The live account's responses remain unobserved. That distinction governs how this project's documents may talk about behaviour at all, and this capability fixes the rule — an artifact describes what was observed and what was tested, and a rejected request proves only that a request was built, signed, delivered and refused.

## Requirements

### Requirement: No successful authenticated response from a live account has been observed

No successful authenticated response from the Longbridge **live** API SHALL be claimed, implied or reconstructed in any artifact of this project. Where an artifact describes what the live API returns, it SHALL say that the shape is taken from the interface contract and not from a live account.

A successful authenticated response from a **paper (simulated) account** MAY be claimed as an observed response, because the paper account is a real API endpoint returning genuine data. The write-path end-to-end verification (order ID `1289784973955432448`) is an example of such a claim.

#### Scenario: An artifact does not describe a success as observed

- **WHEN** an artifact describes a field, value or column of an API response
- **THEN** it does not present that field as something a live account has been observed to return

#### Scenario: An artifact does not promise an output a live run has not produced

- **WHEN** an artifact shows sample output for a request against the API
- **THEN** it does not present that output as a transcript from a successful call

#### Scenario: An artifact does not imply a valid live credential was used

- **WHEN** an artifact claims a command works end to end against a live account
- **THEN** it does not base that claim on a call that authenticated successfully against a live account

#### Scenario: A paper-account success may be claimed as an observed response

- **WHEN** an artifact describes a response from a paper account
- **THEN** it identifies the response as coming from the paper/simulated account
- **AND** it MAY be described as an observed API response, because the paper endpoint is a genuine API

#### Scenario: The write-path was verified end-to-end against a paper account

- **WHEN** the write-path end-to-end test is documented
- **THEN** it states the paper account order ID (`1289784973955432448`) as evidence
- **AND** it does not conflate the paper success with a live-account success

### Requirement: A locally computed output is not presented as observed data

An output a command derives locally from a value the response carried SHALL be described by where its value came from, and SHALL NOT be presented as a response observed from the brokerage or as a prediction of what the brokerage will return. Where the derivation is a pure function of one value, the artifact SHALL say so, and a check that proved it offline SHALL be described as offline.

#### Scenario: A computed block is labelled by its source

- **WHEN** an artifact shows a block of values a command computed from a single value in the response rather than received as fields
- **THEN** it identifies those values as computed from that value, and not as data returned by the brokerage

#### Scenario: A locally verified result is not upgraded to a live observation

- **WHEN** an artifact reports that a value or a rendering was verified by a test that made no request
- **THEN** it does not describe the result as observed live data, and does not count it among the checks that reached the API

#### Scenario: A pure function of a response value is stated to be one

- **WHEN** an artifact documents a value derived from a single response field by a function with no other input
- **THEN** it says the value is a local computation over that field, and does not imply the brokerage supplied or confirmed it

#### Scenario: A rejected request does not stand in for the absent observation

- **WHEN** the only execution evidence for a command is a rejected request
- **THEN** the artifact does not present the locally computed parts of that command's output as though a successful response had been seen

### Requirement: A rejected request proves only that a request was made

A `401` response SHALL be treated as evidence that a request was built, signed, delivered and rejected, and SHALL NOT be treated as evidence about the shape, width, optionality or completeness of a successful response.

#### Scenario: The claim made about a rejected request is limited to the request

- **WHEN** an artifact uses a `401` as evidence that a code path is genuinely wired
- **THEN** the artifact limits the claim to the request being built, signed, sent and rejected

#### Scenario: Nothing is claimed about a successful payload's shape

- **WHEN** an artifact uses a `401` as evidence for a path
- **THEN** it makes no claim about what a successful response would contain, including field widths, column widths or which fields are genuinely optional

#### Scenario: Unverified-by-observation details are labelled

- **WHEN** an artifact states a field name, order-state value or encoding that was not observed
- **THEN** it identifies the source of that value as the interface contract rather than a live account

### Requirement: Artifacts record refusals as refusals

An artifact SHALL report a refused write as a refusal — a non-zero exit status with nothing sent — and SHALL NOT report it as a failed attempt or an inconclusive result.

#### Scenario: A refusal is reported by its exit status

- **WHEN** an artifact records that a write was refused
- **THEN** it records the exit status the refusal produces and states that nothing was sent

#### Scenario: A guard that refused is not described as having tried

- **WHEN** an artifact describes a command whose guard refused
- **THEN** it does not describe the command as having attempted the write

#### Scenario: Counting refused combinations does not become counting attempts

- **WHEN** an artifact reports how many switch combinations refused
- **THEN** it does not report that many as attempts that were made and failed
