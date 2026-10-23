FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/azrbnkctl ./cmd/azrbnkctl

FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    curl \
    bash \
    jq \
    bind-tools \
    iperf3

COPY --from=builder /build/bin/azrbnkctl /usr/local/bin/azrbnkctl

ENTRYPOINT ["azrbnkctl"]
CMD ["--help"]
