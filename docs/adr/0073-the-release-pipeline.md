# The release pipeline publishes to Docker Hub, and lifts its binaries out of the image

Status: accepted

Publishing a GitHub Release runs the tests, builds one multi-arch image, pushes it to Docker Hub, and attaches the pre-compiled binaries the README promises. Two workflows exist: `ci.yml` on every push and pull request, and `release.yml` on `release: published`.

## Publication is a second act, not a tag push

The trigger is `release: published` rather than `push: tags`. A tag push makes the tag itself the trigger, so a typo or a premature `git push --tags` publishes an image before any release note is written, and deleting the tag locally does not unpublish it. Publishing a Release is a deliberate act taken after the notes are drafted.

The trigger also decides how `latest` behaves. `github.event.release.prerelease` — the "set as a pre-release" checkbox — is a machine-readable answer to "is this a production release", which a tag push would force the pipeline to guess by string-sniffing. That guess is wrong the first time someone tags `v1.0.0-hotfix`.

## Docker Hub, not GHCR

The README previously documented `ghcr.io/xiovv/calich`, and GHCR is the cheaper option on the merits: `GITHUB_TOKEN` authenticates to it with no secret to manage, and it does not rate-limit anonymous pulls the way Docker Hub does. Docker Hub was chosen anyway, because it is where a self-hoster looks first and `docker pull xiovv/calich` is the instruction people expect. The cost is two repository secrets (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`) and Docker Hub's anonymous pull limits, both accepted. All three README references were updated; nothing was ever published to GHCR, so no one was pinned to it.

## The image tag keeps its `v`

A production `v0.1.0` publishes `v0.1.0` and `latest`. Docker's own ecosystem convention strips the prefix (`postgres:17.2`), and `docker/metadata-action`'s `type=semver,pattern={{version}}` strips it by default.

It is kept because ADR-0072 has the version badge display the label verbatim. A user reading `v0.1.0` in their top bar should be able to `docker pull xiovv/calich:v0.1.0` with no translation step. One string runs from the git tag through the `-ldflags` injection to the badge to the image tag.

For the same reason the tags are built with `type=raw` rather than `type=semver`. ADR-0072 holds that the label is never parsed or validated; `type=semver` would impose exactly the validation that ADR rejected, and would fail outright on a tag that is not valid semver.

No moving `v0.1` or `v0` tags. Pre-1.0, `v0` is meaningless because every minor may break, and `v0.1` is a patch-compatibility promise not yet made. Both are cheap to add later and awkward to withdraw once someone has pinned to them.

## Prereleases publish an image, but never `latest`

A prerelease builds and pushes its version tag and stops there. Skipping prereleases entirely was rejected: for an app whose primary distribution *is* the image, a prerelease nobody can install is a changelog entry, and the point of cutting `v0.2.0-rc.1` is being able to tell a reporter to pull it.

`latest` is withheld on either of two independent signals — the GitHub prerelease checkbox, or a hyphen in the tag. The checkbox is authoritative, but a `v1.0.0-rc.1` published with the box left unticked is a mistake worth absorbing rather than propagating to everyone who pulls `latest`, which is what every copy-paste `docker run` line in the README resolves to.

## Multi-arch is nearly free, so it is not optional

`linux/amd64,linux/arm64`. The Dockerfile pins both builder stages to `$BUILDPLATFORM` and cross-compiles the backend via `TARGETOS`/`TARGETARCH`, so nothing runs under QEMU and BuildKit resolves the frontend stage to a single node shared by both targets — `yarn install` and `yarn build` execute once, and the second architecture costs one additional `CGO_ENABLED=0 go build`.

arm64 is not a rounding error for this project: Raspberry Pi on a 64-bit OS, Oracle's free Ampere tier, AWS Graviton, and Docker Desktop on Apple Silicon are all ordinary places to self-host a calendar. `linux/arm/v7` was left out — a third leg, and 32-bit time handling in a calendar application is not a path worth shipping untested.

## The binaries are exported from the image, not compiled separately

`server/internal/static/static.go` does `//go:embed all:dist`, and the repo tracks a placeholder `server/internal/static/dist/index.html` reading "frontend not built" so that `go build` and `go test` work without a frontend present. **Only the Dockerfile overwrites that directory.** `make build-backend` does not, and neither does `make build`.

So the obvious way to produce the tarballs — `go build` with the right ldflags, then `tar` — yields a binary that starts, serves the API, answers `/api/version` with the correct label, passes every check worth writing, and renders "frontend not built" to every user.

Instead, a `FROM scratch AS binary` stage carries the compiled binary out, and the release job exports it with `--target binary --output type=local`. This is structurally incapable of the placeholder bug because it is the same binary the container runs; it reuses the image build's cache, so it costs approximately nothing; and it makes the tarball and the image byte-identical, which retires "works in Docker but not standalone" as a category of bug.

That stage sits **before** the runtime stage on purpose. The last stage in a Dockerfile is the default build target, so appending it would quietly turn `docker build .` into a build of a bare binary.

Considered and rejected: copying `dist/` inline in the workflow (a CI-only code path that rots unobserved), and a Makefile target doing the copy (the placeholder `index.html` is tracked, so any contributor running it acquires a permanently dirty working tree).

## Lint gates pull requests; only tests gate a release

Both workflows run the same two suites. `ci.yml` additionally runs `make lint-backend`, `make lint-frontend`, and `make fmt`; `release.yml` deliberately does not.

By the time the release gate runs, the tag exists and the Release is public, so a failure there is recovered by deleting a public release or burning a version number. That is a proportionate response to a failing test and an absurd one to a `gofmt` complaint. Style belongs where fixing it is free.

`make fmt` was changed as part of this work: `gofmt -l` prints offending file names and exits 0, so as a gate it passed unconditionally.

## Known gaps, accepted

- **The ldflag chain is still unverified end-to-end.** ADR-0072 names the fix — the pipeline asserting the flag before publishing — and this pipeline does not implement it. It checks only that the release carries a non-empty tag. A `-X` whose symbol path stops matching `internal/version.Version` after a refactor links silently, and ships an image whose badge reads `dev`. Closing this needs a boot-and-curl smoke test between build and push, which was scoped out. ADR-0072's *Known gap* section is amended to say so rather than implying the pipeline closed it.
- **Nothing ever starts the image before users do.** The same missing smoke test is the only thing that would prove distroless has what the binary needs, that the frontend really got embedded, and that `/data` is writable. Unit tests test packages, not artifacts.
- **Re-running a release overwrites its image tags.** Only a digest is truly immutable. The workflow is serialised per tag so two runs cannot race, but a deliberate re-run replaces what was there.
- **`latest` can move backwards.** Publishing a patch for an older line after a newer minor is out would point `latest` at the older code. Pre-1.0 with no maintenance branches this cannot yet arise; it becomes real the first time it does.
