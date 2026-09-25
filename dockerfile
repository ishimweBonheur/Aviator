FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go build -o aviator-server ./cmd/main.go
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go build -o aviator-migrate ./cmd/migrate

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /app/aviator-server .
COPY --from=builder /app/aviator-migrate .
COPY migrations ./migrations

EXPOSE 7000

CMD ["./aviator-server"]
