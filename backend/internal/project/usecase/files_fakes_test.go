package usecase

import (
	"context"
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
}

type memFile struct {
	domain.StoredFile
}

func newMemFiles(s *memStore) *memFiles {
	return &memFiles{s: s, files: map[string]*memFile{}, main: map[string]string{}, names: map[string]string{}}
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

// memIntake accepts data that starts with "PNG", "JPG" or "PDF" as that type, refuses the
// rest, and makes an image grow by grow bytes when encoded again.
type memIntake struct{ grow int }

func (i memIntake) PrepareFigure(_ context.Context, data []byte) (domain.PreparedFigure, error) {
	switch {
	case strings.HasPrefix(string(data), "PNG"):
		return domain.PreparedFigure{Data: append(append([]byte{}, data...), make([]byte, i.grow)...), ContentType: TypePNG, Width: 1, Height: 1}, nil
	case strings.HasPrefix(string(data), "JPG"):
		return domain.PreparedFigure{Data: data, ContentType: TypeJPEG, Width: 1, Height: 1}, nil
	case strings.HasPrefix(string(data), "PDF"):
		return domain.PreparedFigure{Data: data, ContentType: TypePDF}, nil
	}
	return domain.PreparedFigure{}, domain.ErrUnsupportedType
}

// memObjects is the bucket in memory.
type memObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func (o *memObjects) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.objects == nil {
		o.objects = map[string][]byte{}
	}
	o.objects[key] = b
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
		figures: &memLimiter{}, saves: &memLimiter{},
	}
	fe.service = New(Deps{
		Store: e.store, Tx: memTx{s: e.store}, Permissions: tablePerms{}, Actors: e.actors,
		CreateLimiter: e.create, SearchLimiter: e.search, Watcher: e.watcher,
		Members: e.members, Notifier: e.notifier, JoinLimiter: e.join, InviteLimiter: e.invite,
		FileDeps: FileDeps{
			Files: fe.files, Figures: memIntake{}, Objects: fe.objects, Remover: fe.remover,
			FigureLimiter: fe.figures, SaveLimiter: fe.saves,
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
