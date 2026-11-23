// Package docker is the small part of the Docker Engine API the compile worker needs: create,
// attach, start, wait, inspect, kill, remove and list containers. It speaks HTTP over the
// daemon's socket with the standard library only (docs/compile.md: no Docker SDK, whose
// dependencies dwarf this worker).
//
// Whoever can use the socket is root on that host. Only the compile worker holds it, never the
// API; production runs the worker on a dedicated compile host with rootless Docker and gVisor
// (runsc) where possible (docs/compile.md, Threat model).
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIVersion is the Engine API version the worker speaks: Docker 25 and later.
const APIVersion = "v1.44"

// Client talks to one Docker daemon.
type Client struct {
	http *http.Client
	dial func(ctx context.Context) (net.Conn, error)
	base string // scheme and host for request URLs
}

// New returns a client for host: "unix:///var/run/docker.sock" (the default when empty), any
// other unix socket (rootless Docker: unix:///run/user/1000/docker.sock), or tcp://host:port.
func New(host string) (*Client, error) {
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("docker host: %w", err)
	}
	var dial func(ctx context.Context) (net.Conn, error)
	var d net.Dialer
	switch u.Scheme {
	case "unix":
		path := u.Path
		dial = func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "unix", path) }
	case "tcp":
		addr := u.Host
		dial = func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }
	default:
		return nil, fmt.Errorf("docker host %q: use unix:// or tcp://", host)
	}
	tr := &http.Transport{
		DialContext:       func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		MaxIdleConns:      10,
		IdleConnTimeout:   30 * time.Second,
		DisableKeepAlives: false,
	}
	return &Client{http: &http.Client{Transport: tr}, dial: dial, base: "http://docker"}, nil
}

// Error is an answer of the daemon that is not a success.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("docker: %d %s", e.Status, e.Message) }

// IsNotFound reports whether err is the daemon's 404.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	u := c.base + "/" + APIVersion + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		var m struct{ Message string }
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		_ = json.Unmarshal(b, &m)
		return &Error{Status: res.StatusCode, Message: m.Message}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Ping checks that the daemon answers.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/_ping", nil, nil, nil)
}

// ImageExists reports whether the image (a name with a digest or tag) is present locally.
func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, nil, nil)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Create creates a container from spec and returns its ID.
func (c *Client) Create(ctx context.Context, name string, spec ContainerSpec) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}
	if err := c.do(ctx, http.MethodPost, "/containers/create", q, spec, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Start starts a created container.
func (c *Client) Start(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil, nil, nil)
}

// Wait waits until the container stops and returns its exit code.
func (c *Client) Wait(ctx context.Context, id string) (int, error) {
	var out struct {
		StatusCode int
		Error      *struct{ Message string }
	}
	if err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/wait", nil, nil, &out); err != nil {
		return 0, err
	}
	if out.Error != nil && out.Error.Message != "" {
		return out.StatusCode, fmt.Errorf("docker wait: %s", out.Error.Message)
	}
	return out.StatusCode, nil
}

// Kill sends SIGKILL; a container already stopped is not an error.
func (c *Client) Kill(ctx context.Context, id string) error {
	err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/kill", url.Values{"signal": {"KILL"}}, nil, nil)
	var e *Error
	if errors.As(err, &e) && (e.Status == http.StatusConflict || e.Status == http.StatusNotFound) {
		return nil
	}
	return err
}

// Remove removes the container, running or not, with its anonymous volumes; a container
// already gone is not an error.
func (c *Client) Remove(ctx context.Context, id string) error {
	err := c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	var e *Error
	if errors.As(err, &e) && e.Status == http.StatusConflict && strings.Contains(e.Message, "already in progress") {
		return nil
	}
	return err
}

// State is what the worker reads back of a stopped container.
type State struct {
	OOMKilled bool
	ExitCode  int
}

// Inspect reads the container's state.
func (c *Client) Inspect(ctx context.Context, id string) (State, error) {
	var out struct{ State State }
	if err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, nil, &out); err != nil {
		return State{}, err
	}
	return out.State, nil
}

// Listed is a container a label found.
type Listed struct {
	ID      string `json:"Id"`
	Created int64
}

