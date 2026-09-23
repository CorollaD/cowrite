# Vendored dependencies

`codemirror.js` is CodeMirror 6, bundled locally and committed rather than
loaded from a CDN: cowrite is a local-first tool and has to keep working
with no network.

Built from:

| Package | Version | Licence |
|---|---|---|
| `codemirror` | 6.0.2 | MIT |
| `@codemirror/lang-markdown` | 6.3.1 | MIT |
| `@codemirror/language` | 6.11.3 | MIT |

## Why one bundle

These must be bundled **together**. Fetching them as three separate
bundles gives each its own copy of `@codemirror/state`, and the
`instanceof` checks that validate extensions then fail across the bundle
boundary with "Unrecognized extension value in extension set".

`basicSetup` also ships no highlight style, so `@codemirror/language` is
needed for `syntaxHighlighting(defaultHighlightStyle)` — without it the
markdown parser produces tokens that nothing renders.

## Rebuilding

```sh
npm install codemirror@6.0.2 @codemirror/lang-markdown@6.3.1 @codemirror/language@6.11.3
cat > entry.js <<'JS'
export { EditorView, basicSetup } from "codemirror";
export { markdown } from "@codemirror/lang-markdown";
export { syntaxHighlighting, defaultHighlightStyle } from "@codemirror/language";
JS
npx esbuild entry.js --bundle --format=esm --target=es2020 --minify --outfile=codemirror.js
```

Then confirm the result has no absolute `from "/..."` imports and that
`node_modules/@codemirror/state` resolved to a single directory.
