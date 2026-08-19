# Browser QA

How to exercise the real app in a real browser, rather than inferring behaviour
from unit tests.

Use this when a change needs to be *seen* working: layout and interaction in the
calendar grid, a flow that spans several screens, anything involving drag,
focus, or scroll, or a bug report that describes what the user saw.

Vitest still covers logic and component behaviour (`make test-frontend`) and is
much faster — reach for the browser when the question is genuinely about the
running app.

## The stack you're allowed to touch

There are two stacks, and they are not interchangeable.

| | Owned by | Ports | Database |
|---|---|---|---|
| `make dev-backend` / `make dev-frontend` | the user, run by hand | 8080 / 5173 | `./data/calich.db` |
| `make qa-up` | agents doing browser QA | 8091 / 5191 | `./.qa/data/calich.db` |

**Only ever drive the QA stack.** Never start the user's dev servers, and never
read from or write to `./data` — that's their working database, and QA means
creating and deleting events. The QA stack exists precisely so you can be
destructive without asking. Both stacks can run at once; the ports and
databases don't overlap.

## The loop

```
make qa-up      # boots both servers, seeds the QA account, prints the URLs
                # ... drive the browser with the Playwright MCP tools ...
make qa-down    # always, when you're finished
```

- **App**: http://localhost:5191
- **Login**: `qa@calich.test` / `qa-password`
- **`make qa-reset`** wipes the QA database and starts over — use it when
  earlier QA left junk that's now getting in the way, or when a test needs the
  first-run experience.
- **`make qa-status`** says what's already running. `qa-up` is safe to re-run;
  it's a no-op for anything already up.

The QA database persists across `qa-up`/`qa-down` cycles, so a scenario you set
up by hand survives until you `qa-reset`.

`.qa/` holds everything: `data/` the database, `logs/backend.log` and
`logs/frontend.log`, `output/` the screenshots the MCP server writes. It's
gitignored, and safe to delete entirely.

## Driving the browser

The `playwright` MCP server is configured in `.mcp.json`. It runs an isolated
browser profile, so **every run starts logged out** — sign in through the UI as
the first step of a session. Log in through the form rather than injecting
tokens; it's one step, and it means the auth path is under test too.

While you're in there:

- Read the console and network panels, not just the page. A 401 on
  `/api/auth/refresh` before you log in is normal noise; almost anything else
  is a finding.
- Check `.qa/logs/backend.log` when the UI reports a failure — the cause is
  usually there, and it's cheaper than guessing from the response body.
- Take a screenshot of anything visual you're reporting. Describing a layout
  bug in prose loses the bug.

## Reporting

Say what you did, what you saw, and what you expected — in that order, per
finding. Attach the screenshot. Distinguish "this is broken" from "this looks
off to me": the second is a judgement call and should be flagged as one.

If a QA pass finds nothing, say so plainly, and say what you actually
exercised, so the coverage is legible rather than implied.
