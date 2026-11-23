# Compile contract

How a LaTeX compile runs (FR-13.1, FR-13.2, FR-13.4 to FR-13.7, NFR-3, NFR-4; slice 23). The
API never runs LaTeX and never talks to Docker. It queues jobs; the **compile worker**
(`compile/`, a separate Go program) claims them on the API's internal listener and runs each in
a **sandbox container** of its own. `docs/openapi.yaml` holds the operations (tags `compile`,
`project-files` for the archive, `internal` for the worker's), `docs/decisions.md` the reasons.

```
browser ──POST /compiles──▶ API ──compile_jobs (the queue)──┐
   ▲                         ▲   └─ flush ─▶ sync server     │
   │ compile.updated         │ claim / source / pdf / finish │ (internal listener,
   └──────── realtime ───────┘◀───────── compile worker ◀────┘  COMPILE_SERVICE_TOKEN)
                                               │ Docker socket (the worker only)
                                               ▼
                                   one sandbox container per compile
                                   (files in on stdin, PDF and logs out on stdout)
```

## Jobs and their states

A job is a row of `compile_jobs` (migration 000038); the table is the queue, so no job lives
only in Redis.

| Status | Meaning |
| --- | --- |
| `queued` | Waiting for a worker. At most one queued or running job per project (FR-13.4): a partial unique index, so the refusal (409 `compile_running`, with the job's ID) is the database's. |
| `running` | A worker claimed it (`FOR UPDATE SKIP LOCKED`, oldest first) and holds a **lease of 120 s**: the 60 s limit, reading the files and storing the PDF. |
| `succeeded` | **A PDF was made**, even when the log reports errors: they are in `errors`. |
| `failed` | **No PDF was made.** `failure` says why when the log does not: `no_pdf` (LaTeX ended without one), `input_too_large`, `output_too_large`, `out_of_memory`, `sandbox_error`, `worker_lost` (the lease ended). |
| `timed_out` | Stopped at the time limit (FR-13.5). No PDF is kept. |
| `cancelled` | No worker took it within 5 minutes (`failure: queue_expired`). |

The API sweeps every 30 s: running jobs past their lease fail as `worker_lost`, and queued jobs
older than 5 minutes are cancelled, so a stopped worker never holds a project's slot.

**Starting** (`POST /projects/{id}/compiles`, editors and the owner): 10 a minute per user
(`compile:user:<id>`, fails open); 409 `main_file_missing` when no text file is at the main
file's path; 503 `compile_unavailable` when no worker asked for work in the last 30 s (the
worker's claims keep `compile:worker:seen` alive), so a stopped compile service answers at once
instead of queueing (NFR-10.2); 503 `compile_busy` above `COMPILE_QUEUE_MAX` queued jobs.

**Main file and engine.** The owner sets `mainFilePath` with `PATCH /projects/{id}` (an existing
text file ending in `.tex`); it follows renames (slice 21). The job copies it, so a later change
does not affect a running compile. `projects.compile_engine` and `compile_jobs.engine` allow only
`pdflatex` for now; XeLaTeX is an open item (`docs/decisions.md`).

## Retention (SRS 7)

The log is kept for the **newest finished compile** of a project only; the PDF for the **newest
compile that succeeded** only, so a failed compile never takes the last good PDF away; at most
**10 compile records** a project, plus the one holding the PDF. The PDF is a `stored_files` row
owned by the project (purpose `pdf`), so it counts in the project's storage (FR-12.2) and a
trigger deletes it, which queues its object, when its job goes or another PDF replaces it.

## The worker's operations

On `INTERNAL_API_ADDR` only, with `Authorization: Bearer COMPILE_SERVICE_TOKEN`. The sync
server's token is refused on them and theirs on the sync server's (`httpx.RequireServiceTokens`):
each service can do only its own work.

| Operation | What it does |
| --- | --- |
| `POST /internal/compile/claim` `{workerId}` | Waits up to 10 s for a queued job; 200 with `{id, projectId, mainFilePath, engine}` (now `running`, leased) or 204. |
| `GET /internal/compile/jobs/{id}/source` | The project's files as a POSIX tar: folders, text files, and figures whose scan says `clean`. Pending, failed and infected figures are left out and listed in the job's `skipped`. Before reading, the flush below. 409 `input_too_large` over `COMPILE_MAX_INPUT_BYTES` (the job has failed). |
| `PUT /internal/compile/jobs/{id}/pdf` | The PDF, at most `COMPILE_MAX_PDF_BYTES` (413), starting with `%PDF-` (400). Spooled to a file, then stored under `projects/{projectId}/compiles/{jobId}-{random}.pdf`. |
| `POST /internal/compile/jobs/{id}/finish` | `{outcome, failure?, exitCode?, durationMs, log, bibtexLog}`. The API parses the errors, decides the status, keeps what SRS 7 says and tells the members. A second report is ignored; a job that ended as `worker_lost` answers 404. |

All of them answer 404 unless the job is running under a lease.

**Live text.** A compile must see what members typed. When a text file of the project is open
on the sync server (`sync:live:<fileId>`), the source operation first publishes a `flush`
(`docs/sync.md`) and waits up to 5 s for its answer, then reads the text the sync server stored.
Without an answer it compiles the stored text, at most 4 s old (NFR-10.1), and the job says
`liveTextConfirmed: false`. Nothing outside the sync server decodes Yjs (`docs/decisions.md`
2026-11-17).

## The sandbox (NFR-4)

One container per compile, from `compile/image` (`make compile-image`): TeX Live 2026's full
scheme, pinned by digest, so no package is ever installed at compile time. The image is built
`FROM scratch` with TeX Live's `/usr` and a minimal `/etc` of its own (`compile/image/etc`:
`passwd` and `group` name only the compile user, no root; the loader's cache; the fixed
latexmkrc); it has no `/home`, `/root` or `/var`. Settings
(`compile/internal/sandbox`, `Runner.Spec`; each is checked by a test):

| Setting | Value |
| --- | --- |
| Network | `none` (only `lo`; checked by running a probe in the same configuration) |
| Root filesystem | read-only |
| Files (Landlock) | every compile runs under `rs-landlock` (`compile/image/landlock`), which restricts itself with Landlock (ABI 8 or later) before anything else runs: read and execute only `/usr/bin`, `/usr/lib`, `/usr/lib64`, `/usr/share/perl`, `/usr/share/perl5`, `/usr/local/bin`, `/usr/local/texlive` (the whole TeX Live tree, its `texmf-var` and `texmf-config` included) and `/usr/local/share/rs-biber` (biber, below); read only `/etc/ld.so.cache`, `/etc/rs-compile` and `/dev/urandom` (Perl seeds from it); read and write, never execute, `/work` and `/tmp`; read and write `/dev/null`; everything else, the rest of `/etc`, `/proc`, `/home` and `/sys` included, is refused; no TCP bind or connect; no signals or abstract unix sockets outside the domain. **Fails closed**: without Landlock the container exits 78 before LaTeX runs, and the worker does not start (below) |
| User | 10001:10001, not root |
| Capabilities, privileges | all dropped; `no-new-privileges`; Docker's default seccomp and AppArmor profiles; never privileged |
| CPU, memory | 1 CPU; 1 GiB without swap |
| Processes | 64 |
| `/work` | memory-backed (tmpfs), 768 MiB, `noexec,nosuid,nodev`; its pages count against the 1 GiB |
| `/tmp` | tmpfs, 64 MiB, `noexec,nosuid,nodev` |
| Host mounts | **none** |
| Logs | log driver `none`, so the PDF never lands in the daemon's log files |
| Other | IPC `none`, no core dumps, 1,024 open files, an init process, `runsc` when `COMPILE_RUNTIME` says so |
| Time | the entrypoint stops LaTeX at 55 s (its log still comes out); the worker kills the container at **60 s** whatever happens (FR-13.5) |

**Files in and out.** The worker streams the source tar into the container's stdin, checking
every entry again on the way (folders and regular files only, clean relative paths, no `..`,
no name starting with `.`, no links, devices or owners). The entrypoint (`rs-compile`) unpacks
it into `/work`, runs latexmk from the project's root, and writes a tar of `status`,
`output.pdf`, `output.log` and `output.blg` to stdout, freeing `/work` first so a document that
filled it still gets its log out. The worker reads that tar with the same caps (PDF
`COMPILE_MAX_PDF_BYTES`, log 2 MiB, BibTeX log 256 KiB) and removes the container whatever
happened: success, failure, kill or a crash of its own (a sweep at start and every minute
removes containers labelled `org.researchsphere.compile` older than 3 minutes). Nothing is
written on the host: there is no temporary folder to remove.

**LaTeX.** `latexmk -norc -r /etc/rs-compile/latexmkrc`: pdfLaTeX with `-no-shell-escape
-interaction=nonstopmode -file-line-error`, BibTeX when the `.bib` files say so, up to 5
passes, going on after errors so every error is in the log. `-norc` matters: a project may
have a file named `latexmkrc`, which latexmk would otherwise run as Perl. The site
`texmf.cnf` (and the image's environment) sets `shell_escape = f`, `openout_any = p` (no
writing by absolute path or into a parent folder; TeX Live 2026 refuses every absolute path,
even under `TEXMFOUTPUT = /work`, which is set too), and `max_print_line = 10000` (unwrapped
log lines).

**What TeX writes.** Only `/work` (the project and its outputs, `filecontents` included) and
`/tmp`: fonts that exist only as METAFONT source are generated by mktexpk into `TEXMFVAR`
(`/tmp/texmf-var`; `TEXMFCACHE` points there too) and its temporary folders. Formats and font
maps are built in TeX Live's own `texmf-var` when the image is built (the Dockerfile checks
`pdflatex.fmt` and `pdftex.map`), so a compile never runs fmtutil or updmap.

