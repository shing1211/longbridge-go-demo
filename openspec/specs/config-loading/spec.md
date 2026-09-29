# config-loading Specification

## Purpose

Credentials reach this project from a process environment and, optionally, from one YAML file. This capability fixes the precedence between those sources, exactly which file names are considered when no file is named, and which shapes of user error are ordinary failures rather than reports of missing credentials — a distinction that matters because a missing credential and an unusable file name are both exit-code-affecting but mean opposite things to the person holding the shell.

## Requirements

### Requirement: The environment beats the YAML file

Where the same credential is available from both sources, the environment value SHALL be used, and the file SHALL only supply credentials the environment did not provide. The file that was consulted SHALL remain reportable so a user can see that it was read.

#### Scenario: Credentials only in the environment

- **WHEN** all three credentials are set in the environment and no file is present
- **THEN** the load succeeds and no file is reported as a source

#### Scenario: Credentials only in a file

- **WHEN** no credential is set in the environment and a YAML file supplies all three
- **THEN** the load succeeds and the path of the file used is reported as the source

#### Scenario: The environment overrides the file

- **WHEN** the environment supplies all three credentials and a YAML file supplies different values
- **THEN** the values used are the environment's

#### Scenario: The file fills only the gaps

- **WHEN** the environment supplies some credentials and a YAML file supplies all three
- **THEN** the environment's values are used where present and the file's values are used only for the remainder, and the file is still reported as a source

#### Scenario: A blank environment value is treated as absent

- **WHEN** a credential variable is set to whitespace only and a YAML file supplies a non-blank value
- **THEN** the file's value is used

### Requirement: Only two file names are auto-detected, in a fixed order

When no file is named, the loader SHALL consider `config.yaml` and then `config.local.yaml`, in that order, and SHALL consider nothing else. The absence of both SHALL not be an error.

#### Scenario: The first present candidate wins

- **WHEN** both `config.yaml` and `config.local.yaml` exist
- **THEN** credentials are read from `config.yaml` and `config.local.yaml` is not read

#### Scenario: The second candidate is used when the first is absent

- **WHEN** `config.yaml` is absent and `config.local.yaml` is present
- **THEN** credentials are read from `config.local.yaml`

#### Scenario: No candidate present is not an error

- **WHEN** neither candidate exists
- **THEN** the load reports that no file was used, and continues on the environment alone

#### Scenario: Other extensions are never candidates

- **WHEN** a file named with the `.yml` or `.toml` extension sits in the working directory
- **THEN** it is not read, supplies no credential, and is not named as a source

#### Scenario: An unreadable candidate falls through to the next

- **WHEN** the first candidate exists but cannot be parsed and the second is present and readable
- **THEN** the second candidate is used and the load succeeds

### Requirement: A named file must be YAML, and a wrong shape is a plain error

A file named explicitly SHALL have a `.yaml` name, and the extension SHALL be checked before the file is looked for. A name with any other extension, or a name that does not exist, SHALL be an ordinary error — never a report of missing credentials.

#### Scenario: A YAML file supplies the credentials

- **WHEN** a file with a `.yaml` name that exists is named explicitly and supplies all three credentials
- **THEN** the load succeeds using that file's values

#### Scenario: A non-YAML name is refused

- **WHEN** a named file has any extension other than `.yaml`
- **THEN** the load fails with an error that names the file and states the supported extension

#### Scenario: A non-YAML name is refused whether or not the file exists

- **WHEN** a named file has a non-YAML extension and does not exist
- **THEN** the failure reports the wrong format rather than a missing file

#### Scenario: A wrong format is never reported as a credential problem

- **WHEN** a non-YAML file is named explicitly
- **THEN** the failure is not a missing-credential condition, and does not exit `2`

#### Scenario: A missing named file is an error

- **WHEN** a named `.yaml` file does not exist
- **THEN** the load fails with an error naming that path, and does not fall back to the environment

#### Scenario: A missing named file is not a credential problem

- **WHEN** a named `.yaml` file does not exist
- **THEN** the failure is not a missing-credential condition, even when the environment holds every credential

#### Scenario: A file without the credential block yields no credentials

- **WHEN** a YAML file parses cleanly but carries no credential block
- **THEN** the load succeeds, the file is reported as read, and no credential is invented from it

### Requirement: Credentials come from three named variables with deprecated fallbacks

Credentials SHALL be read from `LONGBRIDGE_APP_KEY`, `LONGBRIDGE_APP_SECRET` and `LONGBRIDGE_ACCESS_TOKEN`. The deprecated `LONGPORT_APP_KEY`, `LONGPORT_APP_SECRET` and `LONGPORT_ACCESS_TOKEN` SHALL continue to be honoured as fallbacks, and the canonical name SHALL win when both spellings are set.

#### Scenario: The canonical names load

- **WHEN** `LONGBRIDGE_APP_KEY`, `LONGBRIDGE_APP_SECRET` and `LONGBRIDGE_ACCESS_TOKEN` are all set
- **THEN** the load succeeds using those three values

#### Scenario: The deprecated names still load

- **WHEN** only `LONGPORT_APP_KEY`, `LONGPORT_APP_SECRET` and `LONGPORT_ACCESS_TOKEN` are set
- **THEN** the load succeeds using those three values

#### Scenario: The canonical name beats the deprecated one

- **WHEN** both spellings of a credential are set to different values
- **THEN** the value from the canonical name is used

#### Scenario: The deprecated names are not advertised

- **WHEN** a command's help output lists the credential variables
- **THEN** it lists one canonical name per credential and does not mention the deprecated spellings

#### Scenario: Every absent credential is named at once

- **WHEN** some but not all credentials resolve
- **THEN** the failure names every absent one in a single report, sorted, rather than one per run
