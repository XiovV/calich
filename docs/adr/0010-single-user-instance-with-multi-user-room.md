# Single-user instance, with room to grow into multi-user

Status: superseded in part by ADR-0034 (ownership model) and ADR-0037 (account lifecycle) — the single-account rule is lifted; the bootstrap design below still stands

Each self-hosted instance supports one user account for now — no open sign-up endpoint. On first run, the backend bootstraps a default username/password that must be changed on first login, unless initial credentials are supplied via environment variables (skipping the forced-change step). The schema still models a `User` as its own record rather than a singleton config value, so multi-user support can be added later without a data-model migration — only the "single account, no invites" business rule needs to be lifted. This keeps today's scope narrow (matching how self-hosted personal calendar tools are typically deployed) while avoiding a rewrite if shared/household use is wanted later.

The default bootstrap credential is the fixed, documented value `admin` / `admin`, not a randomly generated password. This is a deliberate choice of onboarding simplicity (documented in the README, no need to check container logs to find a generated password) over eliminating the known-default window entirely — the forced password-change gate is what actually closes that window, not secrecy of the initial value.
