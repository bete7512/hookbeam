# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f api/cmd/api/Dockerfile .

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./api/cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
ARG GIT_SHA=dev
LABEL org.opencontainers.image.source="https://github.com/bete7512/hookbeam" \
      org.opencontainers.image.revision="${GIT_SHA}"
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
