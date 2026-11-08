# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f examples/receiver/Dockerfile .

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./examples/receiver \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
# The state file lives in a volume mounted here; the directory's owner lets the nonroot process write it.
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
ENV RECEIVER_STATE_FILE=/data/receiver-state.json
EXPOSE 8081
ENTRYPOINT ["/app"]
