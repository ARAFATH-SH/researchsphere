# The compile sandbox image (slice 23, docs/compile.md): TeX Live's full scheme, pinned by
# digest, so every package a paper commonly needs is there and nothing is ever installed at
# compile time (the sandbox has no network). Build with make compile-image.
#
# The final image is built FROM scratch: /usr of TeX Live's image (the programs, libraries
# and the TeX Live tree) and a minimal /etc of our own, whose passwd names only the compile
# user. Every compile runs under rs-landlock, which lets it read only the TeX Live tree, the
# runtime and /work and /tmp (TeX Live 2026 made openin_any a no-op: docs/decisions.md).
FROM texlive/texlive:latest-full@sha256:b20c42c2a5b1be892c26e6d3b896e9aa20350528ddd0d4444d23e5925ea1545d AS tl

# Paranoid writing and no shell escape for every TeX program, in the site texmf.cnf that
# TeX Live reads before its own (the environment below says the same).
# Formats and font maps come built in TeX Live's own texmf-var (checked below), so a compile
# never runs fmtutil or updmap. biber is a PAR-packed program that unpacks itself and runs the
# unpacked copy: it is unpacked here once, into RS_BIBER_CACHE inside the image (read and
# execute only, under Landlock), so a compile never executes anything from the noexec /tmp.
# PAR takes a lock on every start (PAR.pm, _extract_inc), even with nothing to unpack: the lock
# file is a link into /tmp, the one thing biber writes outside /work.
ENV RS_BIBER_CACHE=/usr/local/share/rs-biber
COPY texmf.cnf /tmp/rs-texmf.cnf
COPY biber-warmup /tmp/rs-biber-warmup
COPY etc/passwd etc/group etc/nsswitch.conf /rs-root/etc/
COPY latexmkrc /rs-root/etc/rs-compile/latexmkrc
RUN root="$(kpsewhich -var-value SELFAUTOPARENT)" \
 && test -n "$root" \
 && cat /tmp/rs-texmf.cnf >> "$root/texmf.cnf" \
 && rm /tmp/rs-texmf.cnf \
 && rm -rf /usr/share/doc /usr/share/man /usr/share/info \
 && mkdir -p /rs-root/etc \
 && ln -s usr/bin /rs-root/bin && ln -s usr/sbin /rs-root/sbin \
 && ln -s usr/lib /rs-root/lib && ln -s usr/lib64 /rs-root/lib64 \
 && cp -a /etc/alternatives /etc/ld.so.cache /rs-root/etc/ \
 && chmod 0755 /rs-root/etc /rs-root/etc/rs-compile \
 && chmod 0644 /rs-root/etc/passwd /rs-root/etc/group /rs-root/etc/nsswitch.conf /rs-root/etc/rs-compile/latexmkrc \
 && kpsewhich -engine=pdftex pdflatex.fmt >/dev/null \
 && kpsewhich pdftex.map >/dev/null \
 && mkdir -p "$RS_BIBER_CACHE" \
 && PAR_GLOBAL_TEMP="$RS_BIBER_CACHE" biber --version \
 && cd /tmp/rs-biber-warmup \
 && for style in authoryear numeric; do \
      pdflatex -interaction=batchmode -jobname=main "\\def\\rsstyle{$style}\\input{main}" >/dev/null \
      && PAR_GLOBAL_TEMP="$RS_BIBER_CACHE" biber --quiet main \
      && pdflatex -interaction=batchmode -jobname=main "\\def\\rsstyle{$style}\\input{main}" >/dev/null \
      && grep -q "ngstr" main.bbl || exit 1; \
    done \
 && cd / && rm -rf /tmp/rs-biber-warmup \
 && test -x "$RS_BIBER_CACHE/biber" && test -d "$RS_BIBER_CACHE/inc" \
 && ln -sfn /tmp/rs-biber-inc.lock "$RS_BIBER_CACHE/inc.lock" \
 && chmod -R a+rX,go-w "$RS_BIBER_CACHE"

FROM golang:1.26.8@sha256:eb36c1664dd974cde625f736e02c204383deebe03977365caaec5bf49f794348 AS landlock
WORKDIR /src
COPY landlock/main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rs-landlock main.go

FROM scratch
COPY --from=tl /usr /usr
# The only things outside /usr the image keeps: the usrmerge links (/bin -> usr/bin and so
# on), /etc/alternatives (links only), the loader's cache, the fixed latexmkrc and a minimal
# passwd, group and nsswitch.conf (compile/image/etc: the compile user only, no root).
COPY --from=tl /rs-root/ /
COPY --chmod=0755 --from=landlock /rs-landlock /usr/local/bin/rs-landlock
COPY --chmod=0755 rs-compile /usr/local/bin/rs-compile
# Mount points (WORKDIR creates them): the worker mounts memory-backed /work and /tmp here.
WORKDIR /tmp

ENV PATH=/usr/local/bin:/usr/bin \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    HOME=/tmp \
    TEXMFVAR=/tmp/texmf-var \
    TEXMFCACHE=/tmp/texmf-var \
    PAR_GLOBAL_TEMP=/usr/local/share/rs-biber \
    TEXMFCONFIG=/tmp/texmf-config \
    TEXMFHOME=/tmp/texmf \
    TEXMFOUTPUT=/work \
    openin_any=p \
    openout_any=p \
    shell_escape=f \
    max_print_line=10000 \
    error_line=254 \
    half_error_line=238

USER 10001:10001
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/rs-landlock", "--", "/usr/local/bin/rs-compile"]
