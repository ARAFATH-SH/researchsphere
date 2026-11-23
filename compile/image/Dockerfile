# The compile sandbox image (slice 23, docs/compile.md): TeX Live's full scheme, pinned by
# digest, so every package a paper commonly needs is there and nothing is ever installed at
# compile time (the sandbox has no network). Build with make compile-image.
FROM texlive/texlive:latest-full@sha256:b20c42c2a5b1be892c26e6d3b896e9aa20350528ddd0d4444d23e5925ea1545d

# Paranoid file access and no shell escape for every TeX program, in the site texmf.cnf that
# TeX Live reads before its own (the environment below says the same).
COPY texmf.cnf /tmp/rs-texmf.cnf
RUN root="$(kpsewhich -var-value SELFAUTOPARENT)" \
 && test -n "$root" \
 && cat /tmp/rs-texmf.cnf >> "$root/texmf.cnf" \
 && rm /tmp/rs-texmf.cnf \
 && groupadd --gid 10001 compile \
 && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /tmp --shell /usr/sbin/nologin compile \
 && mkdir -p /etc/rs-compile /work \
 && chown 10001:10001 /work

COPY --chmod=0755 rs-compile /usr/local/bin/rs-compile
COPY --chmod=0644 latexmkrc /etc/rs-compile/latexmkrc

ENV HOME=/tmp \
    TEXMFVAR=/tmp/texmf-var \
    TEXMFCONFIG=/tmp/texmf-config \
    TEXMFHOME=/tmp/texmf \
    openin_any=p \
    openout_any=p \
    shell_escape=f \
    max_print_line=10000 \
    error_line=254 \
    half_error_line=238

USER 10001:10001
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/rs-compile"]
