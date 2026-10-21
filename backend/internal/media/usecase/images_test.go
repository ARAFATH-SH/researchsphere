package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// First bytes of each accepted format, enough for the sniffing.
var (
	jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	pngHead  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n', 0x00, 0x00, 0x00, 0x0D}
	webpHead = []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00")
	// A VP8X chunk whose flags byte (offset 20) has the animation bit (0x02) set.
	animatedWebP = []byte("RIFF\x24\x00\x00\x00WEBPVP8X\x0A\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	// A VP8X chunk without the animation bit: a still image with alpha (0x10).
	stillVP8X = []byte("RIFF\x24\x00\x00\x00WEBPVP8X\x0A\x00\x00\x00\x10\x00\x00\x00\x00\x00\x00\x00\x00\x00")
)

// fakeFiles is a domain.ProfileImages in memory. It keeps the current file of each user
// and purpose, and the IDs of the rows a replacement or a clear deleted.
type fakeFiles struct {
	mu        sync.Mutex
	noProfile map[string]bool
	current   map[string]domain.NewFile // by user ID + "/" + purpose
	ids       map[string]string         // current file ID, same key
	found     map[string]domain.StoredFile
	deleted   []string
	replaced  []domain.NewFile
	cleared   []string
	next      int
	err       error // returned by ReplaceProfileImage when set
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{
		noProfile: map[string]bool{},
		current:   map[string]domain.NewFile{},
		ids:       map[string]string{},
		found:     map[string]domain.StoredFile{},
	}
}

func (f *fakeFiles) ReplaceProfileImage(_ context.Context, nf domain.NewFile) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	if f.noProfile[nf.OwnerID] {
		return "", domain.ErrNoProfile
	}
	k := nf.OwnerID + "/" + string(nf.Purpose)
	if old, ok := f.ids[k]; ok {
		f.deleted = append(f.deleted, old)
	}
	f.next++
	id := fmt.Sprintf("file-%d", f.next)
	f.current[k], f.ids[k] = nf, id
	f.replaced = append(f.replaced, nf)
	return id, nil
}

func (f *fakeFiles) ClearProfileImage(_ context.Context, userID string, p domain.Purpose) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := userID + "/" + string(p)
	if old, ok := f.ids[k]; ok {
		f.deleted = append(f.deleted, old)
		delete(f.ids, k)
		delete(f.current, k)
	}
	f.cleared = append(f.cleared, k)
	return nil
}

func (f *fakeFiles) FindFile(_ context.Context, id string) (domain.StoredFile, error) {
	sf, ok := f.found[id]
	if !ok {
		return domain.StoredFile{}, domain.ErrNotFound
	}
	return sf, nil
}

// fakeBucket is a domain.ObjectStore in memory.
type fakeBucket struct {
	mu        sync.Mutex
	objects   map[string][]byte
	types     map[string]string
	deleted   []string
	putErr    error
	presigned time.Duration // the expiry of the last PresignGet
}

func newFakeBucket() *fakeBucket {
	return &fakeBucket{objects: map[string][]byte{}, types: map[string]string{}}
}

func (b *fakeBucket) Put(_ context.Context, key string, r io.Reader, size int64, ct string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.putErr != nil {
		return b.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("put %s: size %d, read %d bytes", key, size, len(data))
	}
	b.objects[key], b.types[key] = data, ct
	return nil
}

func (b *fakeBucket) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	b.deleted = append(b.deleted, key)
	return nil
}

func (b *fakeBucket) PresignGet(_ context.Context, key string, expiry time.Duration) (*url.URL, error) {
	b.presigned = expiry
	return url.Parse("https://bucket.example/" + key + "?X-Amz-Signature=abc")
}

// fakeCodec reports a chosen size and renders a fixed JPEG-looking result. It records
// what it was asked, so tests can check that nothing was decoded.
type fakeCodec struct {
	size      domain.ImageSize
	sizeErr   error
	renderErr error
	sizes     []domain.Format
	renders   []string // "format WxH qQ"
}

func (c *fakeCodec) Size(r io.Reader, f domain.Format) (domain.ImageSize, error) {
	if _, err := io.ReadAll(r); err != nil {
		return domain.ImageSize{}, err
	}
	c.sizes = append(c.sizes, f)
	if c.sizeErr != nil {
		return domain.ImageSize{}, c.sizeErr
	}
	return c.size, nil
}

