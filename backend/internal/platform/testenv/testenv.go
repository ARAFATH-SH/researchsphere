// Package testenv gives integration tests their own PostgreSQL database and Redis DB,
// emptied before each test, and values that no other test run uses. It is imported only
// by test files.
//
// The settings come from TEST_DATABASE_URL and TEST_REDIS_URL (the Makefile sets both).
// Because every test empties the whole test database, the guards below refuse a database
// whose name does not end in _test and a Redis DB that is 0 or the one REDIS_URL uses, so
// a wrong setting can never wipe development data.
package testenv

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
)

// truncateAll empties every table except the migration record and the reference data the
// migrations seed and no test changes: the research fields of venues (000039). Truncating a
// table that refers to them leaves them alone. It takes no input from Go; format('%I')
// quotes the table names.
const truncateAll = `DO $$
BEGIN
    EXECUTE (
        SELECT 'TRUNCATE ' || string_agg(format('%I.%I', schemaname, tablename), ', ') || ' RESTART IDENTITY CASCADE'
        FROM pg_tables
        WHERE schemaname = 'public' AND tablename NOT IN ('schema_migrations', 'research_fields')
    );
END
$$`

// CheckDatabaseURL returns an error unless url is a PostgreSQL URL whose database name
// ends in _test.
func CheckDatabaseURL(url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("TEST_DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return errors.New("TEST_DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	return checkDatabaseName(cfg.Database)
}

func checkDatabaseName(name string) error {
	if !strings.HasSuffix(name, "_test") || name == "_test" {
		return fmt.Errorf("refusing to run: database %q does not end in _test, and the tests empty every table", name)
	}
	return nil
}

// CheckRedisURL returns an error unless testURL selects a Redis DB other than 0 and other
// than the one devURL (REDIS_URL) selects. devURL may be empty.
func CheckRedisURL(testURL, devURL string) error {
	if strings.TrimSpace(testURL) == "" {
		return errors.New("TEST_REDIS_URL is not set")
	}
	opts, err := goredis.ParseURL(testURL)
	if err != nil {
		return errors.New("TEST_REDIS_URL is not accepted by the Redis client")
	}
	if opts.DB == 0 {
		return errors.New("refusing to run: TEST_REDIS_URL selects Redis DB 0, and the tests flush it; use a separate DB such as /15")
	}
	if devURL != "" {
		dev, err := goredis.ParseURL(devURL)
		if err == nil && dev.Addr == opts.Addr && dev.DB == opts.DB {
			return fmt.Errorf("refusing to run: TEST_REDIS_URL selects DB %d, the same as REDIS_URL", opts.DB)
		}
	}
	return nil
}

// DB connects to the test database, empties every table and returns the pool, which is
// closed when the test ends.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if err := CheckDatabaseURL(url); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	t.Cleanup(pool.Close)
	// Check the name again on the server, in case the connection ended up elsewhere.
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("test database: %v", err)
	}
	if err := checkDatabaseName(name); err != nil {
		t.Fatal(err)
	}
	// TRUNCATE waits for every open transaction on the tables. A short lock timeout turns
	// a test that empties the database while it still holds a transaction into a failure
	// instead of a hang.
	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "SET LOCAL lock_timeout = '10s'"); err != nil {
			return err
		}
		_, err := tx.Exec(t.Context(), truncateAll)
		return err
	})
	if err != nil {
		t.Fatalf("empty the test database (call DB once per test, before opening transactions): %v", err)
	}
	return pool
}

// Redis connects to the test Redis DB, flushes that DB only and returns the client, which
// is closed when the test ends.
func Redis(t *testing.T) *goredis.Client {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if err := CheckRedisURL(url, os.Getenv("REDIS_URL")); err != nil {
		t.Fatal(err)
	}
	opts, _ := goredis.ParseURL(url) // checked above
	platformredis.ApplyDefaults(opts)
	client := goredis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.FlushDB(t.Context()).Err(); err != nil {
		t.Fatalf("flush the test Redis DB: %v", err)
	}
	return client
}

// Word returns a random capitalized word of nine letters, starting with Q, that no other
// test run uses. After the Q it has only the letters a to w, so a test can make a typo by
// putting x, y or z in place of a letter and be sure the text changed.
func Word(t *testing.T) string {
	t.Helper()
	const letters = "abcdefghijklmnopqrstuvw"
	b := []byte{'Q', 0, 0, 0, 0, 0, 0, 0, 0}
	for i := 1; i < len(b); i++ {
		b[i] = letters[randInt(t, int64(len(letters)))]
	}
	return string(b)
}

// Email returns an address that no other test run uses.
func Email(t *testing.T) string {
	t.Helper()
	return "it-" + strings.ToLower(rand.Text()) + "@example.org"
}

// ORCIDiD returns a random ORCID iD with a valid check digit.
func ORCIDiD(t *testing.T) string {
	t.Helper()
	digits := fmt.Sprintf("%015d", randInt(t, 1e15))
	total := 0
	for _, r := range digits {
		total = (total + int(r-'0')) * 2
	}
	check := (12 - total%11) % 11
	last := fmt.Sprint(check)
	if check == 10 {
		last = "X"
	}
	s := digits + last
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func randInt(t *testing.T, n int64) int64 {
	t.Helper()
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		t.Fatal(err)
	}
	return v.Int64()
}
