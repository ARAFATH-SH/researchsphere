package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// GitHub endpoints (tests point them at fake servers).
const (
	DefaultGitHubAPI = "https://api.github.com"
	DefaultGitHubRaw = "https://raw.githubusercontent.com"
)

// Limits of a repository read.
const (
	maxAPIBytes   = 4 << 20 // a commit, a comparison or a tree
	maxFileBytes  = 1 << 20 // one data file (the database's limit too)
	maxCompare    = 300     // GitHub lists at most 300 files in a comparison
	maxDataFiles  = 2000
	githubAPIType = "application/vnd.github+json"
)

// Format is the layout of a repository's data files.
type Format string

// Formats.
const (
	// FormatCCF: conference/<area>/<series>.yml, a list of series each with its editions
	// under confs (github.com/ccfddl/ccf-deadlines).
	FormatCCF Format = "ccf"
	// FormatHCI: conferences/<series>.yml, a list of editions (github.com/hci-deadlines/conf-database).
	FormatHCI Format = "hci"
)

// RepoConfig says which repository and branch a feed reads.
type RepoConfig struct {
	APIBase string // DefaultGitHubAPI when empty
	RawBase string // DefaultGitHubRaw when empty
	Owner   string
	Repo    string
	Branch  string
	DataDir string // such as conference
	// Depth is how many folders below DataDir the files are: 1 for conference/AI/x.yml,
	// 0 for conferences/x.yml.
	Depth  int
	Format Format
	// Contact is the operator's address, sent in the User-Agent.
	Contact string
	// HTTP, Pace, Retries and Backoff override the defaults (tests).
	HTTP    *http.Client
	Pace    time.Duration
	Retries int
	Backoff time.Duration
}

// Repo reads conference editions from one GitHub repository at the head of a branch.
type Repo struct {
	cfg RepoConfig
	f   *fetcher
}

var _ domain.RepoFeed = (*Repo)(nil)

