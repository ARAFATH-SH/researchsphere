package redis

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// qvecPrefix is followed by the model and the hex SHA-256 of the normalized search text, so
// the key holds no search text (AI-5.1).
const qvecPrefix = "profile:search-qvec:"

// QueryVectors keeps query vectors as little-endian float32s.
type QueryVectors struct {
	client *goredis.Client
}

var _ domain.QueryVectorCache = QueryVectors{}

// QueryVectors returns the query vector cache, on the same client.
func (l *Lookups) QueryVectors() QueryVectors {
	return QueryVectors{client: l.client}
}

// QueryVectorKey is where the vector of text made by model is kept.
func QueryVectorKey(model, text string) string {
	sum := sha256.Sum256([]byte(text))
	return qvecPrefix + model + ":" + hex.EncodeToString(sum[:])
}

// Get returns the vector kept for text under model.
func (c QueryVectors) Get(ctx context.Context, model, text string) ([]float32, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := c.client.Get(ctx, QueryVectorKey(model, text)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get query vector: %w", err)
	}
	if len(b) == 0 || len(b)%4 != 0 {
		return nil, false, nil // not written by Set: embed again
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, true, nil
}

// Set keeps v for text under v.Model for ttl.
func (c QueryVectors) Set(ctx context.Context, text string, v domain.QueryVector, ttl time.Duration) error {
	b := make([]byte, 4*len(v.Values))
	for i, x := range v.Values {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.client.Set(ctx, QueryVectorKey(v.Model, text), b, ttl).Err(); err != nil {
		return fmt.Errorf("set query vector: %w", err)
	}
	return nil
}
