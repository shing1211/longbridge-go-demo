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

### Requirement: A header value is masked according to what its name says it is

A header sent with every request SHALL be rendered in the startup summary masked whenever its name contains `token`, `secret`, `key`, `auth`, `pass`, `credential` or `cookie`, case-insensitively, and SHALL be rendered in full otherwise. The masked form SHALL be the same fixed-width mask used for the app key, and SHALL never be the whole value. The name SHALL be shown beside the value, so that a reader can tell which values were shortened.

#### Scenario: A credential-sounding name is masked

- **WHEN** the summary is produced with a header named `x-access-token`
- **THEN** its value appears only as a mask and no part of the whole value appears

#### Scenario: A plain name is shown in full

- **WHEN** the summary is produced with a header named `x-trace-id`
- **THEN** its value appears unchanged

#### Scenario: The decision follows the name alone

- **WHEN** two headers carry values of identical shape, one named with a credential fragment and one without
- **THEN** only the one whose name carries a fragment is masked

#### Scenario: A name that merely contains a fragment is masked too

- **WHEN** a header is named `x-passenger` or `x-keynote`
- **THEN** its value is masked, because the fragment appears in the name

#### Scenario: A header with no value is distinguishable from one with a value

- **WHEN** a header is supplied with an empty value
- **THEN** its printable form is the literal unset marker

#### Scenario: The source of a header is reported

- **WHEN** the summary lists a header
- **THEN** it names the flag, variable or file that supplied it

### Requirement: A refusal about a header never prints its value

A failure caused by a header SHALL name the header and SHALL NOT print its value, in any source and for any reason, including a malformed argument, an unusable name and a refused credential header.

#### Scenario: A malformed argument is refused without its value

- **WHEN** a `-header` argument has no `=`
- **THEN** the failure explains the expected form and prints neither the argument nor any part of it

#### Scenario: A refused credential header prints nothing of its value

- **WHEN** `-header authorization=something` is refused
- **THEN** the failure names the header and the string given as its value appears nowhere in the output

#### Scenario: An environment header is refused without its value

- **WHEN** a variable with the header prefix is refused
- **THEN** the failure names the variable and prints no part of its value

#### Scenario: The diagnostic goes to standard error

- **WHEN** a header is refused
- **THEN** the report is written to standard error and nothing is written to standard output
