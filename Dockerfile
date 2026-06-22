# ==========================================
# STAGE 1: THE KITCHEN (Builder)
# ==========================================
FROM golang:1.26.3-alpine AS builder

WORKDIR /app

# 1. Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# 2. Copy the actual Go source code
COPY . .

# 3. Compile the Go app into a single standalone binary named "analytics-binary"
# CGO_ENABLED=0 ensures the binary works purely on any Linux system
RUN CGO_ENABLED=0 GOOS=linux go build -o analytics-binary ./cmd/api

# ==========================================
# STAGE 2: THE DINING ROOM (Production)
# ==========================================
FROM alpine:latest

WORKDIR /app

# 1. Copy ONLY the compiled binary from the Kitchen
COPY --from=builder /app/analytics-binary .

# 2. The Documentation
EXPOSE 5000

# 3. Turn the key (Execute the binary)
CMD ["./analytics-binary"]