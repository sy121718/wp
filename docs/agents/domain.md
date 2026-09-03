# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

This repo uses a **single-context** layout.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root if present.
- **`docs/02-domain.md`** — this repo's existing long-form domain write-up covering the CMS, Builder, Publish pipeline concepts (控制面/访问面、Page Document、Artifact、Publication 等领域术语)。
- **`docs/adr/`** — read ADRs that touch the area you're about to work in.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo (this repo):

```
/
├── CONTEXT.md          ← may not exist yet; created lazily by /domain-modeling
├── docs/02-domain.md   ← existing domain write-up
├── docs/adr/           ← ADRs (created lazily)
└── internal/ pkg/ ...  ← source
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `/docs/02-domain.md` and `CONTEXT.md`. This repo's `CLAUDE.md` defines the core invariants and 关键协议辨析 — prefer those terms (Page Document, PresentationInstance, Blueprint, ContentTemplate, Artifact, PublicationStore 等). Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-XXXX — but worth reopening because…_