**biber.** biber is a PAR-packed program: it unpacks itself and executes the unpacked copy,
which `/tmp` (noexec, and not executable under Landlock) refuses. The image unpacks it at build
time into `/usr/local/share/rs-biber` (`PAR_GLOBAL_TEMP`), running it on a warm-up document
(`compile/image/biber-warmup`, two styles) so the libraries PAR unpacks only when first used are
there too; PAR still takes a lock on every start, so `inc.lock` there is a link to
`/tmp/rs-biber-inc.lock`. **Limit:** a biber feature that loads a library the warm-up did not
fails with "Read-only file system"; `make compile-trace` shows which, and the warm-up document is
where to add it.

**Tracing a compile.** `make compile-trace dir=<folder>` (development only) runs a folder with
the worker's container settings in `researchsphere-compile:debug`, the image plus strace
(`compile/image/debug`), and lists every path refused, every write outside `/work` and `/tmp`,
the writes under `/tmp` and the devices used. strace starts `rs-landlock`, so it stays outside
the Landlock domain and sees each refusal. Every compile shows the same harmless refusals: `tar`
and coreutils look up `/etc/passwd`, `/etc/group` and `/etc/nsswitch.conf` (user names) and
read `/proc/filesystems` and `/proc/mounts` (an SELinux check), and fall back silently. The
corpus `compile/testdata/corpus` (`TestSandboxCorpus`) must keep compiling.

