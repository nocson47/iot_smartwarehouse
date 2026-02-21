FROM golang:1.24.2-bullseye
WORKDIR /app

# Install sqlite dev for cgo (go-sqlite3) and build tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libsqlite3-dev \
    ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build with cgo enabled for sqlite3
RUN CGO_ENABLED=1 GOOS=linux go build -o server .

EXPOSE 5000
CMD ["./server"]
