# Two stages: a builder with the Go toolchain, and a runtime with one static binary.

FROM golang:1.27-alpine AS build

# Alpine has no C compiler.
ENV CGO_ENABLED=0

ENV GOFLAGS=-buildvcs=false

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# The cache mounts keep the sqlc build between the image builds.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go tool sqlc generate \
    && go build -trimpath -ldflags='-s -w' -o /out/wallet ./cmd/wallet

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/wallet /wallet

ENTRYPOINT ["/wallet"]
CMD ["serve"]
