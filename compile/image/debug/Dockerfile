# The compile sandbox image plus strace, for `make compile-trace` only (docs/compile.md,
# "Tracing a compile"). Never used by the worker: it exists to list every path a compile opens
# and Landlock refuses. strace runs outside the Landlock domain (it starts rs-landlock), so it
# sees each refusal as EACCES on the traced call.
ARG BASE=researchsphere-compile:dev
FROM texlive/texlive:latest-full@sha256:b20c42c2a5b1be892c26e6d3b896e9aa20350528ddd0d4444d23e5925ea1545d AS strace
RUN apt-get update \
 && apt-get install -y --no-install-recommends strace \
 && mkdir -p /out \
 && cp --parents /usr/bin/strace /out/ \
 && ldd /usr/bin/strace | awk '/=> \/usr\// { print $3 }' | xargs -I{} cp --parents -L {} /out/ \
 && ldconfig && cp --parents /etc/ld.so.cache /out/

FROM ${BASE}
# strace, its libraries, and the loader's cache that lists them (same base, so it is a superset).
COPY --from=strace /out/ /
