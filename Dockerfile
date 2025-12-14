FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /catty-exec-runtime ./cmd/catty-exec-runtime

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

# Copy binary
COPY --from=builder /catty-exec-runtime /usr/local/bin/

EXPOSE 8080

CMD ["catty-exec-runtime"]
