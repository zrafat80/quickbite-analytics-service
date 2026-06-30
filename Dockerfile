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

ENV ENVIRONMENT=production
ENV PORT=3000

WORKDIR /app

# 1. Copy ONLY the compiled binary from the Kitchen
COPY --from=builder /app/analytics-binary .

# 2. DocumentDB TLS
# MONGO_URL should reference tlsCAFile=global-bundle.pem relative to /app.
ADD https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem /app/global-bundle.pem
RUN chmod 0644 /app/global-bundle.pem

# 3. The Documentation
EXPOSE 3000

# 4. Drop root and turn the key (Execute the binary)
USER nobody
CMD ["./analytics-binary"]
