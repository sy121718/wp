# Plugin Development

This guide is the English entry point for the implemented plugin contract. The complete design, limits, and rollout status remain in [06-plugin-system.md](06-plugin-system.md). Dynamic data-source integration is covered in [04-B-dynamic-development-guide.md](04-B-dynamic-development-guide.md).

## Mental Model

A go_wp plugin is a declarative build input, not dynamically loaded Go code. A plugin package can contribute Jet templates, inspector property schemas, constrained style rules, CSS assets, presets, and versioned SQL migrations. Enabled plugin material is assembled into page builds; visitor requests execute no plugin code.

This boundary preserves deterministic artifacts and keeps activation and rollback on the existing publication path.

## Package Shape

```text
plugin.zip
├── manifest.json
├── components/
│   └── component.jet
├── assets/
│   └── component.css
└── migrations/
    └── 001_init.sql
```

The current package layout uses fixed `components/` and `assets/` directories; the manifest optionally names the migrations directory. A plugin must declare at least one component. Current validation limits the package, file count, component count, property count, template names, control kinds, style values, the migrations directory name, and migration file extensions. Treat installation-time validation as part of the contract, not as a best-effort warning.

## Namespaces and Rendering

- Component type names use `plugin.{pluginID}.{componentName}` and cannot collide with built-in `core.*` types.
- Templates are loaded from `plugin/{pluginID}/{file}.jet`; plugins cannot shadow built-in templates.
- Property controls are restricted to the supported manifest kinds. `select` controls require declared options.
- Style rules are compiled through the constrained style engine and scoped to the component node. Arbitrary style values are outside this contract.
- The current build assembly consumes only `assets/*.css`. Other accepted package asset types are not a public rendering capability yet.
- Collection components declare an allowlisted source, fields, and fixed filters. The builder exposes only the declared data.

The neutral compiler-facing shapes are `PluginPropControl`, `CollectionBinding`, `PluginComponentSpec`, and `PluginResolver` in `internal/builder/source`. The plugin module assembles enabled templates and specs; builder aliases consume the same definitions without creating a reverse dependency from business contracts into the compiler core.

## Development Checklist

1. Define a stable plugin ID and component names before writing templates.
2. Keep templates inside the plugin namespace and render only declared props or collection fields.
3. Express styles with the supported schema; use packaged `assets/*.css` only for structures the schema does not represent.
4. Add migrations as ordered, idempotent SQL files when the plugin owns data.
5. Validate install, enable, preview/build, disable, upgrade, and uninstall paths with the directly related plugin packages.
6. Verify identical inputs produce identical artifact bytes.

Plugins that need arbitrary runtime logic, global hooks, or unrestricted queries require a different capability design; they do not fit the current declarative contract.
