# 0001: Record architecture decisions

## Status

Accepted

## Context

Significant architectural decisions benefit from a durable record of why they were made. Without one, decisions can be revisited without their original constraints, and important context may exist only in conversations or commit history.

The project needs a lightweight way to preserve these decisions alongside the code.

## Decision

We will document significant architectural decisions as Architecture Decision Records (ADRs) in `docs/adrs/`.

Each ADR will:

- use a four-digit sequence number and short descriptive filename;
- contain Status, Context, Decision, Consequences, and Related sections;
- begin as `Proposed` and become `Accepted` when adopted;
- remain as a historical record after acceptance.

When an accepted decision changes, a new ADR will supersede or deprecate the earlier record instead of replacing its original content.

## Consequences

- Architectural decisions and their rationale remain discoverable in the repository.
- Contributors have a consistent format for proposing and reviewing decisions.
- Maintaining ADRs adds a small documentation cost.
- The ADR index must be updated when records are added or their statuses change.

## Related

- [Project documentation](../README.md)
