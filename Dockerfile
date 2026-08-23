FROM node:22-alpine AS frontend
WORKDIR /app
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile
COPY tsconfig*.json vite.config.ts index.html eslint.config.js ./
COPY public ./public
COPY src ./src
RUN yarn build

FROM golang:1.26-alpine AS backend
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
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/XiovV/calich/server/internal/version.Version=${VERSION}" -o /calich-server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=backend /calich-server /calich-server
EXPOSE 8080
ENTRYPOINT ["/calich-server"]
