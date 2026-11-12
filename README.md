# Inkwell — Markdown Editor

Inkwell is a lightweight, self-hosted Markdown editor that runs in your browser. You write in a
syntax-highlighted editor and see a live preview beside it. Documents are stored in SQLite and saved
automatically, and you can import and export Markdown, HTML and PDF.

It is built with Go, the standard library, `html/template`, plain CSS and vanilla JavaScript. It uses
no frontend framework, no build step and no cgo.

## Features

- **Editor:** line numbers, Markdown syntax highlighting, auto-indentation, list and blockquote
  continuation on Enter, Tab/Shift+Tab indentation, undo/redo, and word, character and reading-time
  counts. It also shows the cursor position, the document title and an unsaved-changes indicator.
- **Live preview:** GitHub Flavored Markdown (tables, task lists, strikethrough, autolinks),
  footnotes, and syntax-highlighted fenced code blocks. All rendered HTML is sanitized.
- **Layouts:** editor only, split or preview only, with a resizable divider (drag, keyboard or
  double-click to reset) and a distraction-free mode. The layout is responsive: on small screens
  the editor and preview are shown one at a time, and the sidebar becomes a drawer.
- **Formatting toolbar:** headings 1–6, bold, italic, strikethrough, inline code, code blocks,
  quotes, bulleted, numbered and task lists, links, images, tables and horizontal rules. Each
  command wraps the selection or inserts a placeholder, and toggles off when applied twice.
- **Documents:** create, open, rename, duplicate and delete (with confirmation). You can search
  titles and content, sort by title, creation date or last-modified date, and see a "Recently
  edited" list.
- **Autosave:** saves after 1 s of inactivity and shows *Saving… / Saved / Unsaved changes / Save
  failed* states. Unchanged content is never written, failed saves are retried with backoff, and
  the browser warns you before you leave with unsaved changes. Ctrl/Cmd+S saves manually.
- **Import/export:** import `.md`/`.markdown` files with the Import button or by drag and drop.
  Export to `.md`, styled standalone `.html`, or PDF.
- **Appearance:** light and dark themes, plus editor font family, font size and line spacing. These
  preferences are stored in the database.

## Requirements

- Go 1.26 or newer (see `go.mod`)
- A modern browser (Chrome, Edge, Firefox or Safari)

No C compiler is needed. SQLite is provided by the pure-Go `modernc.org/sqlite` driver.

## Running

```bash
go run ./cmd/web
```

Then open <http://127.0.0.1:8080>. On first start the database is created and migrated
automatically, and a "Welcome" document is created.

To build a single self-contained binary (templates and assets are embedded):

```bash
go build -o markdown-editor ./cmd/web
./markdown-editor
```

### Configuration

All settings are optional environment variables:

| Variable             | Default                   | Description                                                   |
| -------------------- | ------------------------- | ------------------------------------------------------------- |
| `ADDR`               | `127.0.0.1:8080`          | Listen address. Use `:8080` to listen on all interfaces.      |
| `DATABASE_PATH`      | `data/markdown-editor.db` | SQLite database file. The directory is created if missing.    |
| `MAX_DOCUMENT_BYTES` | `5242880` (5 MiB)         | Maximum document size and maximum import file size.           |
| `SECURE_COOKIES`     | `false`                   | Set to `true` when served over HTTPS (marks cookies `Secure`). |

Example:

```bash
ADDR=:9000 DATABASE_PATH=/var/lib/inkwell/docs.db go run ./cmd/web
```

### Database initialization

No manual step is required. On startup the app opens (or creates) the SQLite file, enables WAL mode
and foreign keys, and applies any pending migrations from `migrations/`. Migrations are embedded in
the binary. Each one runs in a transaction and is recorded in the `schema_migrations` table, so
restarting is always safe. To start fresh, stop the server and delete the database file and its
`-wal`/`-shm` siblings.

## Testing

```bash
go test ./...                                          # all tests
go test ./internal/handlers -run TestImport -v         # a single test
go test -race ./...                                    # with the race detector (needs cgo)
go vet ./...
```

The tests use the standard `testing` package and `net/http/httptest`. Each test gets its own
temporary SQLite database. They cover:

- the document service (create, update, rename, duplicate, delete, validation)
- database queries, constraints and migrations
- every HTTP endpoint, including status codes and validation
- CSRF and security headers
- Markdown rendering and XSS sanitization
- autosave behavior (unchanged saves, stale versions, concurrent saves)
- import/export, including file names and size limits
- preferences and configuration

## Keyboard shortcuts

Use Cmd instead of Ctrl on macOS. The list is also available in the app (keyboard icon or
Ctrl+/).

| Shortcut                            | Action                                  |
| ----------------------------------- | --------------------------------------- |
| Ctrl+S                              | Save                                    |
| Ctrl+B / Ctrl+I                     | Bold / italic                           |
| Ctrl+K                              | Insert link                             |
| Ctrl+Shift+P                        | Toggle preview                          |
| Ctrl+N (or Ctrl+Alt+N)              | New document                            |
| Ctrl+Z / Ctrl+Shift+Z (or Ctrl+Y)   | Undo / redo                             |
| Ctrl+Shift+F                        | Distraction-free mode                   |
| Esc                                 | Exit distraction-free mode              |
| Tab / Shift+Tab                     | Indent / outdent                        |

Some browsers reserve Ctrl+N (new window) and do not let web pages override it, which is why
Ctrl+Alt+N is also available. Inside the editor, Tab indents, so **Esc** moves keyboard focus out
of the editor.

## Project structure

