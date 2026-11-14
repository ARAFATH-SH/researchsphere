package aiclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
)

// FakeModel is the model name Fake reports.
const FakeModel = "fake-model@0000000000000000000000000000000000000000"

// Fake is an Embedder for tests: it calls no service and returns deterministic unit-length
// vectors, the same for the same kind and text. Set Err to make every call fail with it,
// and ModelName to report another model than FakeModel; set both with SetErr and SetModel
// once other goroutines use it. Calls records each Embed call, DocumentCalls each
// EmbedDocument call. Safe for concurrent use.
type Fake struct {
	mu        sync.Mutex
	Err       error
	ModelName string
	calls     []FakeCall
	docCalls  []string
}

// SetErr sets Err under the lock.
func (f *Fake) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Err = err
}

// SetModel sets ModelName under the lock.
func (f *Fake) SetModel(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ModelName = name
}

func (f *Fake) model() string {
	if f.ModelName != "" {
		return f.ModelName
	}
	return FakeModel
}

// FakeCall is one Embed call Fake received.
type FakeCall struct {
	Kind  Kind
	Texts []string
}

var _ Embedder = (*Fake)(nil)

// Embed checks the input as Client does, then returns one vector per text.
func (f *Fake) Embed(_ context.Context, kind Kind, texts []string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Kind: kind, Texts: append([]string(nil), texts...)})
	if f.Err != nil {
		return Result{}, f.Err
	}
	if err := checkInput(kind, texts); err != nil {
		return Result{}, err
	}
	res := Result{Model: f.model(), Vectors: make([][]float32, len(texts))}
	for i, t := range texts {
		res.Vectors[i] = FakeVector(kind, t)
	}
	return res, nil
}

// EmbedDocument checks the input as Client does, then returns FakeVector of the whole text
// as one chunk.
func (f *Fake) EmbedDocument(_ context.Context, text string, maxChunks int) (DocumentResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docCalls = append(f.docCalls, text)
	if f.Err != nil {
		return DocumentResult{}, f.Err
	}
	if err := checkDocument(text, maxChunks); err != nil {
		return DocumentResult{}, err
	}
	return DocumentResult{Model: f.model(), Vector: FakeVector(KindPassage, text), Chunks: 1}, nil
}

// DocumentCalls returns a copy of the texts EmbedDocument received.
func (f *Fake) DocumentCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.docCalls...)
}

// Model returns ModelName (FakeModel when empty), or Err.
func (f *Fake) Model(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	return f.model(), nil
}

// Ping returns Err.
func (f *Fake) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Err
}

// Calls returns a copy of the Embed calls so far.
func (f *Fake) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// FakeVector is the vector Fake returns for kind and text: Dimensions numbers derived from
// a hash of both, scaled to length 1.
func FakeVector(kind Kind, text string) []float32 {
	v := make([]float32, Dimensions)
	var sum float64
	var block [sha256.Size]byte
	for i := range v {
		if i%(sha256.Size/4) == 0 {
			block = sha256.Sum256([]byte(string(kind) + "\x00" + text + "\x00" + string(rune(i))))
		}
		j := (i % (sha256.Size / 4)) * 4
		x := float64(binary.BigEndian.Uint32(block[j:j+4]))/math.MaxUint32 - 0.5
		v[i] = float32(x)
		sum += x * x
	}
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}