**Reading files.** `openin_any = p` is set too, but it is a **no-op in TeX Live 2026**: its own
`texmf-dist/web2c/texmf.cnf` says "as of 2026, openin_any no longer has any effect; the
underlying kpse_in_name_ok and related functions always return true" (`docs/decisions.md`).
No TeX setting restricts `\input`, `\openin`, `\includegraphics`, `\lstinputlisting` or the
pdfTeX file primitives, so reads are restricted below TeX, by Landlock (above): a compile opens
only its project in `/work`, `/tmp` (TeX's font cache) and the public TeX Live tree and runtime;
`/etc/passwd`, `/proc/self/environ` and any `../` path out of `/work` give `Permission denied`.
The minimal `/etc` is a second layer: even without Landlock, the image's passwd has no root
line. **Limit:** Landlock restricts opening a file, not `stat`, so `\IfFileExists` and
`\pdffilesize` still learn whether one of the image's files exists and its size; never its
content.

**Fail closed.** The worker runs `rs-landlock --probe` in a compile container before it claims
any job (`Runner.Probe`: restrict, check that `/etc/passwd` cannot be opened and `/work` can be
written) and refuses to start if it fails; with no worker polling, `POST
/projects/{id}/compiles` answers 503 `compile_unavailable` (NFR-10.2). If a run reports the
refusal later, the worker fails that job (`sandbox_error`) and exits. `make check` runs the
same probe. gVisor's `runsc` is not known to implement Landlock: with `COMPILE_RUNTIME=runsc`
the probe would refuse to start (not verified here; runsc is not installed).

## Errors (FR-13.2)

The API parses the logs (`internal/compile/usecase/logparse.go`) into at most 100
`{file, line, message, severity}`, errors first: `file:line:` errors, `!` errors with their
`l.N` line, LaTeX, class and package warnings with their `on input line N` and the file TeX was
reading, overfull boxes, and BibTeX's `---line N of file x.bib` errors and warnings. The log is
the document's to write (`\typeout` prints anything), so a file is reported only when it is one
of the project's text files, and messages are cut to 500 characters.

## Downloads

**PDF** (`GET /projects/{id}/pdf`, members): streamed by the API, never through a storage URL,
because these headers must come with it: `Content-Type: application/pdf`,
`Content-Disposition: inline` (the viewer) or `attachment` (the default),
`X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'; …`,
`Cache-Control: private, no-store`, `Cross-Origin-Resource-Policy: same-origin`. A PDF can hold
JavaScript; the generated PDF is not scanned for malware (its inputs were), and these headers
keep a browser from running anything in the app's origin. The workspace's viewer (23.2,
`frontend/src/features/project/workspace/compile/pdf/load-pdf.ts`) reads the PDF through the API
client (`disposition=inline`) and draws each page on a canvas with pdf.js, at the screen's pixel
density and only near the visible part, with pdf.js's text layer over it (transparent text for
selecting and finding; it runs nothing; `docs/decisions.md`, 2026-11-24): no annotation layer
(`AnnotationMode.DISABLE`: no links, form fields or JavaScript actions), no `PDFViewer` and no
scripting sandbox, no XFA. pdf.js 6 has no `isEvalSupported` option because it no longer
evaluates code; a test checks its build has no `eval` (`docs/decisions.md`). Both downloads are
fetched through the API client as a blob and then saved, so a lapsed session is refreshed and a
refusal (`no_pdf`, `archive_too_large`, `rate_limited`) is said in words.

