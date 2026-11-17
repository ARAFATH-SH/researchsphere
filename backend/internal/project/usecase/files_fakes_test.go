package usecase

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// memFiles is the FileStore in memory. It keeps the database's rules that the usecase relies
// on (migration 000035): paths unique ignoring letter case, a parent folder for every entry,
// at most MaxFiles entries per project.
type memFiles struct {
	mu     sync.Mutex
	s      *memStore
	next   int
	files  map[string]*memFile // by ID
	main   map[string]string   // project -> main file path
	names  map[string]string   // user -> profile name
	shared []string            // projects LockShared was called for
	// uploads by ID; now is the clock of their expiry, the test's.
	uploads map[string]*memUpload
	now     func() time.Time
}

type memUpload struct {
	domain.Upload
	created time.Time
}

type memFile struct {
	domain.StoredFile
}

func newMemFiles(s *memStore) *memFiles {
	return &memFiles{s: s, files: map[string]*memFile{}, main: map[string]string{}, names: map[string]string{},
		uploads: map[string]*memUpload{}, now: time.Now}
}

func (m *memFiles) project(projectID string) []*memFile {
	var out []*memFile
	for _, f := range m.files {
		if f.ProjectID == projectID {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	return out
}

func (m *memFiles) at(projectID, path string) *memFile {
	for _, f := range m.files {
		if f.ProjectID == projectID && strings.EqualFold(f.Path, path) {
			return f
		}
	}
	return nil
}

func (m *memFiles) mainPath(projectID string) string {
	if p, ok := m.main[projectID]; ok {
		return p
	}
	return MainFileName
}

func (m *memFiles) Tree(_ context.Context, projectID string) (domain.FileTree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := domain.FileTree{MainFilePath: m.mainPath(projectID), Files: []domain.File{}}
	for _, f := range m.project(projectID) {
		t.Files = append(t.Files, f.File)
	}
	return t, nil
}

func (m *memFiles) File(_ context.Context, projectID, fileID string) (domain.StoredFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[fileID]
	if !ok || f.ProjectID != projectID {
		return domain.StoredFile{}, domain.ErrFileNotFound
	}
	return f.StoredFile, nil
}

func (m *memFiles) LockFile(ctx context.Context, projectID, fileID string) (domain.StoredFile, error) {
	return m.File(ctx, projectID, fileID)
}

func (m *memFiles) FileAt(_ context.Context, projectID, path string) (domain.File, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f := m.at(projectID, path); f != nil {
		return f.File, true, nil
	}
	return domain.File{}, false, nil
}

func (m *memFiles) insert(f domain.StoredFile) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.at(f.ProjectID, f.Path) != nil {
		return "", domain.ErrPathTaken
	}
	if i := strings.LastIndexByte(f.Path, '/'); i >= 0 {
		if p := m.at(f.ProjectID, f.Path[:i]); p == nil || p.Kind != domain.KindFolder {
			return "", domain.ErrParentNotFolder
		}
	}
	if len(m.project(f.ProjectID)) >= MaxFiles {
		return "", domain.ErrFileLimit
	}
	m.next++
	f.ID = "f" + strconv.Itoa(m.next)
	f.Version = 1
	f.UpdatedAt = time.Now()
	m.files[f.ID] = &memFile{StoredFile: f}
	return f.ID, nil
}

func by(user string) *domain.Person {
	if user == "" {
		return nil
	}
	return &domain.Person{UserID: user}
}

func (m *memFiles) InsertFolder(_ context.Context, projectID, path, user string) (string, error) {
	return m.insert(domain.StoredFile{File: domain.File{ProjectID: projectID, Path: path, Kind: domain.KindFolder, UpdatedBy: by(user)}})
}

func (m *memFiles) InsertText(_ context.Context, projectID, path, content, user string) (string, error) {
	return m.insert(domain.StoredFile{
		File:    domain.File{ProjectID: projectID, Path: path, Kind: domain.KindText, Size: int64(len(content)), UpdatedBy: by(user)},
		Content: &content,
	})
}

func (m *memFiles) InsertFigure(_ context.Context, f domain.NewFigure) (string, error) {
	if f.Size > 200<<20 {
		return "", errors.New("check violation: project_files_binary")
	}
	st := domain.ScanPending
	return m.insert(domain.StoredFile{
		File: domain.File{
			ProjectID: f.ProjectID, Path: f.Path, Kind: domain.KindBinary, Size: f.Size,
			ContentType: &f.ContentType, ScanStatus: &st, UpdatedBy: by(f.UploadedBy),
		},
		ObjectKey: &f.ObjectKey,
	})
}

func (m *memFiles) SaveContent(_ context.Context, fileID, content, user string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[fileID]
	f.Content, f.Size, f.UpdatedBy = &content, int64(len(content)), by(user)
	f.Version++
	return f.Version, nil
}

func (m *memFiles) Move(_ context.Context, projectID, from, to, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.project(projectID) {
		if under(f.Path, from) {
			f.Path, f.UpdatedBy = to+f.Path[len(from):], by(user)
		}
	}
	if mp := m.mainPath(projectID); under(mp, from) {
		m.main[projectID] = to + mp[len(from):]
	}
	return nil
}

func (m *memFiles) ObjectKeysUnder(_ context.Context, projectID, path string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for _, f := range m.project(projectID) {
		if f.ObjectKey != nil && under(f.Path, path) {
			keys = append(keys, *f.ObjectKey)
		}
	}
	return keys, nil
}

func (m *memFiles) DeleteTree(_ context.Context, projectID, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.project(projectID) {
		if under(f.Path, path) {
			delete(m.files, f.ID)
		}
	}
	return nil
}

func (m *memFiles) ObjectKeys(ctx context.Context, projectID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for _, f := range m.project(projectID) {
		if f.ObjectKey != nil {
			keys = append(keys, *f.ObjectKey)
		}
	}
	return keys, nil
}

func (m *memFiles) FileCount(_ context.Context, projectID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.project(projectID)), nil
}