// NewRepo returns a Repo.
func NewRepo(cfg RepoConfig) *Repo {
	if cfg.APIBase == "" {
		cfg.APIBase = DefaultGitHubAPI
	}
	if cfg.RawBase == "" {
		cfg.RawBase = DefaultGitHubRaw
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	cfg.RawBase = strings.TrimRight(cfg.RawBase, "/")
	pace := cfg.Pace
	if pace == 0 {
		pace = DefaultPace
	}
	retries := cfg.Retries
	if retries == 0 {
		retries = DefaultRetries
	}
	return &Repo{cfg: cfg, f: newFetcher(cfg.HTTP, UserAgent(cfg.Contact), pace, retries, cfg.Backoff)}
}

// URL is where the feed reads from, for the status.
func (r *Repo) URL() string {
	return fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s", r.cfg.Owner, r.cfg.Repo, r.cfg.Branch, r.cfg.DataDir)
}

func (r *Repo) api(p string) string {
	return r.cfg.APIBase + "/repos/" + url.PathEscape(r.cfg.Owner) + "/" + url.PathEscape(r.cfg.Repo) + p
}

// isData says whether a path is one of the repository's data files.
func (r *Repo) isData(p string) bool {
	if !strings.HasSuffix(p, ".yml") && !strings.HasSuffix(p, ".yaml") {
		return false
	}
	rest, ok := strings.CutPrefix(p, r.cfg.DataDir+"/")
	if !ok {
		return false
	}
	return strings.Count(rest, "/") == r.cfg.Depth
}

// Fetch reads the head of the branch. The first request is conditional on the ETag of the
// last: GitHub answers 304, which costs nothing, when the branch has not moved. A new commit
// is compared with the last one and only the changed data files are read; the first run, a
// history that was rewritten or a change of more than 300 files reads every data file.
// Files are read at the commit, so a run sees one consistent state.
func (r *Repo) Fetch(ctx context.Context, prev domain.RepoState) (domain.RepoFetch, error) {
	headers := map[string]string{"Accept": githubAPIType}
	if prev.CommitSHA != "" && prev.ETag != "" {
		headers["If-None-Match"] = prev.ETag
	}
	resp, err := r.f.get(ctx, r.api("/commits/"+url.PathEscape(r.cfg.Branch)), headers, maxAPIBytes)
	if err != nil {
		return domain.RepoFetch{}, fmt.Errorf("read the branch head: %w", err)
	}
	if resp.Status == http.StatusNotModified {
		return domain.RepoFetch{State: prev}, nil
	}
	var head struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(resp.Body, &head); err != nil || !isSHA(head.SHA) {
		return domain.RepoFetch{}, errors.New("read the branch head: not a commit")
	}
	state := domain.RepoState{CommitSHA: head.SHA, ETag: resp.ETag}
	if head.SHA == prev.CommitSHA {
		return domain.RepoFetch{State: state}, nil
	}
	if prev.CommitSHA != "" {
		out, ok, err := r.compare(ctx, prev.CommitSHA, head.SHA)
		if err != nil {
			return domain.RepoFetch{}, err
		}
		if ok {
			out.State = state
			return out, nil
		}
	}
	files, err := r.tree(ctx, head.SHA)
	if err != nil {
		return domain.RepoFetch{}, err
	}
	out := domain.RepoFetch{State: state, Changed: true, Full: true}
	if out.Files, err = r.readFiles(ctx, head.SHA, files); err != nil {
		return domain.RepoFetch{}, err
	}
	return out, nil
}

// compare reads the data files changed between two commits. ok is false when a full read
// is needed instead.
func (r *Repo) compare(ctx context.Context, base, head string) (domain.RepoFetch, bool, error) {
	resp, err := r.f.get(ctx, r.api("/compare/"+base+"..."+head), map[string]string{"Accept": githubAPIType}, maxAPIBytes)
	if IsStatus(err, http.StatusNotFound) {
		return domain.RepoFetch{}, false, nil
	}
	if err != nil {
		return domain.RepoFetch{}, false, fmt.Errorf("compare commits: %w", err)
	}
	var cmp struct {
		Status string `json:"status"`
		Files  []struct {
			Filename         string `json:"filename"`
			Status           string `json:"status"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Body, &cmp); err != nil {
		return domain.RepoFetch{}, false, errors.New("compare commits: not a comparison")
	}
	if cmp.Status != "ahead" || len(cmp.Files) >= maxCompare {
		return domain.RepoFetch{}, false, nil
	}
	out := domain.RepoFetch{Changed: true}
	var read []string
	for _, f := range cmp.Files {
		if f.Status == "renamed" && r.isData(f.PreviousFilename) {
			out.Deleted = append(out.Deleted, f.PreviousFilename)
		}
		if !r.isData(f.Filename) {
			continue
		}
		if f.Status == "removed" {
			out.Deleted = append(out.Deleted, f.Filename)
			continue
		}
		read = append(read, f.Filename)
	}
	if out.Files, err = r.readFiles(ctx, head, read); err != nil {
		return domain.RepoFetch{}, false, err
	}
	return out, true, nil
}

// tree lists the data files at a commit.
func (r *Repo) tree(ctx context.Context, sha string) ([]string, error) {
	resp, err := r.f.get(ctx, r.api("/git/trees/"+sha+"?recursive=1"), map[string]string{"Accept": githubAPIType}, maxAPIBytes)
	if err != nil {
		return nil, fmt.Errorf("list the files: %w", err)
	}
	var t struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(resp.Body, &t); err != nil {
		return nil, errors.New("list the files: not a tree")
	}
	if t.Truncated {
		return nil, errors.New("list the files: the tree is too large to list")
	}
	var out []string
	for _, e := range t.Tree {
		if e.Type == "blob" && r.isData(e.Path) {
			out = append(out, e.Path)
		}
	}
	if len(out) > maxDataFiles {
		return nil, fmt.Errorf("list the files: %d data files, more than %d", len(out), maxDataFiles)
	}
	return out, nil
}

// readFiles reads files at a commit from raw.githubusercontent.com, one at a time at the
// fetcher's pace. The URL names the commit, so what is read never changes under the run.
func (r *Repo) readFiles(ctx context.Context, sha string, paths []string) ([]domain.RepoFile, error) {
	out := make([]domain.RepoFile, 0, len(paths))
	for _, p := range paths {
		u := r.cfg.RawBase + "/" + url.PathEscape(r.cfg.Owner) + "/" + url.PathEscape(r.cfg.Repo) + "/" + sha + "/" + escapePath(p)
		resp, err := r.f.get(ctx, u, nil, maxFileBytes)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		out = append(out, domain.RepoFile{Path: p, Content: string(resp.Body)})
	}
	return out, nil
}

// escapePath escapes each segment of a path: conference/SC/ih&mmsec.yml.
func escapePath(p string) string {
	parts := strings.Split(path.Clean(p), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Parse turns the data files into editions.
func (r *Repo) Parse(files []domain.RepoFile) ([]domain.Edition, []domain.FileError) {
	switch r.cfg.Format {
	case FormatCCF:
		return parseCCF(files)
	case FormatHCI:
		return parseHCI(files)
	}
	return nil, []domain.FileError{{Path: "", Message: "unknown format " + string(r.cfg.Format)}}
}