**ZIP** (`GET /projects/{id}/archive`, FR-13.7, members): streamed while it is written, text
compressed and figures stored as they are, clean figures only; 10 an hour per user; refused
over `ARCHIVE_MAX_BYTES`.

## Threat model

**Assets:** other projects' files and PDFs; the host and the secrets of the API, the database
and storage; the service's availability; the integrity of a project's PDF.
**Attackers:** any editor (anyone can create a project and become its owner), so the LaTeX is
hostile; a leaked compile token; a compromised compile host.

| Threat | Defence | Test |
| --- | --- | --- |
| Commands through `\write18` or `\input\|"cmd"` | `-no-shell-escape`, `shell_escape = f`; no network anyway | `TestSandboxEscapesFail`: shell escape, piped input |
| Perl through a project `latexmkrc` | `latexmk -norc` with a fixed rc outside `/work` | `TestSandboxEscapesFail`: project latexmkrc |
| Reading other files by path (`openin_any` is a no-op) | Landlock (`rs-landlock`) allows reads of `/work`, `/tmp` and the TeX Live tree and runtime only; minimal `/etc` without root; fail closed | `TestSandboxReadsOutsideWorkFail`: `\input`, `\openin`, `\includegraphics`, `\pdfximage`, `\lstinputlisting`, `\verbatiminput`, `\pdffiledump`, `\pdfmdfivesum`, `\IfFileExists` × `/etc/passwd`, `/proc/self/environ` and `../` paths, nothing of them in the log or the PDF's text; `TestSandboxReadControl` proves the same commands do find a project file |
| Writing outside the project | `openout_any = p`; Landlock allows writes in `/work` and `/tmp` only; read-only root, non-root user | `TestSandboxWritesOutsideWorkFail`: `/tmp`, `/etc`, `../`, `/work/../tmp`, `/proc/self`, the TeX Live tree, each read back |
| The settings silently not applied | checked inside the sandbox, not on a command line | `TestSandboxSettings`: kpsewhich values, the Landlock probe, a shell under Landlock, the minimal `/etc` |
| A host without Landlock | the worker does not start; a refused run stops it | `TestProbe`, `TestUnavailableSandboxStopsTheWorker`, `TestRunOutcomesAndCleanup`: sandbox unavailable |
| Exfiltration over the network | network `none` | `TestSandboxHasNoNetwork` |
| A compile that never ends (`\loop`) | 55 s in the entrypoint, 60 s hard kill | `TestSandboxTimeouts` (both) |
| Memory, processes, disk | 1 GiB without swap (the tmpfs counts), 64 processes, size-capped tmpfs, no host mount | `TestSandboxOutputCaps`: write bomb |
| Huge output | caps in the entrypoint, the worker (authoritative) and the API | `TestSandboxOutputCaps`, `TestRunOutcomesAndCleanup`, `TestStorePDF` |
| Leftover containers | removal after every run, sweep at start and every minute | every sandbox test checks none is left; `TestSweepRemovesOldContainers` |
| Tar tricks (links, `..`, devices) | the API builds the tar from checked paths; the worker checks every entry again | `TestSanitize` |
| Unscanned or infected figures | only `clean` figures go into a compile or an archive | `TestCompileFlow`, `TestProjectArchive`, `TestSourceSkipsUncleanFigures` |
| Cross-project access by a job | job-scoped operations, a running lease, the API resolves paths | `TestSourceNeedsARunningJob`, `TestCompileLeaseExpiry` |
| A leaked token | separate token per service; internal listener only (the public port answers 404); constant-time check | `TestRequireServiceTokens`, `TestCompileWorkerRoutesNeedTheirToken` |
| The Docker socket (root on its host) | only the worker has it, never the API; it has no other secret than its token | `TestOnlyTheWorkerGetsTheDockerSocket` |
| Spoofed errors in the log | only the user's own list; files must be project paths; capped | `TestParseLogsUntrustedInput` |
| Queue flooding | one compile per project, 10 a minute per user, `COMPILE_QUEUE_MAX`, 5-minute expiry | `TestStartRefusals`, `TestSweep` |
| Scripts in a PDF | served with `sandbox` CSP and `nosniff`; pdf.js without scripting in 23.2 | `TestCompileRoutes`, `TestCompileFlow` |

**Residual risk.** A container shares the host's kernel under `runc`: a kernel bug could let a
document escape. Defences in depth reduce it (non-root, no capabilities, seccomp, AppArmor,
no-new-privileges, read-only root, no network), and production should add a **dedicated compile
host with rootless Docker and gVisor's `runsc`**, so an escape reaches neither root nor the
other services. A socket proxy that filters API calls is not a boundary: it still lets its
caller create a privileged container. Landlock and runsc may not combine (above). Whether a
public image file exists, and its size, can be learnt (above); its content cannot.

## Running it

| Command | What it does |
| --- | --- |
| `make compile-image` | Builds the sandbox image |
| `make compile-worker` | Runs the worker on this host with your Docker (needs `make api` with `COMPILE_SERVICE_TOKEN`) |
| `make test-compile` | The worker's tests with a fake Docker daemon (in `make test`) |
| `make test-compile-sandbox` | The sandbox tests against real Docker and the image (about a minute; CI nightly, by hand and on compile changes) |
| `make bench-compile` | NFR-3: the 10-page sample, timed (`docs/evaluation.md`) |
| `docker compose --profile compile up compile-worker` | The worker as a container, with the socket mounted into it alone |