func (c *fakeCodec) Render(_ io.Reader, f domain.Format, w, h, q int) ([]byte, error) {
	c.renders = append(c.renders, fmt.Sprintf("%s %dx%d q%d", f, w, h, q))
	if c.renderErr != nil {
		return nil, c.renderErr
	}
	return []byte("\xFF\xD8rendered"), nil
}

// fakeLimiter is a fixed window in memory. A test sets err to act as Redis being down.
type fakeLimiter struct {
	counts  map[string]int
	left    time.Duration
	err     error
	windows []time.Duration
}

func newFakeLimiter() *fakeLimiter {
	return &fakeLimiter{counts: map[string]int{}, left: 42 * time.Minute}
}

func (l *fakeLimiter) Used(_ context.Context, userID string) (int, time.Duration, error) {
	if l.err != nil {
		return 0, 0, l.err
	}
	if l.counts[userID] == 0 {
		return 0, 0, nil
	}
	return l.counts[userID], l.left, nil
}

func (l *fakeLimiter) Record(_ context.Context, userID string, window time.Duration) error {
	if l.err != nil {
		return l.err
	}
	l.counts[userID]++
	l.windows = append(l.windows, window)
	return nil
}

type imagesFixture struct {
	files   *fakeFiles
	bucket  *fakeBucket
	codec   *fakeCodec
	limiter *fakeLimiter
	logs    *bytes.Buffer
	svc     *Images
}

func newImagesFixture() *imagesFixture {
	f := &imagesFixture{
		files:   newFakeFiles(),
		bucket:  newFakeBucket(),
		codec:   &fakeCodec{size: domain.ImageSize{Width: 1200, Height: 900}},
		limiter: newFakeLimiter(),
		logs:    &bytes.Buffer{},
	}
	f.svc = NewImages(f.files, f.bucket, f.codec, f.limiter, slog.New(slog.NewJSONHandler(f.logs, nil)))
	return f
}

// file returns head followed by filler bytes, n bytes in all.
func file(head []byte, n int) io.Reader {
	b := make([]byte, n)
	copy(b, head)
	return bytes.NewReader(b)
}

var keyPattern = regexp.MustCompile(`^users/u1/(photo|cover)/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.jpg$`)

func TestUploadStoresTheReEncodedImage(t *testing.T) {
	tests := []struct {
		purpose domain.Purpose
		render  string
		w, h    int
	}{
		{domain.PurposePhoto, "png 400x400 q85", 400, 400},
		{domain.PurposeCover, "png 1584x396 q85", 1584, 396},
	}
	for _, tt := range tests {
		t.Run(string(tt.purpose), func(t *testing.T) {
			f := newImagesFixture()
			id, err := f.svc.Upload(context.Background(), "u1", tt.purpose, file(pngHead, 1000))
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if id != "file-1" {
				t.Errorf("id = %q, want file-1", id)
			}
			if got := strings.Join(f.codec.renders, ","); got != tt.render {
				t.Errorf("render = %q, want %q", got, tt.render)
			}
			if len(f.files.replaced) != 1 {
				t.Fatalf("replaced %d rows, want 1", len(f.files.replaced))
			}
			nf := f.files.replaced[0]
			if !keyPattern.MatchString(nf.ObjectKey) || !strings.Contains(nf.ObjectKey, "/"+string(tt.purpose)+"/") {
				t.Errorf("object key = %q, want users/u1/%s/<uuid>.jpg", nf.ObjectKey, tt.purpose)
			}
			want := domain.NewFile{OwnerID: "u1", Purpose: tt.purpose, ObjectKey: nf.ObjectKey,
				ContentType: "image/jpeg", Size: int64(len("\xFF\xD8rendered")), Width: tt.w, Height: tt.h}
			if nf != want {
				t.Errorf("row = %+v, want %+v", nf, want)
			}
			if got := string(f.bucket.objects[nf.ObjectKey]); got != "\xFF\xD8rendered" {
				t.Errorf("stored object = %q, want the rendered bytes, not the upload", got)
			}
			if f.bucket.types[nf.ObjectKey] != "image/jpeg" {
				t.Errorf("content type = %q, want image/jpeg", f.bucket.types[nf.ObjectKey])
			}
			if len(f.limiter.windows) != 1 || f.limiter.windows[0] != time.Hour {
				t.Errorf("limiter windows = %v, want one of 1h", f.limiter.windows)
			}
		})
	}
}

