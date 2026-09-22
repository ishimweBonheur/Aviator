FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o aviator-server ./cmd/main.go

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /app/aviator-server .

EXPOSE 7000

CMD ["./aviator-server"]