func (m *memFiles) FullName(_ context.Context, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.names[userID], nil
}

func (m *memFiles) ProjectsWithoutFiles(_ context.Context, after string, limit int) ([]string, error) {
	m.s.mu.Lock()
	ids := make([]string, 0, len(m.s.projects))
	for id := range m.s.projects {
		ids = append(ids, id)
	}
	m.s.mu.Unlock()
	sort.Strings(ids)
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, id := range ids {
		if id > after && len(m.project(id)) == 0 && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memFiles) LockShared(ctx context.Context, projectID, userID string) (domain.Role, error) {
	m.mu.Lock()
	m.shared = append(m.shared, projectID)
	m.mu.Unlock()
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	if _, ok := m.s.projects[projectID]; !ok {
		return domain.RoleNone, domain.ErrNotFound
	}
	return m.s.members[projectID][userID], nil
}

func (m *memFiles) StorageUsed(_ context.Context, projectID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, f := range m.project(projectID) {
		if f.Kind == domain.KindBinary && (f.ScanStatus == nil || *f.ScanStatus != domain.ScanInfected) {
			n += f.Size
		}
	}
	return n, nil
}

func (m *memFiles) PendingUploadBytes(_ context.Context, projectID string, since time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, u := range m.uploads {
		if u.ProjectID == projectID && u.created.After(since) && u.ExpiresAt.After(m.now()) {
			n += u.Size
		}
	}
	return n, nil
}

func (m *memFiles) InsertUpload(_ context.Context, u domain.Upload) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	u.ID = "u" + strconv.Itoa(m.next)
	m.uploads[u.ID] = &memUpload{Upload: u, created: m.now()}
	return u.ID, nil
}

func (m *memFiles) Upload(_ context.Context, projectID, uploadID, userID string) (domain.Upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.uploads[uploadID]
	if !ok || u.ProjectID != projectID || u.UserID != userID || !u.ExpiresAt.After(m.now()) {
		return domain.Upload{}, domain.ErrUploadNotFound
	}
	return u.Upload, nil
}

func (m *memFiles) ClaimUpload(_ context.Context, uploadID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.uploads[uploadID]
	if !ok || !u.ExpiresAt.After(m.now()) {
		return false, nil
	}
	delete(m.uploads, uploadID)
	return true, nil
}

func (m *memFiles) DeleteExpiredUploads(_ context.Context, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, u := range m.uploads {
		if n < limit && !u.ExpiresAt.After(m.now()) {
			delete(m.uploads, id)
			n++
		}
	}
	return n, nil
}

// memIntake checks figures by a prefix of the content: "PNG" and "JPG" are images, whose
// metadata is the text "META" (removed), "PDF" a PDF, "BAD" a broken image; the rest is
// refused.
type memIntake struct{}

func (memIntake) StripFigure(r io.Reader, w io.Writer) (domain.FigureInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return domain.FigureInfo{}, err
	}
	var info domain.FigureInfo
	switch {
	case bytes.HasPrefix(data, []byte("PNG")):
		info = domain.FigureInfo{ContentType: TypePNG, Width: 1, Height: 1}
		data = bytes.ReplaceAll(data, []byte("META"), nil)
	case bytes.HasPrefix(data, []byte("JPG")):
		info = domain.FigureInfo{ContentType: TypeJPEG, Width: 1, Height: 1}
		data = bytes.ReplaceAll(data, []byte("META"), nil)
	case bytes.HasPrefix(data, []byte("PDF")):
		info = domain.FigureInfo{ContentType: TypePDF}
	case bytes.HasPrefix(data, []byte("BAD")):
		return domain.FigureInfo{}, domain.ErrInvalidImage
	default:
		return domain.FigureInfo{}, domain.ErrUnsupportedType
	}
	if _, err := w.Write(data); err != nil {
		return domain.FigureInfo{}, err
	}
	return info, nil
}