func TestUploadSniffsTheTypeFromTheFirstBytes(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		format  domain.Format
		wantErr error
	}{
		{"jpeg", jpegHead, domain.FormatJPEG, nil},
		{"png", pngHead, domain.FormatPNG, nil},
		{"webp", webpHead, domain.FormatWebP, nil},
		{"still webp with VP8X", stillVP8X, domain.FormatWebP, nil},
		{"animated webp", animatedWebP, "", domain.ErrUnsupportedType},
		{"text renamed .jpg", []byte("hello, I am not a picture at all"), "", domain.ErrUnsupportedType},
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00"), "", domain.ErrUnsupportedType},
		{"riff that is not webp", []byte("RIFF\x24\x00\x00\x00WAVEfmt "), "", domain.ErrUnsupportedType},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "", domain.ErrUnsupportedType},
		{"jpeg cut short", jpegHead[:2], "", domain.ErrUnsupportedType},
		{"empty", nil, "", domain.ErrUnsupportedType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newImagesFixture()
			_, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, bytes.NewReader(tt.data))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(f.codec.sizes)+len(f.codec.renders) != 0 {
					t.Errorf("codec was called for a refused type: sizes %v, renders %v", f.codec.sizes, f.codec.renders)
				}
				return
			}
			if len(f.codec.sizes) != 1 || f.codec.sizes[0] != tt.format {
				t.Errorf("codec read format %v, want %s", f.codec.sizes, tt.format)
			}
		})
	}
}

func TestUploadEnforcesTheSizeLimit(t *testing.T) {
	tests := []struct {
		name    string
		purpose domain.Purpose
		n       int
		wantErr error
	}{
		{"photo of exactly 5 MB", domain.PurposePhoto, 5 << 20, nil},
		{"photo over 5 MB", domain.PurposePhoto, 5<<20 + 1, domain.ErrFileTooLarge},
		{"cover of 6 MB", domain.PurposeCover, 6 << 20, nil},
		{"cover of exactly 8 MB", domain.PurposeCover, 8 << 20, nil},
		{"cover over 8 MB", domain.PurposeCover, 8<<20 + 1, domain.ErrFileTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newImagesFixture()
			_, err := f.svc.Upload(context.Background(), "u1", tt.purpose, file(jpegHead, tt.n))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && (len(f.codec.sizes) != 0 || len(f.bucket.objects) != 0) {
				t.Error("a file over the limit was read as an image or stored")
			}
		})
	}
}

func TestUploadRefusesHugeDimensionsBeforeDecoding(t *testing.T) {
	tests := []struct {
		name    string
		size    domain.ImageSize
		wantErr error
	}{
		{"8000 x 8000", domain.ImageSize{Width: 8000, Height: 8000}, nil},
		{"too wide", domain.ImageSize{Width: 8001, Height: 10}, domain.ErrImageTooLarge},
		{"too high", domain.ImageSize{Width: 10, Height: 8001}, domain.ErrImageTooLarge},
		{"decompression bomb", domain.ImageSize{Width: 65535, Height: 65535}, domain.ErrImageTooLarge},
		{"zero width", domain.ImageSize{Width: 0, Height: 10}, domain.ErrInvalidImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newImagesFixture()
			f.codec.size = tt.size
			_, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, file(pngHead, 100))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && len(f.codec.renders) != 0 {
				t.Errorf("image was decoded: %v", f.codec.renders)
			}
		})
	}
}

func TestUploadUnreadableImage(t *testing.T) {
	f := newImagesFixture()
	f.codec.sizeErr = domain.ErrInvalidImage
	if _, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, file(jpegHead, 100)); !errors.Is(err, domain.ErrInvalidImage) {
		t.Errorf("bad header: err = %v, want ErrInvalidImage", err)
	}
	f = newImagesFixture()
	f.codec.renderErr = fmt.Errorf("decode: %w", domain.ErrInvalidImage)
	if _, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, file(jpegHead, 100)); !errors.Is(err, domain.ErrInvalidImage) {
		t.Errorf("bad pixels: err = %v, want ErrInvalidImage", err)
	}
	if len(f.bucket.objects)+len(f.files.replaced) != 0 {
		t.Error("an unreadable image was stored")
	}
}

