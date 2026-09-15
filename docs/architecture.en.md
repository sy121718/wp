# Architecture

This is an English map of the boundaries contributors need before changing the system. The complete design and frozen-boundary table remain in [01-overview.md](01-overview.md); publishing details are in [03-pipeline.md](03-pipeline.md).

## System Shape

go_wp combines a CMS, a visual website builder, and a static publishing engine. Editors change CMS content and Page Documents in the control plane. The publish compiler combines a document, a `BuildContext`, and registered components into immutable HTML, CSS, JavaScript, and a manifest. `ArtifactStore` persists those bytes, and `PublicationStore` activates a public URL.

```text
CMS content + Page Document + BuildContext + Registry
                         -> Publish Compiler
                         -> immutable Artifact
                         -> ArtifactStore
                         -> PublicationStore activation
                         -> Static Server / CDN
```

Normal visitor requests read activated files. They do not query the CMS database, execute Jet, or interpret a Page Document. Live inventory, cart state, account state, and similar data use allowlisted Runtime Fragment endpoints. Browser-only behavior uses controlled client enhancements.

## Core Invariants

- Control and delivery are separate. Database pointers support control, audit, and recovery; filesystem publication state determines which bytes a URL serves.
- Manual `Page` instances and automatic `PresentationInstance` instances share the same compiler, artifact store, and publication store.
- A `Blueprint` initializes a Page Document and is then discarded. A `ContentTemplate` participates in each automatic content build.
- Bindings are allowlisted references, not a query language. Documents do not carry SQL, arbitrary endpoints, or executable user code.
- Builds are deterministic for the same document, context, registry, and plugin version set.
- Shared build-time data shapes live in `internal/builder/source`. Business contract packages may depend on that package, but must not depend back on `internal/builder/core`.

## Where Changes Belong

- `internal/builder/`: document protocol, component registry, compiler, and constrained style/rendering support
- `internal/module/`: business modules; cross-module calls go through narrow `contract` interfaces
- `internal/pipeline/`: publishing and artifact activation flow
- `internal/templates/`: admin/workbench templates and static assets
- `pkg/`: infrastructure packages
- `public/migrations/`: versioned database migrations
- `public/test/`: feature and integration tests

Before treating a design note as runtime truth, trace the current route, assembly path, and tests. Source comments and old plans may lag behind the running composition.