// memObjects is the bucket in memory. Each write gives the object a new ETag.
type memObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	etags   map[string]string
	writes  int
	deleted []string
	copies  int
	// posts is what each presigned POST allows: key -> size.
	posts map[string]int64
	// beforeOpen runs before each Open, for a test that writes the object in between.
	beforeOpen func(key string)
}

func (o *memObjects) put(key string, b []byte, contentType string) {
	if o.objects == nil {
		o.objects, o.types, o.etags = map[string][]byte{}, map[string]string{}, map[string]string{}
	}
	o.writes++
	o.objects[key], o.types[key], o.etags[key] = b, contentType, "etag-"+strconv.Itoa(o.writes)
}

func (o *memObjects) Put(_ context.Context, key string, r io.Reader, size int64, contentType string) error {
	b, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return errors.New("put: wrong size")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.put(key, b, contentType)
	return nil
}

func (o *memObjects) PresignPost(_ context.Context, key string, size int64, expiry time.Duration) (domain.PostUpload, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.posts == nil {
		o.posts = map[string]int64{}
	}
	o.posts[key] = size
	return domain.PostUpload{
		URL: &url.URL{Scheme: "https", Host: "bucket", Path: "/"}, Fields: map[string]string{"key": key},
		ExpiresAt: time.Now().Add(expiry),
	}, nil
}

// post is the browser's POST: storage keeps it only at the size the policy allows.
func (o *memObjects) post(key string, data []byte) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	size, ok := o.posts[key]
	if !ok || size != int64(len(data)) {
		return false
	}
	o.put(key, data, "application/octet-stream")
	return true
}

func (o *memObjects) Stat(_ context.Context, key string) (domain.ObjectVersion, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	b, ok := o.objects[key]
	if !ok {
		return domain.ObjectVersion{}, domain.ErrObjectMissing
	}
	return domain.ObjectVersion{Size: int64(len(b)), ETag: o.etags[key]}, nil
}

func (o *memObjects) version(key, etag string) ([]byte, error) {
	b, ok := o.objects[key]
	switch {
	case !ok:
		return nil, domain.ErrObjectMissing
	case o.etags[key] != etag:
		return nil, domain.ErrObjectChanged
	}
	return b, nil
}

func (o *memObjects) Open(_ context.Context, key, etag string) (io.ReadCloser, error) {
	if o.beforeOpen != nil {
		o.beforeOpen(key)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	b, err := o.version(key, etag)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (o *memObjects) Copy(_ context.Context, src, dst, etag, contentType string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	b, err := o.version(src, etag)
	if err != nil {
		return err
	}
	o.copies++
	o.put(dst, bytes.Clone(b), contentType)
	return nil
}

func (o *memObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.objects, key)
	o.deleted = append(o.deleted, key)
	return nil
}

func (o *memObjects) PresignDownload(_ context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error) {
	return &url.URL{Scheme: "https", Host: "bucket", Path: "/" + key, RawQuery: url.Values{
		"name": {filename}, "type": {contentType}, "expiry": {expiry.String()},
	}.Encode()}, nil
}

// memRemover records the keys it was asked to delete.
type memRemover struct {
	mu   sync.Mutex
	keys []string
}

func (r *memRemover) RemoveAfterCommit(_ context.Context, keys []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, keys...)
}

// fileEnv is env with the file operations.
type fileEnv struct {
	*env
	now     time.Time
	files   *memFiles
	objects *memObjects
	remover *memRemover
	figures *memLimiter
	saves   *memLimiter
}

func newFileEnv() *fileEnv {
	e := newEnv()
	fe := &fileEnv{
		env: e, files: newMemFiles(e.store), objects: &memObjects{}, remover: &memRemover{},
		figures: &memLimiter{}, saves: &memLimiter{}, now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC),
	}
	fe.files.now = func() time.Time { return fe.now }
	fe.service = New(Deps{
		Store: e.store, Tx: memTx{s: e.store}, Permissions: tablePerms{}, Actors: e.actors,
		CreateLimiter: e.create, SearchLimiter: e.search, Watcher: e.watcher,
		Members: e.members, Notifier: e.notifier, JoinLimiter: e.join, InviteLimiter: e.invite,
		FileDeps: FileDeps{
			Files: fe.files, Figures: memIntake{}, Objects: fe.objects, Remover: fe.remover,
			FigureLimiter: fe.figures, SaveLimiter: fe.saves,
			Now: func() time.Time { return fe.now },
		},
	})
	return fe
}

// withMember gives user role in project.
func (e *fileEnv) withMember(project, user string, role domain.Role) {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	e.store.members[project][user] = role
}

// upload sends a figure the way the browser does: start, POST to storage, complete.
func (e *fileEnv) upload(ctx context.Context, user, project, path string, data []byte) (domain.File, error) {
	t, err := e.service.StartFigureUpload(ctx, user, project, path, int64(len(data)))
	if err != nil {
		return domain.File{}, err
	}
	if !e.objects.post(t.Fields["key"], data) {
		return domain.File{}, errors.New("storage refused the POST")
	}
	return e.service.CompleteFigureUpload(ctx, user, project, t.ID)
}