func TestUploadReplacesTheOldImage(t *testing.T) {
	f := newImagesFixture()
	ctx := context.Background()
	first, err := f.svc.Upload(ctx, "u1", domain.PurposePhoto, file(jpegHead, 100))
	if err != nil {
		t.Fatal(err)
	}
	cover, err := f.svc.Upload(ctx, "u1", domain.PurposeCover, file(jpegHead, 100))
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.Upload(ctx, "u1", domain.PurposePhoto, file(pngHead, 100))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.files.deleted) != 1 || f.files.deleted[0] != first {
		t.Errorf("deleted rows = %v, want only the first photo %s", f.files.deleted, first)
	}
	if f.files.ids["u1/photo"] != second || f.files.ids["u1/cover"] != cover {
		t.Errorf("current = %v, want photo %s and cover %s", f.files.ids, second, cover)
	}
	if k1, k2 := f.files.replaced[0].ObjectKey, f.files.replaced[2].ObjectKey; k1 == k2 {
		t.Errorf("both photos have the key %s, want a new key per upload", k1)
	}
}

func TestUploadRateLimit(t *testing.T) {
	f := newImagesFixture()
	ctx := context.Background()
	for i := range UploadsPerWindow {
		p := domain.PurposePhoto
		if i%2 == 1 {
			p = domain.PurposeCover // photo and cover share the limit
		}
		if _, err := f.svc.Upload(ctx, "u1", p, file(jpegHead, 100)); err != nil {
			t.Fatalf("upload %d: %v", i+1, err)
		}
	}
	_, err := f.svc.Upload(ctx, "u1", domain.PurposeCover, file(jpegHead, 100))
	var rl *domain.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("6th upload: err = %v, want RateLimitedError", err)
	}
	if rl.RetryAfter != 42*time.Minute {
		t.Errorf("RetryAfter = %s, want the 42m left in the window", rl.RetryAfter)
	}
	if len(f.bucket.objects) != UploadsPerWindow || len(f.codec.sizes) != UploadsPerWindow {
		t.Errorf("6th upload was read or stored: %d objects, %d headers read", len(f.bucket.objects), len(f.codec.sizes))
	}
	if _, err := f.svc.Upload(ctx, "u2", domain.PurposePhoto, file(jpegHead, 100)); err != nil {
		t.Errorf("another user: %v, want the upload accepted", err)
	}
	if err := f.svc.Remove(ctx, "u1", domain.PurposePhoto); err != nil {
		t.Errorf("Remove while rate limited: %v, want it accepted", err)
	}
}

func TestRefusedUploadsDoNotCount(t *testing.T) {
	f := newImagesFixture()
	ctx := context.Background()
	refused := []struct {
		name string
		prep func()
		data io.Reader
	}{
		{"wrong type", func() {}, file([]byte("not an image"), 100)},
		{"too large", func() {}, file(jpegHead, 5<<20+1)},
		{"huge dimensions", func() { f.codec.size = domain.ImageSize{Width: 9000, Height: 9000} }, file(jpegHead, 100)},
		{"unreadable", func() { f.codec.renderErr = domain.ErrInvalidImage }, file(jpegHead, 100)},
		{"storage fails", func() { f.bucket.putErr = errors.New("bucket down") }, file(jpegHead, 100)},
		{"no profile", func() { f.files.noProfile["u1"] = true }, file(jpegHead, 100)},
	}
	for _, r := range refused {
		f.codec.size, f.codec.renderErr, f.bucket.putErr = domain.ImageSize{Width: 10, Height: 10}, nil, nil
		delete(f.files.noProfile, "u1")
		r.prep()
		if _, err := f.svc.Upload(ctx, "u1", domain.PurposePhoto, r.data); err == nil {
			t.Fatalf("%s: upload accepted, want refused", r.name)
		}
	}
	if f.limiter.counts["u1"] != 0 {
		t.Fatalf("refused uploads counted %d times, want 0", f.limiter.counts["u1"])
	}
	f.codec.size, f.codec.renderErr, f.bucket.putErr = domain.ImageSize{Width: 10, Height: 10}, nil, nil
	delete(f.files.noProfile, "u1")
	for i := range UploadsPerWindow {
		if _, err := f.svc.Upload(ctx, "u1", domain.PurposePhoto, file(jpegHead, 100)); err != nil {
			t.Fatalf("upload %d after refused ones: %v", i+1, err)
		}
	}
}

