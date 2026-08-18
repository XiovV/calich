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
RUN CGO_ENABLED=0 go build -o /calich-server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=backend /calich-server /calich-server
EXPOSE 8080
ENTRYPOINT ["/calich-server"]
