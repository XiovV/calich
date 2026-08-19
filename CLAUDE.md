## Agent skills

### Issue tracker

Issues live in GitHub Issues (XiovV/calich), via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Workflow rules

- Never start the user's dev servers (`make dev-backend`, `make dev-frontend`, `yarn dev`) and never touch `./data` — that stack and its database are theirs, run by hand.
- For browser QA, use the throwaway QA stack instead: `make qa-up`, drive it with the `playwright` MCP tools, `make qa-down` when finished. See `docs/agents/browser-qa.md`.
