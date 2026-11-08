package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type itemDoc struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Qty  int64  `json:"qty"`
}

func TestAPI(t *testing.T) {
	db, err := openDB(t.Context(), filepath.Join(t.TempDir(), "items.db"))
	if err != nil {
		t.Fatalf("openDB = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := newApp(db)

	do := func(method, target, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, target, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	// Порожня таблиця — "[]", не "null".
	if code, body := do("GET", "/items", ""); code != 200 || body != "[]" {
		t.Fatalf("GET /items on empty db = %d %q, want 200 []", code, body)
	}

	for _, name := range []string{"a", "b", "c"} {
		code, body := do("POST", "/items", `{"name":"`+name+`","qty":2}`)
		var it itemDoc
		if err := json.Unmarshal([]byte(body), &it); code != 201 || err != nil || it.ID == 0 || it.Name != name || it.Qty != 2 {
			t.Fatalf("POST /items %q = %d %q, want 201 with id, name and qty", name, code, body)
		}
	}

	cases := []struct {
		name, method, target, body string
		wantCode                   int
		wantBody                   string // порожній — тіло не перевіряється
	}{
		{"create invalid json", "POST", "/items", `{"name":`, 400, ""},
		{"create empty name", "POST", "/items", `{"name":"","qty":1}`, 400, ""},
		{"get existing", "GET", "/items/2", "", 200, `{"id":2,"name":"b","qty":2}`},
		{"get missing", "GET", "/items/999", "", 404, ""},
		{"get bad id", "GET", "/items/abc", "", 400, ""},
		{"list first page", "GET", "/items?limit=2", "", 200, `[{"id":1,"name":"a","qty":2},{"id":2,"name":"b","qty":2}]`},
		{"list after", "GET", "/items?after=2", "", 200, `[{"id":3,"name":"c","qty":2}]`},
		{"list past end", "GET", "/items?after=3", "", 200, `[]`},
		{"list bad limit", "GET", "/items?limit=0", "", 400, ""},
		{"fanout zero", "GET", "/fanout?n=0", "", 400, ""},
		{"fanout over limit", "GET", "/fanout?n=100001", "", 400, ""},
		{"fanout not a number", "GET", "/fanout?n=abc", "", 400, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := do(tc.method, tc.target, tc.body)
			if code != tc.wantCode || tc.wantBody != "" && body != tc.wantBody {
				t.Errorf("%s %s = %d %q, want %d %q", tc.method, tc.target, code, body, tc.wantCode, tc.wantBody)
			}
		})
	}
	t.Run("fanout runs n goroutines at once", func(t *testing.T) {
		const n = 200
		code, body := do("GET", "/fanout?n=200", "")
		var got struct {
			N, Peak, Rows int64
		}
		if err := json.Unmarshal([]byte(body), &got); code != 200 || err != nil {
			t.Fatalf("GET /fanout?n=%d = %d %q, want 200 JSON", n, code, body)
		}
		// Кожна горутина читає count(*) з трьох рядків; пік — щонайменше n живих разом.
		if got.N != n || got.Rows != 3*n || got.Peak < n {
			t.Fatalf("GET /fanout?n=%d = %+v, want n=%d rows=%d peak>=%d", n, got, n, 3*n, n)
		}
	})
}
