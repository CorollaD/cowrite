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

`basicSetup` also ships no highlight style, so the markdown parser's tokens
render as plain text unless a highlighter is added. cowrite uses
`tagHighlighter` from `@lezer/highlight` to give each token a `tok-*`
class, so the colours come from CSS variables and follow the selected
writing background rather than being fixed in JavaScript. Import it from
the same bundle: `@lezer/highlight` must also exist exactly once.

## Rebuilding

```sh
npm install codemirror@6.0.2 @codemirror/lang-markdown@6.3.1 @codemirror/language@6.11.3
cat > entry.js <<'JS'
export { EditorView, basicSetup } from "codemirror";
export { markdown } from "@codemirror/lang-markdown";
export { Decoration, ViewPlugin, MatchDecorator, WidgetType, keymap } from "@codemirror/view";
export { Prec } from "@codemirror/state";
export { syntaxHighlighting } from "@codemirror/language";
export { tagHighlighter, tags } from "@lezer/highlight";
JS
npx esbuild entry.js --bundle --format=esm --target=es2020 --minify --outfile=codemirror.js
```

Then confirm the result has no absolute `from "/..."` imports and that
`node_modules/@codemirror/state` resolved to a single directory.
