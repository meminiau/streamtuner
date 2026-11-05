# Build stage
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY . .
RUN VERSION="v1.0.$(git rev-list --count HEAD 2>/dev/null || date +%Y%m%d)" && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.appVersion=${VERSION}" -o streamtuner main.go

# Production stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata ffmpeg
WORKDIR /app
COPY --from=builder /app/streamtuner /app/streamtuner

LABEL net.unraid.docker.managed="dockerman"
LABEL net.unraid.docker.webui="http://[IP]:[PORT:8080]/"
LABEL net.unraid.docker.icon="https://raw.githubusercontent.com/meminiau/streamtuner/main/icon.png"

EXPOSE 8080
ENV PORT=8080

ENTRYPOINT ["/app/streamtuner"]
