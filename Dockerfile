# Stage 1: build the frontend. The committed dist/ already works; this
# stage rebuilds it so an image never ships a stale frontend.
FROM oven/bun:1-alpine AS frontend
WORKDIR /src
COPY package.json bun.lock ./
COPY web/frontend/package.json web/frontend/
# Extensions are bun workspaces too; the lockfile expects them.
COPY extensions/ extensions/
RUN bun install --frozen-lockfile
COPY web/frontend/ web/frontend/
RUN bun run --cwd web/frontend build

# Stage 2: build the server with the fresh frontend embedded.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/frontend/dist ./web/frontend/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Stage 3: distroless runtime, a few dozen MB, no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
