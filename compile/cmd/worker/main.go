// Command worker is the compile worker (slice 23, docs/compile.md): it claims compile jobs on
// the API's internal listener and runs each in its own sandbox container. Settings come from
// the environment (see .env.example, "Compile").
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/apiclient"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/sandbox"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/worker"
)

// Defaults of the settings.
const (
	defaultAPIURL      = "http://127.0.0.1:8081"
	defaultConcurrency = 2
	defaultMaxPDF      = 100 << 20
	defaultMaxInput    = 384 << 20
	minTokenLen        = 32
)

type config struct {
	apiURL, token, dockerHost, image, runtime, workerID string
	concurrency                                         int
	maxPDF, maxInput                                    int64
	logLevel                                            slog.Level
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "compile worker:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	cfg, err := load(getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))
	dc, err := docker.New(cfg.dockerHost)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dc.Ping(pctx); err != nil {
		return fmt.Errorf("the Docker daemon does not answer at %s: %w", orDefault(cfg.dockerHost, "unix:///var/run/docker.sock"), err)
	}
	ok, err := dc.ImageExists(pctx, cfg.image)
	if err != nil {
		return fmt.Errorf("check the compile image %s: %w", cfg.image, err)
	}
	if !ok {
		return fmt.Errorf("the compile image %s is not on this host; run make compile-image", cfg.image)
	}
	runner := sandbox.New(sandbox.FromClient(dc), sandbox.Config{
		Image: cfg.image, Runtime: cfg.runtime, MaxPDFBytes: cfg.maxPDF, MaxInputBytes: cfg.maxInput,
	}, logger)
	// Fail closed (NFR-4): no compile runs unless the sandbox can keep it to its files.
	probeCtx, cancelProbe := context.WithTimeout(ctx, 90*time.Second)
	landlock, err := runner.Probe(probeCtx)
	cancelProbe()
	if err != nil {
		return fmt.Errorf("refusing to start: %w", err)
	}
	logger.Info("starting compile worker", slog.String("worker_id", cfg.workerID), slog.Int("concurrency", cfg.concurrency),
		slog.String("api", cfg.apiURL), slog.String("image", cfg.image), slog.String("runtime", orDefault(cfg.runtime, "runc")), slog.String("sandbox", landlock))
	if err := worker.New(apiclient.New(cfg.apiURL, cfg.token), runner, cfg.workerID, cfg.concurrency, logger).Run(ctx); err != nil {
		return fmt.Errorf("stopped: %w", err)
	}
	logger.Info("compile worker stopped")
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func load(getenv func(string) string) (config, error) {
	var errs []error
	cfg := config{
		apiURL:     strings.TrimRight(orDefault(getenv("COMPILE_API_URL"), defaultAPIURL), "/"),
		token:      strings.TrimSpace(getenv("COMPILE_SERVICE_TOKEN")),
		dockerHost: getenv("COMPILE_DOCKER_HOST"),
		image:      getenv("COMPILE_IMAGE"),
		runtime:    getenv("COMPILE_RUNTIME"),
	}
	if len(cfg.token) < minTokenLen {
		errs = append(errs, fmt.Errorf("COMPILE_SERVICE_TOKEN must be set, at least %d characters", minTokenLen))
	}
	if cfg.image == "" {
		errs = append(errs, errors.New("COMPILE_IMAGE must name the sandbox image (make compile-image prints it)"))
	}
	switch cfg.runtime {
	case "", "runc", "runsc":
	default:
		errs = append(errs, errors.New("COMPILE_RUNTIME must be runc or runsc"))
	}
	if !strings.HasPrefix(cfg.apiURL, "http://") && !strings.HasPrefix(cfg.apiURL, "https://") {
		errs = append(errs, errors.New("COMPILE_API_URL must be an http(s) URL"))
	}
	host, _ := os.Hostname()
	cfg.workerID = orDefault(getenv("COMPILE_WORKER_ID"), orDefault(host, "worker"))
	if len(cfg.workerID) > 100 {
		cfg.workerID = cfg.workerID[:100]
	}
	var err error
	if cfg.concurrency, err = positive(getenv("COMPILE_CONCURRENCY"), defaultConcurrency); err != nil {
		errs = append(errs, fmt.Errorf("COMPILE_CONCURRENCY: %w", err))
	}
	if cfg.maxPDF, err = bytesSetting(getenv("COMPILE_MAX_PDF_BYTES"), defaultMaxPDF); err != nil {
		errs = append(errs, fmt.Errorf("COMPILE_MAX_PDF_BYTES: %w", err))
	}
	if cfg.maxInput, err = bytesSetting(getenv("COMPILE_MAX_INPUT_BYTES"), defaultMaxInput); err != nil {
		errs = append(errs, fmt.Errorf("COMPILE_MAX_INPUT_BYTES: %w", err))
	}
	switch strings.ToLower(getenv("LOG_LEVEL")) {
	case "debug":
		cfg.logLevel = slog.LevelDebug
	case "warn":
		cfg.logLevel = slog.LevelWarn
	case "error":
		cfg.logLevel = slog.LevelError
	}
	return cfg, errors.Join(errs...)
}

func positive(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errors.New("must be a whole number of at least 1")
	}
	return n, nil
}

// bytesSetting reads a size in bytes or with K, M or G (binary), as the API's settings.
func bytesSetting(raw string, def int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	mult := int64(1)
	switch raw[len(raw)-1] {
	case 'K', 'k':
		mult, raw = 1<<10, raw[:len(raw)-1]
	case 'M', 'm':
		mult, raw = 1<<20, raw[:len(raw)-1]
	case 'G', 'g':
		mult, raw = 1<<30, raw[:len(raw)-1]
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, errors.New("must be a size such as 100M")
	}
	return n * mult, nil
}
