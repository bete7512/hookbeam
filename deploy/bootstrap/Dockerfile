# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f deploy/bootstrap/Dockerfile .
# One-shot: writes the local keys and certificates the compose stack needs into /secrets (a named volume), once.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./scripts/bootstrap \
    && mkdir -p /out/secrets

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
# The volume mounted at /secrets inherits this directory's owner on first use, so the nonroot services can read it.
COPY --from=build --chown=nonroot:nonroot /out/secrets /secrets
USER nonroot:nonroot
ENTRYPOINT ["/app", "-dir", "/secrets"]