// List returns every container, running or not, with the label key=value.
func (c *Client) List(ctx context.Context, key, value string) ([]Listed, error) {
	f, err := json.Marshal(map[string][]string{"label": {key + "=" + value}})
	if err != nil {
		return nil, err
	}
	var out []Listed
	err = c.do(ctx, http.MethodGet, "/containers/json", url.Values{"all": {"1"}, "filters": {string(f)}}, nil, &out)
	return out, err
}

// Attached is a container's standard streams: write stdin, then CloseStdin; read stdout.
type Attached struct {
	conn   net.Conn
	reader *bufio.Reader
}

// Attach attaches to the created, not yet started, container's stdin, stdout and stderr, so no
// output is lost. The connection is hijacked from HTTP (Upgrade: tcp), as the Docker CLI does.
func (c *Client) Attach(ctx context.Context, id string) (*Attached, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker attach: %w", err)
	}
	path := "/" + APIVersion + "/containers/" + url.PathEscape(id) + "/attach?stream=1&stdin=1&stdout=1&stderr=1"
	req := "POST " + path + " HTTP/1.1\r\nHost: docker\r\nContent-Type: text/plain\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("docker attach: %w", err)
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, nil) //nolint:bodyclose // the body is the hijacked stream
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("docker attach: %w", err)
	}
	if res.StatusCode != http.StatusSwitchingProtocols && res.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, &Error{Status: res.StatusCode, Message: "attach refused"}
	}
	_ = conn.SetDeadline(time.Time{})
	return &Attached{conn: conn, reader: br}, nil
}

// Write writes to the container's stdin.
func (a *Attached) Write(p []byte) (int, error) { return a.conn.Write(p) }

// CloseStdin ends the container's stdin (it reads EOF), keeping stdout open.
func (a *Attached) CloseStdin() error {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := a.conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("docker attach: the connection cannot close its write side")
}

// Close closes the connection.
func (a *Attached) Close() error { return a.conn.Close() }

// SetDeadline bounds every further read and write.
func (a *Attached) SetDeadline(t time.Time) error { return a.conn.SetDeadline(t) }

// Demux splits the multiplexed output (no TTY) into stdout and stderr until EOF: each frame is
// a stream byte (1 stdout, 2 stderr), three zero bytes and a big-endian length.
func (a *Attached) Demux(stdout, stderr io.Writer) error {
	return Demux(a.reader, stdout, stderr)
}

// Demux is Attached.Demux on any reader (tests).
func Demux(r io.Reader, stdout, stderr io.Writer) error {
	var h [8]byte
	for {
		if _, err := io.ReadFull(r, h[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		n := int64(binary.BigEndian.Uint32(h[4:]))
		var w io.Writer
		switch h[0] {
		case 1:
			w = stdout
		case 2:
			w = stderr
		default:
			w = io.Discard
		}
		if _, err := io.CopyN(w, r, n); err != nil {
			return err
		}
	}
}

// ContainerSpec is the create body: the fields the sandbox sets.
type ContainerSpec struct {
	Image           string
	Cmd             []string          `json:",omitempty"`
	Entrypoint      []string          `json:",omitempty"`
	Env             []string          `json:",omitempty"`
	User            string            `json:",omitempty"`
	WorkingDir      string            `json:",omitempty"`
	Labels          map[string]string `json:",omitempty"`
	AttachStdin     bool
	AttachStdout    bool
	AttachStderr    bool
	OpenStdin       bool
	StdinOnce       bool
	Tty             bool
	NetworkDisabled bool
	HostConfig      HostConfig
}

// HostConfig is the sandbox's isolation and limits (NFR-4).
type HostConfig struct {
	NetworkMode    string
	ReadonlyRootfs bool
	CapDrop        []string
	SecurityOpt    []string
	Tmpfs          map[string]string
	Memory         int64
	MemorySwap     int64
	NanoCPUs       int64 `json:"NanoCpus"`
	PidsLimit      int64
	Ulimits        []Ulimit
	IpcMode        string
	Init           bool
	Runtime        string `json:",omitempty"`
	LogConfig      LogConfig
	AutoRemove     bool
	Privileged     bool
	Binds          []string `json:",omitempty"`
}

// Ulimit is one resource limit.
type Ulimit struct {
	Name string
	Soft int64
	Hard int64
}

// LogConfig names the log driver; "none" keeps the container's output off the host's disk.
type LogConfig struct {
	Type string
}
