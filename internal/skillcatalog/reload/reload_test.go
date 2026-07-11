package reload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
)

// restoreCatalog snapshots the process-global catalog and restores it after the
// test, since fetchOnce mutates it via skillcatalog.Load.
func restoreCatalog(t *testing.T) {
	t.Helper()
	origJSON, err := json.Marshal(skillcatalog.Catalog())
	if err != nil {
		t.Fatalf("marshal original catalog: %v", err)
	}
	origVer := skillcatalog.Version()
	t.Cleanup(func() {
		if err := skillcatalog.Load(origJSON, origVer); err != nil {
			t.Fatalf("restore catalog: %v", err)
		}
	})
}

// markedOperationsJSON returns the live catalog's operations JSON with a marker
// summary on the first op, so a swap is observable.
func markedOperationsJSON(t *testing.T, marker string) []byte {
	t.Helper()
	ops := append([]skillcatalog.Operation(nil), skillcatalog.Catalog()...)
	if len(ops) == 0 {
		t.Fatal("empty live catalog")
	}
	ops[0].Summary = marker
	b, err := json.Marshal(ops)
	if err != nil {
		t.Fatalf("marshal ops: %v", err)
	}
	return b
}

func TestFetchOnce_200_SwapsAndPersists(t *testing.T) {
	restoreCatalog(t)

	const marker = "RELOAD-200-MARKER"
	const version = "v-200-newsha"
	opsJSON := markedOperationsJSON(t, marker)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != catalogPathSuffix {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("ETag", `"`+version+`"`)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(catalogResponse{Version: version, Operations: opsJSON})
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "nested", "skill-catalog.json")
	r := newReloader(Options{URL: srv.URL + catalogPathSuffix, FilePath: file, Client: srv.Client()})
	r.fetchOnce(context.Background())

	if skillcatalog.Version() != version {
		t.Fatalf("Version() = %q, want %q", skillcatalog.Version(), version)
	}
	if !strings.Contains(skillcatalog.Description(), marker) {
		t.Fatal("swapped catalog not visible via Description()")
	}
	// File persisted, atomic write left no temp files, and content is a valid catalog.
	onDisk, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read persisted catalog: %v", err)
	}
	if err := skillcatalog.Load(onDisk, "from-disk-check"); err != nil {
		t.Fatalf("persisted file is not a valid catalog: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestFetchOnce_304_KeepsCurrentAndSendsINM(t *testing.T) {
	restoreCatalog(t)

	// Establish a known baseline version the loader will echo as If-None-Match.
	const baseVer = "v-base-304"
	if err := skillcatalog.Load(markedOperationsJSON(t, "BASE-304"), baseVer); err != nil {
		t.Fatalf("seed baseline: %v", err)
	}

	var gotINM string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotINM = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	r := newReloader(Options{URL: srv.URL + catalogPathSuffix, Client: srv.Client()})
	r.fetchOnce(context.Background())

	if gotINM != baseVer {
		t.Fatalf("If-None-Match = %q, want %q", gotINM, baseVer)
	}
	if skillcatalog.Version() != baseVer {
		t.Fatalf("304 changed the catalog: Version() = %q, want %q", skillcatalog.Version(), baseVer)
	}
}

func TestFetchOnce_ErrorsKeepCurrent(t *testing.T) {
	const baseVer = "v-base-err"

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }},
		{"malformed envelope", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"x","operations": not-json}`))
		}},
		{"invalid catalog (empty ops)", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"x","operations":[]}`))
		}},
		{"200 without version", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"operations":[{"ID":"a.b","Skill":"a","Method":"GET","Path":"/a/b"}]}`))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restoreCatalog(t)
			if err := skillcatalog.Load(markedOperationsJSON(t, "BASE-"+c.name), baseVer); err != nil {
				t.Fatalf("seed baseline: %v", err)
			}
			srv := httptest.NewServer(c.handler)
			defer srv.Close()

			r := newReloader(Options{URL: srv.URL + catalogPathSuffix, Client: srv.Client()})
			r.fetchOnce(context.Background())

			if skillcatalog.Version() != baseVer {
				t.Fatalf("error path changed the catalog: Version() = %q, want %q", skillcatalog.Version(), baseVer)
			}
		})
	}
}

func TestFetchOnce_NetworkErrorKeepsCurrent(t *testing.T) {
	restoreCatalog(t)
	const baseVer = "v-base-net"
	if err := skillcatalog.Load(markedOperationsJSON(t, "BASE-net"), baseVer); err != nil {
		t.Fatalf("seed baseline: %v", err)
	}
	// Point at a closed server so the request fails at the transport layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + catalogPathSuffix
	srv.Close()

	r := newReloader(Options{URL: url, Client: &http.Client{}})
	r.fetchOnce(context.Background())

	if skillcatalog.Version() != baseVer {
		t.Fatalf("network error changed the catalog: Version() = %q, want %q", skillcatalog.Version(), baseVer)
	}
}
