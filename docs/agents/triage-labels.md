# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

## Wayfinder labels

In addition to the five triage roles, the `/wayfinder` skill uses its own ticket labels:

| Label | Meaning |
| ----- | ------- |
| `wayfinder:map`      | The single canonical map issue |
| `wayfinder:research` | Wayfinder ticket: research (AFK) |
| `wayfinder:prototype`| Wayfinder ticket: prototype (HITL) |
| `wayfinder:grilling` | Wayfinder ticket: grilling (HITL) |
| `wayfinder:task`     | Wayfinder ticket: task (HITL or AFK) |

Edit the right-hand column of the triage table to match whatever vocabulary you actually use.
