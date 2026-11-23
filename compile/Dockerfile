# The compile worker (slice 23, docs/compile.md): one static Go binary on an empty image. It
# holds the Docker socket, which is root on its host, so it runs only where compiles run and
# nothing else: in production a dedicated compile host with rootless Docker (and gVisor's
# runsc where it is installed). Build: docker compose --profile compile build compile-worker.

FROM golang:1.26.8 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /worker ./cmd/worker

FROM scratch
COPY --from=build /worker /worker
# Not root: the socket's group (DOCKER_GID in docker-compose.yml) gives it the daemon.
USER 65532:65532
STOPSIGNAL SIGTERM
ENTRYPOINT ["/worker"]
