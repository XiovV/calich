# The object graph is built in one place

Status: accepted

Until now nothing owned construction. `cmd/server/main.go` wired the server by hand, and so did every test needing more than one repository — 42 test files, each holding its own copy of the same expression. `NewEventService` takes 16 concrete `*repository.X` values and `NewCalendarService` 10, so widening either edited all of them at once: `a40f3e1` ("Default reminders fire") added two repositories and touched 57 files, 37 of which changed by ten lines or fewer, almost entirely constructor arguments.

**There is now one build order, and two adapters over it.** `service.NewGraph(sqlDB, cfg)` builds every repository and service; `app.New(sqlDB, cfg)` builds the handlers, the CalDAV backend, the mail transport and the background workers on top of it. `service.NewInMemoryGraph(cfg)` and `app.NewInMemory(cfg)` are the same builds over `db.OpenInMemory`. No test constructs a service anywhere in the repo. Adding a repository is a change to `internal/service/graph.go` and that repository's own package, and to no test file at all.

The two adapters are the point. A seam with one implementation is a claim rather than a fact: it is only true that construction is centralized while nothing needs to construct it differently, and a test that needs a different database is exactly the thing that would otherwise fork it. Production and in-memory are the two shapes this server actually has, and both go through the same function.

## Why the build order sits in two packages

The natural design puts everything in `internal/app`. Go forbids it.

`internal/service` and `internal/handlers` both test themselves *in-package*: their tests reach unexported helpers (`pickFreeColor`, `calendarSwatches`, `hashToken`, `errorCase`, `calendarResponse`, `pathPrefix`), and moving them to an external `_test` package would mean exporting production internals for the tests' benefit. An in-package test may not import a package that imports the package under test — the compiler rejects it as an import cycle — so a builder living in `internal/app` would be unreachable from the 25 test files in `internal/service` that need a wired-up service, which is precisely the duplication being removed.

So the split follows the constraint rather than a preference. The repository-and-service half — the 21 repositories and 12 services, which is all of the churn-prone wiring — lives in `internal/service`, where everything above it can reach it and where the package's own tests can too. `internal/app` sits on top for `cmd/server`. Each half states its build order once.

The same rule decides what `internal/app` may import. It imports `handlers` and `caldavserver`, so those packages' own tests build a `service.Graph` and construct the one handler or backend they are testing — a single call each, and the thing under test besides. It deliberately does *not* import `router`, which is why `internal/router`'s tests can use `app.NewInMemory` and get the whole server, handlers included.

`internal/apptest` holds the config every test builds from and nothing else. Keeping `testing` out of the production binary is the usual reason for a separate package; here it is also what lets *every* test package share those defaults, since a package importing `service` could not be imported by `service`'s own tests.

## The role interfaces stay as they are

`router.New` takes 17 parameters, five of them narrow interfaces `httpauth` declares — `Authenticator`, `ActiveUserChecker`, `CalDAVAuthenticator`, `DisabledChecker`, `WorkspaceMembershipChecker`. It is tempting to collapse them into the `App` value on the same "too many arguments" reasoning, and it would be wrong.

Those five are not wiring; they are the seam that keeps middleware from depending on services. Each names one question a request-time check asks, `AuthService` satisfies three of them and `AppPasswordService` a fourth, and every one has a real adapter behind it. Passing `*app.App` instead would let any middleware reach any service, which is the opposite of what the interfaces exist to prevent. **The App's job is to supply their arguments, not to absorb them** — `main` passes `a.Auth`, `a.AppPasswords`, `a.Workspaces` into the same signature as before.

Router construction stays in `main` for the same reason it stays out of `internal/app`: it is the composition of handlers with policy, and the caller that owns the process is the one that should state it.

## Considered Options

- **One `internal/app` builder, with `internal/service` keeping hand-wired tests.** Rejected: two ways to build the graph is worse than either end state, and the 25 files it would leave behind are most of the problem.
- **Move `internal/service` and `internal/handlers` tests to external `_test` packages** so one `apptest` could serve everyone. Rejected: it needs `calendarSwatches`, `hashToken` and a dozen response structs exported for no reason other than test packaging, and it rewrites 42 files' package clauses to save one package boundary.
- **A DI container (wire, fx, or a hand-rolled registry).** Rejected: the graph is a fixed list built once at startup. A generator or a reflection-based container buys nothing a struct literal doesn't, and costs a build step or a runtime failure mode.
- **Keep `main` hand-wiring and give tests a shared helper.** Rejected for the same reason as the first option, one level down: the helper and `main` drift, and it is `main` that is exercised least.
- **A fourth package wrapping `NewInMemoryGraph` in `*testing.T` lifecycle,** shared by `internal/handlers` and `internal/caldavserver` so the nine-line `newTestGraph` helper isn't written twice. It could not also serve `internal/service`, whose tests are the reason the seam is shaped this way, so it would dedupe two of three copies at the cost of a package existing for nine lines. Not worth it while the helper stays this small; worth revisiting if it grows.

## Consequences

- **The JWT signing secret is minted by `NewGraph`,** 32 random bytes per build, and exposed as `Graph.JWTSecret`. `main` no longer generates it. A test needing to hand-craft a token pins it with `WithJWTSecret`.
- **Two build inputs are options rather than config,** because neither is something a self-hoster sets: `WithJWTSecret`, and `WithSubscribeHTTPClient` for the client `SubscribeService` fetches feeds with — a test serving a feed from loopback needs the address guard (ADR-0032) out of the way, and a test *of* the guard keeps the default.
- **A test's deployment shape is now a config, not a wiring choice.** Whether an Invitation can be queued was "did this fixture pass an `OutboxRepository`"; it is now `SMTPConfigured()`, the same question production asks. Same for the attachment limits, the invite rate limit, and the bootstrap account.
- **`internal/reminder`'s tests keep building repositories directly.** They test the firing engine against specific repositories and construct no service, so the graph would tell them nothing. Adding a repository does not touch them either.
- **`main` builds the workers but no longer wires them.** `App` holds the scheduler, poller, sweeper, and the two optional workers; `main` keeps the tick intervals, the contexts, and the goroutines, which is the part that belongs to whoever owns the process.
- **The in-memory adapter is ordinary code, not a test helper.** `NewInMemoryGraph` and `app.NewInMemory` therefore link into the production binary, as `db.OpenInMemory` already did. That is the price of the import rule above: a package's in-memory constructor has to sit where its production one does. `testing` itself stays out, which is what the constraint was about.
- **`Graph` is a struct of exported fields, not an interface.** It is a value handed to `main` and to tests, both of which want the concrete services; hiding them behind accessors would add a method per field and answer no question.
