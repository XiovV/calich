# Both builder stages are pinned to $BUILDPLATFORM — the machine running the
# build — rather than the platform being built for. Nothing here needs to run
# on the target: the frontend emits arch-independent static assets, and the
# backend cross-compiles (see the go build below). Without these pins a
# multi-arch `buildx` build would run the foreign leg under QEMU emulation,
# which turns a fast build into a very slow one for no benefit.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /app
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile
COPY tsconfig*.json vite.config.ts index.html eslint.config.js ./
COPY public ./public
COPY src ./src
RUN yarn build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /app
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=frontend /app/dist ./internal/static/dist
# The build label served by /api/version (#256, ADR-0072). This stage — not
# the Makefile — is what produces a release binary, so the release pipeline
# passes `--build-arg VERSION=<tag>` here. Unset, it matches the package
# default and the image honestly reports "dev".
ARG VERSION=dev
# TARGETOS/TARGETARCH are supplied by BuildKit from the platform being built
# for, so this stage cross-compiles instead of building for whatever the build
# machine happens to be — the whole reason an arm64 laptop can push an image an
# amd64 host can pull. Cheap because the backend is CGO_ENABLED=0 throughout
# (modernc.org/sqlite is pure Go), so cross-compiling needs no C toolchain.
# Both are left unset on a plain non-BuildKit `docker build`, where empty
# GOOS/GOARCH means "build native" and the result is the same as before.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-X github.com/XiovV/calich/server/internal/version.Version=${VERSION}" -o /calich-server ./cmd/server

# The release tarballs are exported from this stage rather than compiled by a
# separate `go build` (see .github/workflows/release.yml). Deliberately placed
# *before* the runtime stage: the last stage in a Dockerfile is the default
# build target, so appending it would silently turn a plain `docker build .`
# into a build of a bare binary instead of the image.
FROM scratch AS binary
COPY --from=backend /calich-server /calich-server

FROM gcr.io/distroless/static-debian12
COPY --from=backend /calich-server /calich-server
EXPOSE 8080
ENTRYPOINT ["/calich-server"]
