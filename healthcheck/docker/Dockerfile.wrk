FROM golang:latest AS builder

WORKDIR /app

RUN export GO111MODULE=on

COPY ./go.mod ./
COPY ./go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -v -o ./processor ./cmd/processor

FROM alpine:latest

COPY --from=builder /app/processor .

EXPOSE 8080 9000

ENTRYPOINT ["./processor"]