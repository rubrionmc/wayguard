# Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o wayguard .

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/wayguard .
COPY config.toml .

EXPOSE 25565

# Use a custom config here
CMD ["./wayguard"]
