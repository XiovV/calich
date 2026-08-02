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
RUN CGO_ENABLED=0 go build -o /calendar-server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=backend /calendar-server /calendar-server
# Frontend static assets are not yet served (embedding into the Go binary is #16) —
# copied here so the frontend build stage is part of this image's build graph.
COPY --from=frontend /app/dist /dist
EXPOSE 8080
ENTRYPOINT ["/calendar-server"]
