# Engineering & AI Collaboration Guidelines

This repository is developed using AI-assisted engineering with strict human oversight. A high bar for stability, extensive test coverage, and code hardening is maintained.

---

## 🎯 Architecture & Intent
- **Context:** Minimal, highly encapsulated Go library (`package lpsrv`) providing
  composable `net/http` server primitives, hardened middleware, strict
  size-bounded JSON codecs, health probes, and Google Cloud Run lifecycle
  management.
- **Role:** Consumed by downstream Cloud Run microservices (particularly
  long-running AI services and data APIs) so service packages only wire domain
  handlers instead of duplicating transport, authentication, JSON validation,
  and graceful-shutdown boilerplate.
- **Frameworks:** Pure Go standard library and thorsphere foundational packages
  (`tserr`, `tslog`). Keep dependencies to an absolute minimum, no third-party
  HTTP routers or web frameworks.
- **Configuration:** Twelve-factor app principles. Core middleware, handlers,
  and codecs accept explicit parameters and functional options; only top-level
  Cloud Run lifecycle entrypoints (`Run` / `RunContext`) read environment
  variables (`os.Getenv`) such as `PORT` or `API_TOKEN`.

## 🤖 AI Agent Instructions
When generating code, refactoring, or planning tasks, you MUST adhere to these technical constraints:

### 1. Package Scope, HTTP Hardening & Cloud Run Constraints
`lpsrv` is a domain-agnostic HTTP server toolkit library, not an application binary or an opaque framework.

**a) Library & API Design**
- **Standard `net/http` Composability:** Built around standard `http.Handler`
  and `http.HandlerFunc` so downstream services can test handlers directly with
  `net/http/httptest` without starting a listener.
- **Domain Agnosticism:** `lpsrv` MUST NOT contain service-specific business
  logic, AI provider SDKs, storage drivers, or domain environment variables
  (e.g., model names or database DSNs). No binaries under `cmd/` unless
  explicitly requested.
- **Statelessness:** No local file persistence or mutable cross-request global
  state.

**b) Cloud Run Lifecycle (`Run` / `RunContext`)**
- **Port Binding:** Read and trim the `$PORT` environment variable, falling back
  to `"8080"` with a `tslog.Warn` diagnostic when unset or whitespace-only.
- **Signal Handling & Graceful Shutdown:** `Run` listens for `os.Interrupt` and
  `syscall.SIGTERM` and delegates to `RunContext`. When the context is canceled,
  stop accepting new connections and drain in-flight requests within a bounded
  shutdown deadline (default `8s`, fitting inside Cloud Run's 10s `SIGTERM`
  window).
- **Deadline-Aware Socket Timeouts:** Always configure `ReadHeaderTimeout`,
  `ReadTimeout`, `WriteTimeout`, and `IdleTimeout` on `http.Server`. `WriteTimeout`
  MUST include a safety buffer above the configured per-request/handler timeout
  so slow AI generations can fail cleanly before the TCP socket is reset.

**c) HTTP & Security Hardening**
- **Bearer Authentication:** Reject configured tokens outside the RFC 6750 grammar,
  compare fixed-length SHA-256 digests with `subtle.ConstantTimeCompare` to
  prevent both content and length timing leaks, and set `WWW-Authenticate: Bearer`
  per RFC 6750 on `401 Unauthorized` responses.
- **Strict Bounded JSON Decoding:** Bound request bodies with
  `http.MaxBytesReader` (mapping `*http.MaxBytesError` to `413 Request Entity
  Too Large` via `tserr.TooLarge`), call `DisallowUnknownFields()`, and verify
  no trailing tokens follow the decoded JSON value (`io.EOF` check).
- **Buffered JSON Encoding & Safe 500s:** Marshal response payloads in memory
  before writing status headers so marshaling failures can cleanly return `500`.
  Log server errors via `tslog.Error` and avoid leaking sensitive upstream
  internals in `500` response bodies unless explicitly intended.
- **RFC 9110 Semantics:** Health probe handlers MUST support both `GET` and
  `HEAD` (omitting the response body on `HEAD`). Every `405 Method Not Allowed`
  response MUST set the `Allow` header before calling `http.Error` with
  `tserr.MethodNotAllowed`.

### 2. Quality & Hardening Standards
- **Testing:** Every new feature or package function requires table-driven Unit Tests. Target >85% coverage. Error-presence assertions use tserr.NilFailed/NilExpected/Empty; value mismatches use tserr.EqualStr for strings and plain t.Errorf for composite values.
- **Error Handling:** Explicit, idiomatic Go error handling. Do not panic. Wrap errors contextually where appropriate. Use the `tserr` package for error handling.
- **Code Formatting:** Use `gofmt` to format Go code.
- **Logging:** Structured JSON logging to stdout (GCP Cloud Logging compatible) via `tslog`.

### 3. Coding Style (match the existing codebase exactly)
- **File header:** Every `.go` file starts with the thorsphere copyright
  notice (FSL-1.1-ALv2, see any existing file) followed by the package clause.
