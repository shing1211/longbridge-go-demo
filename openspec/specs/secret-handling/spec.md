# secret-handling Specification

## Purpose

Three values in this project are sensitive enough that printing them would be a disclosure: the app secret, the access token, and — less severely but still deliberately — the app key. This capability fixes what each of them renders as in any printable summary, and it fixes one property that is easy to get wrong and hard to notice afterwards: the mask applied to a value is fixed-width, so the length of the secret is not readable off the output.

## Requirements

### Requirement: The app secret and the access token are never printed

The app secret and the access token SHALL NOT appear in any printable summary, in any form, and SHALL NOT be redacted rather than omitted. A redaction would still disclose length and shape; these two values are pure credentials with no diagnostic value.

#### Scenario: Neither secret appears in the startup summary

- **WHEN** a startup summary is produced with all three credentials loaded
- **THEN** the app secret and the access token appear nowhere in it

#### Scenario: The same holds in every mode

- **WHEN** a startup summary is produced in simulated mode, in live mode, in live mode with dry run still on, and with an unset mode
- **THEN** neither the app secret nor the access token appears in any of them

#### Scenario: A missing-credential report echoes no value

- **WHEN** a missing-credential report is printed
- **THEN** it names the absent variables and no credential values

#### Scenario: The credential help text marks only the two secrets

- **WHEN** the credential variable list is rendered for help output
- **THEN** the app secret and the access token are marked as secrets and the app key is not

### Requirement: The app key is rendered as a fixed-width mask

The app key SHALL be shown only as a mask whose width does not vary with the length of the key, so that a reader cannot infer the key's length from the output. A short key SHALL be masked in full rather than partly echoed.

#### Scenario: A long key renders at a fixed width

- **WHEN** the app key has more than ten characters
- **THEN** it renders as at most four leading characters, a fixed run of six mask characters and at most two trailing characters, and the rendered width does not grow with the key

#### Scenario: Two keys of very different lengths are indistinguishable

- **WHEN** an eleven-character key and a much longer key are both rendered
- **THEN** the two renderings are identical

#### Scenario: A short key is fully masked

- **WHEN** the app key has six characters or fewer
- **THEN** the whole key is replaced by mask characters and no part of it is echoed

#### Scenario: A mid-length key keeps only leading characters

- **WHEN** the app key has more than six and up to ten characters
- **THEN** at most the first two characters survive and the rest is masked

#### Scenario: A multibyte key is measured in characters

- **WHEN** the app key contains multi-byte characters
- **THEN** the mask boundaries are applied per character and no part of the key is echoed

### Requirement: An absent secret is distinguishable from a present one

The printable form of a secret SHALL distinguish "not set" from "set", so that a reader can tell a missing credential from a loaded one, and SHALL NOT disclose the value in either case.

#### Scenario: An unset secret renders as an unset marker

- **WHEN** a secret is empty
- **THEN** its printable form is the literal unset marker and no characters of a value are shown

#### Scenario: A set secret renders as a mask only

- **WHEN** a secret is non-empty
- **THEN** its printable form contains only mask characters and never any character of the secret

#### Scenario: Unset and set render differently

- **WHEN** the same secret is rendered once empty and once populated
- **THEN** the two renderings differ, and neither contains the populated value