func TestUploadWithoutProfileDeletesTheObject(t *testing.T) {
	f := newImagesFixture()
	f.files.noProfile["u1"] = true
	_, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, file(jpegHead, 100))
	if !errors.Is(err, domain.ErrNoProfile) {
		t.Fatalf("err = %v, want ErrNoProfile", err)
	}
	if len(f.bucket.objects) != 0 || len(f.bucket.deleted) != 1 {
		t.Errorf("objects left %v, deleted %v; want the stored object deleted again", f.bucket.objects, f.bucket.deleted)
	}
}

func TestUploadFailsOpenWhenTheLimiterIsDown(t *testing.T) {
	f := newImagesFixture()
	f.limiter.err = errors.New("redis: connection refused")
	for i := range UploadsPerWindow + 2 {
		if _, err := f.svc.Upload(context.Background(), "u1", domain.PurposePhoto, file(jpegHead, 100)); err != nil {
			t.Fatalf("upload %d with Redis down: %v, want it accepted (fail open)", i+1, err)
		}
	}
	if !strings.Contains(f.logs.String(), `"level":"WARN"`) || !strings.Contains(f.logs.String(), "upload rate limit") {
		t.Errorf("logs = %s, want a warning about the rate limit", f.logs.String())
	}
}

func TestRemove(t *testing.T) {
	f := newImagesFixture()
	ctx := context.Background()
	id, err := f.svc.Upload(ctx, "u1", domain.PurposeCover, file(jpegHead, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Remove(ctx, "u1", domain.PurposeCover); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := f.svc.Remove(ctx, "u1", domain.PurposeCover); err != nil {
		t.Fatalf("Remove with nothing set: %v", err)
	}
	if len(f.files.deleted) != 1 || f.files.deleted[0] != id {
		t.Errorf("deleted = %v, want [%s]", f.files.deleted, id)
	}
	if f.limiter.counts["u1"] != 1 {
		t.Errorf("uploads counted = %d, want 1: removing does not count", f.limiter.counts["u1"])
	}
}

func TestFileURL(t *testing.T) {
	photo := domain.StoredFile{ID: "f1", OwnerID: "owner", Purpose: domain.PurposePhoto,
		ObjectKey: "users/owner/photo/x.jpg", ScanStatus: "clean", OwnerVerified: true}
	with := func(change func(*domain.StoredFile)) domain.StoredFile {
		sf := photo
		change(&sf)
		return sf
	}
	tests := []struct {
		name   string
		file   domain.StoredFile
		caller string
		ok     bool
	}{
		{"owner", photo, "owner", true},
		{"another user", photo, "viewer", true},
		{"cover, another user", with(func(f *domain.StoredFile) { f.Purpose = domain.PurposeCover }), "viewer", true},
		{"suspended owner, another user", with(func(f *domain.StoredFile) { f.OwnerSuspended = true }), "viewer", false},
		{"suspended owner, the owner", with(func(f *domain.StoredFile) { f.OwnerSuspended = true }), "owner", true},
		{"unverified owner, another user", with(func(f *domain.StoredFile) { f.OwnerVerified = false }), "viewer", false},
		{"figure, another user", with(func(f *domain.StoredFile) { f.Purpose = "figure" }), "viewer", false},
		{"figure, the owner", with(func(f *domain.StoredFile) { f.Purpose = "figure" }), "owner", false},
		{"not scanned", with(func(f *domain.StoredFile) { f.ScanStatus = "pending" }), "owner", false},
		{"rejected by the scan", with(func(f *domain.StoredFile) { f.ScanStatus = "rejected" }), "viewer", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newImagesFixture()
			f.files.found["f1"] = tt.file
			u, err := f.svc.FileURL(context.Background(), tt.caller, "f1")
			if !tt.ok {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FileURL: %v", err)
			}
			if !strings.HasPrefix(u.String(), "https://bucket.example/"+tt.file.ObjectKey) {
				t.Errorf("url = %s, want the presigned URL of %s", u, tt.file.ObjectKey)
			}
			if f.bucket.presigned != 10*time.Minute {
				t.Errorf("expiry = %s, want 10m", f.bucket.presigned)
			}
		})
	}
	f := newImagesFixture()
	if _, err := f.svc.FileURL(context.Background(), "owner", "unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown file: err = %v, want ErrNotFound", err)
	}
}
