//go:build integration

package redis

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// A query vector comes back exactly, under its model only, for 24 h, and the key holds no
// search text.
func TestQueryVectorRoundTrip(t *testing.T) {
	l, client, _ := newTestLookups(t)
	ctx := t.Context()
	c := l.QueryVectors()
	const model, text = "intfloat/multilingual-e5-base@abc", "computers that read scans"
	if _, ok, err := c.Get(ctx, model, text); ok || err != nil {
		t.Fatalf("Get() before Set = %v, %v", ok, err)
	}
	v := domain.QueryVector{Model: model, Values: []float32{0.25, -1.5, 3.1415927, 0}}
	if err := c.Set(ctx, text, v, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, model, text)
	if err != nil || !ok || !reflect.DeepEqual(got, v.Values) {
		t.Fatalf("Get() = %v, %v, %v; want %v", got, ok, err, v.Values)
	}
	if _, ok, _ := c.Get(ctx, "other/model@x", text); ok {
		t.Error("found under another model")
	}
	keys, err := client.Keys(ctx, "*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	if strings.Contains(keys[0], "computers") || !strings.HasPrefix(keys[0], "profile:search-qvec:"+model+":") {
		t.Errorf("key = %q", keys[0])
	}
	if ttl := client.TTL(ctx, keys[0]).Val(); ttl < 23*time.Hour || ttl > 24*time.Hour {
		t.Errorf("TTL = %v, want 24h", ttl)
	}
}

// A value Set did not write is a miss, not an error.
func TestQueryVectorBadValue(t *testing.T) {
	l, client, _ := newTestLookups(t)
	ctx := t.Context()
	client.Set(ctx, QueryVectorKey("m@r", "x"), "abc", time.Minute)
	if _, ok, err := l.QueryVectors().Get(ctx, "m@r", "x"); ok || err != nil {
		t.Errorf("Get() = %v, %v; want a miss", ok, err)
	}
}
