<!-- Copyright The Linux Foundation and each contributor to LFX. -->
<!-- SPDX-License-Identifier: MIT -->

# Go Development Conventions (deep reference)

Deep do/don't lists for the rules summarized in the parent SKILL.md. Read
this file when adding a new package, when the inline section is not enough,
or when an unfamiliar convention surfaces in review. Query-specific patterns
(pagination, CEL split, batched access checks) live in the SKILL.md body.

## Generated Code

Do:

- Edit `design/query-svc.go` and `design/types.go`, then run `make apigen`
  (or `goa gen github.com/linuxfoundation/lfx-v2-query-service/design`).
- Keep handwritten code in `cmd/`, `internal/`, `pkg/`.
- Match existing package names, file naming, and constructor style before
  introducing a new abstraction.

Do not:

- Hand-edit anything under `gen/`.
- Reuse generated identifiers as the public surface of an internal package.

## Logging

Do:

- Use `log/slog` with the `*Context` variants
  (`slog.InfoContext`, `slog.DebugContext`, `slog.ErrorContext`,
  `slog.WarnContext`).
- Add the repo's stable structured fields when available: `request_id`,
  `principal`, `object_type`, `object_id`, `object_ref`, `error`, plus the
  operation name as positional context.
- Honor `LOG_LEVEL` and the `-d` debug flag.
- Use `pkg/log.AppendCtx` from middleware to attach slog attributes to the
  request context.

Do not:

- Use `fmt.Println`, `fmt.Printf`, `log.Print*`, or `log.Println` for
  runtime logging.
- Log JWTs, raw bearer headers, request bodies, or other PII-bearing
  payloads.
- Add new logger packages. The standard `log/slog` plus `pkg/log` is
  enough.

## Errors

Do:

- Construct domain errors via `pkg/errors`: `NewValidation`, `NewNotFound`,
  `NewServiceUnavailable`, `NewUnexpected`.
- Wrap upstream errors with `%w` so `errors.Is` and `errors.As` still work
  across layers.
- Translate domain errors at the Goa or transport boundary
  (`cmd/service/error.go::wrapError`).

Do not:

- Return raw OpenSearch, NATS, or Clearbit errors out of the service
  layer. Wrap or translate first.
- Introduce a parallel sentinel-error family. The typed domain errors in
  `pkg/errors` already cover the V2 status mapping (400, 404, 500, 503).
- Map errors to HTTP status codes outside the Goa boundary.

## Request Context

Do:

- Read context values via the typed keys in `pkg/constants/http.go`
  (`PrincipalContextID`, `RequestIDHeader`).
- Set up context in the right layer: the request ID is set by
  `internal/middleware/` (`RequestIDMiddleware`), while the principal
  (`PrincipalContextID`) is set in the Goa `JWTAuth` security handler in
  `cmd/service/service.go` during token validation, not in middleware.
  Service code consumes context, it does not parse HTTP headers.
- Forward context into downstream calls (NATS, OpenSearch, CEL evaluator)
  so cancellation propagates.

Do not:

- Use bare-string context keys.
- Mutate context shape ad-hoc in deep call sites. If a new key is needed,
  add it to `pkg/constants/http.go` and set it in the appropriate entry
  layer (request-scoped middleware, or the Goa security handler for
  auth-derived values such as the principal).

## NATS

Query-service is a NATS client (request/reply), not a subscriber. It does
not own queue groups or JetStream consumers.

Do:

- Keep subjects in `pkg/constants/access_control.go`
  (`AccessCheckSubject`, `ReadTuplesSubject`).
- Use `NATSClient.CheckAccess` for batched access checks. It currently calls
  `conn.Request` with the timeout supplied by the service layer.
- Use `NATSClient.ReadTuples` for `filter_grants=direct`; that path wraps the
  request in a timeout-aware context and calls `RequestWithContext`.
- Close the connection through `NATSAccessControlChecker.Close()` from the
  graceful-shutdown path in `cmd/main.go`.

Do not:

- Hardcode subjects at call sites.
- Write directly to another service's KV bucket. Query-service does not
  own any KV bucket of its own.

## Pagination

Do:

- Use `page_size` (default `constants.DefaultPageSize = 50`, max
  `constants.MaxPageSize = 1000`) and opaque `page_token`.
- Generate page tokens through `pkg/paging.EncodePageToken` only when
  `len(hits) == pageSize` in the OpenSearch adapter.
- Treat `page_token` as opaque outside `pkg/paging`. The codec uses the
  secret from `pkg/global.PageTokenSecret(ctx)`.

Do not:

- Let downstream code parse the token.
- Suppress the token when post-page CEL or access-control filtering
  shrinks the page. See SKILL.md "Post-page-shrinkage pagination" for why
  this matters.

## Tests

Do:

- Depend on the interfaces in `internal/domain/port/` for external
  systems (OpenSearch, NATS, FGA, Clearbit, filter).
- Keep mocks in `internal/infrastructure/mock/` and use the in-package
  mocks for NATS client tests (see `access_control_test.go`).
- Write table-driven tests with one test function per exported method.
- Co-locate `*_test.go` files with the code under test.
- Run `make test` (race detector enabled) before handing off.

Do not:

- Add new mock layouts under different paths.
- Stub external systems in service-layer tests at the HTTP level when an
  interface mock would do.

## Formatting and Review Hygiene

Do:

- Run `gofmt -w` on changed files (there is no `make fmt` target) and
  `make lint` before commit.
- Use `npx mega-linter-runner .` to mirror CI when unsure.
- Preserve the two-line license header on every new `.go` file.
- Add docstrings for exported symbols when `revive.toml` requires them.
- Update `docs/` and `pkg/constants/` in the same change as
  any behavior that affects callers or contracts.

Do not:

- Bypass the linter target with selective skips when a fix is easy.
- Land contract-affecting changes without updating the matching contract
  doc.
