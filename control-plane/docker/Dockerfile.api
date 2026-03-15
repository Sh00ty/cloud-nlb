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
    CGO_ENABLED=0 go build -v -o ./api ./cmd/api

FROM alpine:latest

COPY --from=builder /app/api .

EXPOSE 8080 9090

ENTRYPOINT ["./api"]