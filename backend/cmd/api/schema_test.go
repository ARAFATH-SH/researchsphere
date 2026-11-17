package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The constant names the newest migration file, so the API refuses a database behind it.
func TestRequiredSchemaVersion(t *testing.T) {
	entries, err := os.ReadDir("../../db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	newest := 0
	for _, e := range entries {
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err == nil && n > newest {
			newest = n
		}
	}
	if newest != requiredSchemaVersion {
		t.Errorf("requiredSchemaVersion = %d, newest migration %d", requiredSchemaVersion, newest)
	}
}

func TestCompareSchema(t *testing.T) {
	if err := compareSchema(requiredSchemaVersion, false); err != nil {
		t.Errorf("current: %v", err)
	}
	if err := compareSchema(requiredSchemaVersion+1, false); err != nil {
		t.Errorf("newer database: %v", err)
	}
	err := compareSchema(requiredSchemaVersion-1, false)
	if !errors.Is(err, errSchemaBehind) || !strings.Contains(err.Error(), "run make migrate") {
		t.Errorf("behind: %v", err)
	}
	if err := compareSchema(requiredSchemaVersion, true); !errors.Is(err, errSchemaBehind) {
		t.Errorf("dirty: %v", err)
	}
}
