## Agent skills

### Issue tracker

Issues live in GitHub Issues (XiovV/calendar), via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Workflow rules

- Never launch dev servers or run browser tests (Playwright, chromium-cli, etc.) yourself. The user runs dev servers and does browser testing manually.
