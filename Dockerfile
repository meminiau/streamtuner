# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o streamtuner main.go

# Production stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/streamtuner /app/streamtuner

EXPOSE 8080
ENV PORT=8080

ENTRYPOINT ["/app/streamtuner"]