- **Imports:** Precede the import block with `// Import packages` and annotate
  every import inline, e.g. `"strings" // Import the strings package for string manipulation.`
- **Comments:** Doc comment on every declaration, functions (exported
  and unexported), types, and package-level vars/consts. Narrate defensive
  steps inline: `// If the token is blank, return an error.`,
  `// Return no error to indicate success.`
- **Atomic lines:** Comment every code line that carries behavior of its
  own (a call, assignment, branch, or return). Scaffolding lines (braces,
  bare closures) may go without; const-block members each carry their own
  trailing comment.
- **Structs:** Doc comment above every struct declaration, and a purpose
  comment appended to every field line, e.g.
  `ReadTimeout time.Duration // The maximum duration for reading the entire request body.`
- **Consts, vars, and types:** Doc comments open with a complete sentence
  naming the symbol. A type's doc comment states what each instance
  represents or provides; document the zero value's meaning when it is
  not obvious. Grouped consts or vars carry one doc comment for the
  group plus a short end-of-line comment per member; ungrouped
  declarations carry a full doc comment. Typed constants may lean on the
  type's doc comment instead of a group comment. Vars follow the const
  conventions.
- **Function doc comments:** Begin with the function name and state the
  contract, not the implementation: what the function returns, or — for
  functions called for side effects — what it does. Refer to named
  parameters and results directly by name, without backquotes. Name
  results in the signature only when the comment must distinguish
  multiple results; otherwise omit them. Use idiomatic openers:
  "returns" for value-plus-error functions, "reports whether" for
  booleans. A comment that merely restates the function name or narrates
  implementation steps does not satisfy this rule — narration belongs on
  the atomic lines inside the body.
- **Errors:** Check every returned error. Never panic. Wrap with the
  appropriate `tserr` constructor.
- **Tests:** External test package `lpsrv_test`. Bridge unexported
  functions and seams via `export_test.go`. Unit tests must NOT open real
  network sockets, depend on external binaries, or call remote services —
  tests must pass in any environment (CI, Cloud Build, restricted sandboxes).
  Route socket operations (`listenAndServe`, `shutdownServer`) and OS signal
  hooks through package-level function-variable seams and swap them in tests
  via `Set<Name>` helpers in `export_test.go`. Tests that mutate a global seam
  or call `t.Setenv` must not use `t.Parallel()`.

### 4. Commands
- Build: `go build ./...` — Test: `go test ./...` — Vet: `go vet ./...`
- Format check: `gofmt -l .` (must output nothing)
- Toolchain: latest Go toolchain (per `go.mod`)

### 5. Commit Messages
- Commits in this repository follow strict Conventional Commits:
  `<type>(<scope>)[!]: <imperative description>` with a header ≤72 characters,
  a body wrapped at 72 characters explaining WHAT and WHY, and a
  `BREAKING CHANGE:` footer when consumers must adapt.
- **Scope is mandatory:** Every commit header MUST include a parenthesized
  scope — never omit `(<scope>)`.
- When a format or contract is defined in multiple coupled places, a change MUST be
  applied to all of them in the same commit, never one side alone.
- Description at least 3 characters.

### 6. Deliberate Design Decisions — do NOT "fix" these
- **SHA-256 digest before `subtle.ConstantTimeCompare` in Bearer auth:**
  `subtle.ConstantTimeCompare` returns `0` immediately when slice lengths
  differ, which leaks the length of `API_TOKEN` via timing. Parse the
  case-insensitive Bearer scheme and its ASCII space separator, then hash both
  the supplied and configured tokens with `sha256.Sum256` for a fixed 32-byte
  constant-time comparison. Do not "simplify" this to direct
  `ConstantTimeCompare` on raw strings/bytes or `==`.
- **In-memory `json.Marshal` before writing HTTP headers:** Response JSON is
  deliberately marshaled into a `[]byte` buffer before setting headers or
  calling `w.WriteHeader`, rather than streaming with `json.NewEncoder(w).Encode`.
  Streaming flushes `200 OK` on the first write, making it impossible to return
  `500 Internal Server Error` if marshaling fails midway. Do not replace with
  `json.NewEncoder(w)`.
- **`WriteTimeout` derived with a buffer above the request timeout:** Long-running
  AI handlers rely on per-request `context.WithTimeout` deadlines to cancel
  upstream calls cleanly and write a structured error response. `http.Server.WriteTimeout`
  must always remain strictly greater than the handler timeout; do not decouple
  them or set a static short `WriteTimeout`.
- **Function-variable seams instead of ephemeral `:0` sockets in tests:**
  `listenAndServe` and `shutdownServer` are package-level seams swapped via
  `export_test.go` so lifecycle, startup failures, and shutdown errors are
  tested deterministically without binding ports. Do not replace these seams
  with real `net.Listen("tcp", "127.0.0.1:0")` sockets in unit tests.

---

## 🛠️ Human Review & Hardening Process
*Every line of code generated by AI agents undergoes a mandatory human review, hardening loop, and optimization pass before being submitted to the repository. The review loop applies to the internal AI-assisted process; external contribution policy is governed by CONTRIBUTING.md*