```text
cmd/web/              entrypoint: config, database, wiring, graceful shutdown
internal/config/      environment-variable configuration
internal/documents/   document domain: validation, business rules, import/export helpers
internal/preferences/ user preference validation and defaults
internal/database/    SQLite connection, migration runner, SQL stores
internal/markdown/    GFM rendering, code highlighting, HTML sanitization
internal/middleware/  CSRF, security headers, logging, panic recovery
internal/handlers/    HTTP routing, request decoding/validation, error mapping
migrations/           embedded SQL schema migrations
web/templates/        html/template pages (app, print view, HTML export)
web/static/css/       app.css (UI) and document.css (rendered Markdown)
web/static/js/        vanilla JS modules: app, editor, highlight, format, api
```

The layers depend in one direction: `handlers → documents/preferences → Store interface ←
database`. Business rules live in `documents` and `preferences`, which know nothing about HTTP or
SQL. Handlers only translate HTTP to and from service calls, and the `database` package implements
the store interfaces with parameterized SQL.

### HTTP API

| Method & path                             | Purpose                                           |
| ----------------------------------------- | ------------------------------------------------- |
| `GET /api/documents?q=&sort=updated-desc` | List or search (`sort`: `updated`/`created`/`title` + `-asc`/`-desc`) |
| `GET /api/documents/recent`               | Five most recently edited documents               |
| `POST /api/documents`                     | Create `{title, content}`                         |
| `GET /api/documents/{id}`                 | Fetch one document                                |
| `PUT /api/documents/{id}`                 | Save `{title, content, version}`; returns `409` on a version conflict |
| `PATCH /api/documents/{id}`               | Rename `{title}`                                  |
| `DELETE /api/documents/{id}`              | Delete                                            |
| `POST /api/documents/{id}/duplicate`      | Duplicate                                         |
| `POST /api/documents/import`              | Multipart upload, field `file`                    |
| `GET /api/documents/{id}/export?format=`  | Download as `md` or `html`                        |
| `GET /documents/{id}/print`               | Print-optimized page (used for PDF export)        |
| `POST /api/render`                        | Render Markdown to sanitized HTML (live preview)  |
| `GET/PUT /api/preferences`                | Read or update preferences                        |

Requests that change data must send the `X-CSRF-Token` header. The token comes from the page's
`<meta name="csrf-token">` tag.

## Design decisions

- **Server-side rendering for the preview.** The browser sends the Markdown to `POST /api/render`
  (debounced, with stale responses ignored). The live preview, the HTML export and the PDF all use
  the same Go renderer and the same sanitizer, so they always match and only one Markdown
  implementation needs to be secured.
- **Defense in depth against XSS.** goldmark is allowed to pass raw HTML through so that harmless
  tags like `<kbd>` and `<details>` work. Its output is then always filtered through a strict
  bluemonday allow-list, which strips scripts, event handlers, `javascript:` URLs, iframes, styles
  and forms. A strict Content-Security-Policy (`script-src 'self'`, no inline scripts or styles) is
  a second layer. Sidebar entries and titles are built with `textContent`, never `innerHTML`.
- **Autosave without lost updates.** Each document has a `version` column. A save sends the version
  it was based on, and the update is a compare-and-swap (`UPDATE … WHERE id = ? AND version = ?`).
  The client sends at most one save at a time and merges edits made during a save into a single
  follow-up save of the latest text, so an older snapshot can never land after a newer one. If the
  same document is edited in another tab, the server returns `409` with its current copy, and you
  choose whether to keep your version or load the latest. Saves whose title and content are
  unchanged are skipped on both client and server.
- **Editor = textarea + highlighted mirror.** A transparent `<textarea>` sits on top of a `<pre>`
  that renders highlighted Markdown, one block per source line. Native typing, IME, spellcheck,
  selection and accessibility keep working. Line numbers come from CSS counters on each line block,
  so they stay aligned with soft-wrapped lines. Highlighting only changes colors, never font
  metrics, so both layers wrap identically. Undo/redo is implemented in JS because programmatic
  edits (toolbar actions, Tab, list continuation) would break the browser's native undo stack.
- **PDF export through the browser's print engine.** "Export → PDF" opens a print-optimized page
  (`@page` margins, page-break rules for headings, code blocks and tables) and opens the print
  dialog, where *Save as PDF* lets you choose where the file goes. This produces properly formatted
  PDFs with selectable text, without bundling a headless browser or a PDF library on the server.
- **CSRF.** There are two independent checks. Go's `http.CrossOriginProtection` rejects
  cross-origin browser requests using `Sec-Fetch-Site`/`Origin`. A double-submit token stored in a
  `SameSite=Strict`, `HttpOnly` cookie must also be echoed in the `X-CSRF-Token` header.
- **Safe imports and downloads.** Uploads are streamed with a hard size limit and accepted only if
  they have a `.md`/`.markdown` extension and are valid UTF-8 text without NUL bytes. The upload's
  file name is only used to derive a title and never touches the filesystem. Download names are
  sanitized and encoded with RFC 2231 in `Content-Disposition`, and downloads are served with
  `nosniff` and a sandboxing CSP.
- **Validation everywhere.** Document IDs are random 128-bit hex strings and are checked before any
  query. JSON bodies are size-limited and reject unknown fields. Titles are normalized and limited
  to 200 characters. The schema repeats the key rules as `CHECK` constraints.
- **Dependencies, and why each one is needed:**
  - `github.com/yuin/goldmark`: a CommonMark-compliant Markdown parser with GFM extensions.
  - `github.com/yuin/goldmark-highlighting` and `github.com/alecthomas/chroma`: syntax
    highlighting for code blocks, using CSS classes so a light and a dark theme can be served.
  - `github.com/microcosm-cc/bluemonday`: a well-tested HTML sanitizer.
  - `modernc.org/sqlite`: SQLite without cgo, so `go run` works on any machine with Go installed.
