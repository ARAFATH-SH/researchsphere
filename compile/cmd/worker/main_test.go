package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The Docker socket is root on its host (docs/compile.md, Threat model): only the compile
// worker may mount it, never the API or any other service of docker-compose.yml.
func TestOnlyTheWorkerGetsTheDockerSocket(t *testing.T) {
	b, err := os.ReadFile("../../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	service := regexp.MustCompile(`^  ([a-z0-9-]+):\s*$`)
	current := ""
	inServices := false
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "services:"):
			inServices = true
			continue
		case len(line) > 0 && line[0] != ' ' && line[0] != '#':
			inServices = false
		}
		if !inServices {
			continue
		}
		if m := service.FindStringSubmatch(line); m != nil {
			current = m[1]
		}
		if strings.Contains(line, "docker.sock") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			if current != "compile-worker" {
				t.Errorf("service %q mounts the Docker socket: %s", current, strings.TrimSpace(line))
			}
			found = true
		}
	}
	if !found {
		t.Error("the compile worker does not mount the Docker socket; the test no longer looks in the right place")
	}
}

func TestLoad(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tok := strings.Repeat("t", 32)
	cfg, err := load(env(map[string]string{"COMPILE_SERVICE_TOKEN": tok, "COMPILE_IMAGE": "img@sha256:x", "COMPILE_MAX_PDF_BYTES": "50M"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.apiURL != defaultAPIURL || cfg.concurrency != defaultConcurrency || cfg.maxPDF != 50<<20 || cfg.maxInput != defaultMaxInput {
		t.Errorf("config %+v", cfg)
	}
	for name, vars := range map[string]map[string]string{
		"no token":    {"COMPILE_IMAGE": "img"},
		"short token": {"COMPILE_SERVICE_TOKEN": "short", "COMPILE_IMAGE": "img"},
		"no image":    {"COMPILE_SERVICE_TOKEN": tok},
		"runtime":     {"COMPILE_SERVICE_TOKEN": tok, "COMPILE_IMAGE": "img", "COMPILE_RUNTIME": "kata"},
		"concurrency": {"COMPILE_SERVICE_TOKEN": tok, "COMPILE_IMAGE": "img", "COMPILE_CONCURRENCY": "0"},
		"size":        {"COMPILE_SERVICE_TOKEN": tok, "COMPILE_IMAGE": "img", "COMPILE_MAX_PDF_BYTES": "lots"},
		"api url":     {"COMPILE_SERVICE_TOKEN": tok, "COMPILE_IMAGE": "img", "COMPILE_API_URL": "127.0.0.1:8081"},
	} {
		if _, err := load(env(vars)); err == nil {
			t.Errorf("%s: loaded", name)
		} else if strings.Contains(err.Error(), tok) {
			t.Errorf("%s: the error repeats the token", name)
		}
	}
}
