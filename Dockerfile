FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /seed-admin ./cmd/seed-admin

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata docker-cli postgresql16-client

COPY --from=builder /api /app/api
COPY --from=builder /seed-admin /app/seed-admin
COPY --from=builder /app/uploads /app/uploads

EXPOSE 8000

CMD ["/app/api"]
