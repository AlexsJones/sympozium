package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedTestDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	if err := migrateMembraneColumns(db); err != nil {
		t.Fatalf("migrateMembraneColumns: %v", err)
	}
}

// helper to store with defaults for backward-compat tests.
func storeDefault(t *testing.T, db *sql.DB, content string, tags []string) int64 {
	t.Helper()
	id, _, _, err := storeMemory(db, content, tags, "public", "", 0)
	if err != nil {
		t.Fatalf("storeMemory: %v", err)
	}
	return id
}

// ── initSchema tests ─────────────────────────────────────────────────────────

func TestInitSchema(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	// Verify the memories table exists.
	var tableName string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='memories'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("memories table not found: %v", err)
	}

	// Verify the FTS5 virtual table exists.
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='memories_fts'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("memories_fts virtual table not found: %v", err)
	}

	// Verify idempotency — calling again should not fail.
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema (idempotent): %v", err)
	}
}

// ── Membrane migration tests ────────────────────────────────────────────────

func TestMigrateMembraneColumns(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	if err := migrateMembraneColumns(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}

	// Verify columns exist.
	for _, col := range []string{"visibility", "source_agent", "parent_id", "seq"} {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('memories') WHERE name=?`, col).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("column %q missing after migration", col)
		}
	}
}

func TestMigrateMembraneColumns_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	if err := migrateMembraneColumns(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := migrateMembraneColumns(db); err != nil {
		t.Fatalf("second migration should be idempotent: %v", err)
	}
}

// ── Core database function tests ─────────────────────────────────────────────

func TestStoreMemory(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	id, seq, storedAt, err := storeMemory(db, "Kafka consumer lag detected", []string{"kafka", "payments"}, "public", "researcher", 0)
	if err != nil {
		t.Fatalf("storeMemory: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
	if seq <= 0 {
		t.Errorf("expected positive seq, got %d", seq)
	}
	if storedAt == "" {
		t.Error("expected non-empty stored_at")
	}
}

func TestStoreMemory_WithVisibility(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	for _, vis := range []string{"public", "trusted", "private"} {
		id, _, _, err := storeMemory(db, "entry-"+vis, nil, vis, "agent-a", 0)
		if err != nil {
			t.Fatalf("storeMemory(%s): %v", vis, err)
		}

		var got string
		err = db.QueryRow(`SELECT visibility FROM memories WHERE id = ?`, id).Scan(&got)
		if err != nil {
			t.Fatalf("query visibility: %v", err)
		}
		if got != vis {
			t.Errorf("visibility = %q, want %q", got, vis)
		}
	}
}

func TestStoreMemory_SequenceNumbers(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	_, seq1, _, _ := storeMemory(db, "first", nil, "public", "", 0)
	_, seq2, _, _ := storeMemory(db, "second", nil, "public", "", 0)
	_, seq3, _, _ := storeMemory(db, "third", nil, "public", "", 0)

	if seq2 != seq1+1 || seq3 != seq2+1 {
		t.Errorf("expected monotonic seq: %d, %d, %d", seq1, seq2, seq3)
	}
}

func TestSearchMemories(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "Kafka consumer lag detected in payments namespace", []string{"kafka"})
	storeDefault(t, db, "OOM crash in checkout service", []string{"oom"})
	storeDefault(t, db, "Deployment rollback completed for auth service", nil)

	results, err := searchMemories(db, "kafka consumer", 5, "", nil, nil, "")
	if err != nil {
		t.Fatalf("searchMemories: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result")
	}
	if results[0].Content != "Kafka consumer lag detected in payments namespace" {
		t.Errorf("first result content = %q", results[0].Content)
	}
}

func TestSearchMemories_VisibilityFilter(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeMemory(db, "kafka public finding", nil, "public", "agent-a", 0)
	storeMemory(db, "kafka trusted secret", nil, "trusted", "agent-a", 0)
	storeMemory(db, "kafka private note", nil, "private", "agent-a", 0)

	// agent-b with no trust relationship should only see public
	results, err := searchMemories(db, "kafka", 10, "agent-b", nil, nil, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (public only), got %d", len(results))
	}
	if results[0].Content != "kafka public finding" {
		t.Errorf("expected kafka public finding, got %q", results[0].Content)
	}
}

func TestSearchMemories_TrustPeers(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeMemory(db, "kafka public finding", nil, "public", "agent-a", 0)
	storeMemory(db, "kafka trusted secret", nil, "trusted", "agent-a", 0)
	storeMemory(db, "kafka private note", nil, "private", "agent-a", 0)

	// agent-b trusts agent-a → should see public + trusted
	results, err := searchMemories(db, "kafka", 10, "agent-b", []string{"agent-a"}, nil, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (public + trusted), got %d", len(results))
	}
}

func TestSearchMemories_CallerSeesOwnPrivate(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeMemory(db, "kafka private note", nil, "private", "agent-a", 0)

	// agent-a should see their own private entries
	results, err := searchMemories(db, "kafka", 10, "agent-a", nil, nil, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (own private), got %d", len(results))
	}
}

func TestSearchMemories_TimeDecay(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	// Store an entry with a very old timestamp.
	_, err := db.Exec(`
		INSERT INTO memories (content, tags, visibility, source_agent, parent_id, seq, created_at, updated_at)
		VALUES (?, '', 'public', '', 0, 1, '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z')
	`, "ancient entry")
	if err != nil {
		t.Fatalf("insert old entry: %v", err)
	}
	// FTS trigger fires on INSERT, so the entry is indexed.

	storeMemory(db, "recent entry", nil, "public", "", 0)

	// With max_age=24h, only the recent entry should appear.
	results, err := searchMemories(db, "entry", 10, "", nil, nil, "24h")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (recent only), got %d", len(results))
	}
	if results[0].Content != "recent entry" {
		t.Errorf("expected recent entry, got %q", results[0].Content)
	}
}

func TestSearchMemories_Fallback(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "the payments service had an OOM kill event", nil)

	results, err := searchMemories(db, "OOM kill", 5, "", nil, nil, "")
	if err != nil {
		t.Fatalf("searchMemories fallback: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected LIKE fallback to find the entry")
	}
}

func TestListMemories(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "first entry", nil)
	storeDefault(t, db, "second entry", nil)
	storeDefault(t, db, "third entry", nil)

	results, err := listMemories(db, "", 20, "", nil, "")
	if err != nil {
		t.Fatalf("listMemories: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(results))
	}
}

func TestListMemories_WithTags(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "kafka issue", []string{"kafka", "infra"})
	storeDefault(t, db, "redis issue", []string{"redis", "infra"})
	storeDefault(t, db, "code review notes", []string{"review"})

	results, err := listMemories(db, "kafka", 20, "", nil, "")
	if err != nil {
		t.Fatalf("listMemories with tags: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry with kafka tag, got %d", len(results))
	}
	if results[0].Content != "kafka issue" {
		t.Errorf("entry content = %q", results[0].Content)
	}
}

// ── Provenance tests ────────────────────────────────────────────────────────

func TestProvenanceChain(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	// Create a chain: root → child → grandchild
	rootID, _, _, _ := storeMemory(db, "root finding", nil, "public", "agent-a", 0)
	childID, _, _, _ := storeMemory(db, "derived insight", nil, "public", "agent-b", rootID)
	grandchildID, _, _, _ := storeMemory(db, "final conclusion", nil, "public", "agent-c", childID)

	chain, err := getProvenanceChain(db, grandchildID)
	if err != nil {
		t.Fatalf("getProvenanceChain: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected chain of 3, got %d", len(chain))
	}
	// Should be root-first order.
	if chain[0].ID != rootID {
		t.Errorf("chain[0].ID = %d, want root %d", chain[0].ID, rootID)
	}
	if chain[1].ID != childID {
		t.Errorf("chain[1].ID = %d, want child %d", chain[1].ID, childID)
	}
	if chain[2].ID != grandchildID {
		t.Errorf("chain[2].ID = %d, want grandchild %d", chain[2].ID, grandchildID)
	}
}

// ── fts5Query tests ──────────────────────────────────────────────────────────

func TestFts5Query(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"kafka consumer", "kafka* AND consumer*"},
		{"single", "single*"},
		{"", ""},
		{`special "chars" and (parens)`, "special* AND chars* AND and* AND parens*"},
		{"***", "***"}, // all chars stripped → empty terms → returns original
	}

	for _, tt := range tests {
		got := fts5Query(tt.input)
		if got != tt.want {
			t.Errorf("fts5Query(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── HTTP handler tests ───────────────────────────────────────────────────────

func TestHealthHandler(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", w.Body.String())
	}
}

func TestStoreHandler(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	body := `{"content":"Kafka lag in payments","tags":["kafka","payments"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/store", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	storeHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	// Content should contain id, seq, and stored_at.
	contentBytes, _ := json.Marshal(resp.Content)
	var stored map[string]any
	if err := json.Unmarshal(contentBytes, &stored); err != nil {
		t.Fatalf("parse content: %v", err)
	}
	if id, ok := stored["id"].(float64); !ok || id <= 0 {
		t.Errorf("expected positive id, got %v", stored["id"])
	}
	if seq, ok := stored["seq"].(float64); !ok || seq <= 0 {
		t.Errorf("expected positive seq, got %v", stored["seq"])
	}
}

func TestStoreHandler_WithVisibility(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	body := `{"content":"trusted finding","tags":["test"],"visibility":"trusted","source_agent":"researcher"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/store", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	storeHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify the stored entry has correct visibility.
	var vis, src string
	err := db.QueryRow(`SELECT visibility, source_agent FROM memories WHERE id = 1`).Scan(&vis, &src)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if vis != "trusted" {
		t.Errorf("visibility = %q, want trusted", vis)
	}
	if src != "researcher" {
		t.Errorf("source_agent = %q, want researcher", src)
	}
}

func TestStoreHandler_MissingContent(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	body := `{}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/store", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	storeHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestStoreHandler_InvalidJSON(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/store", bytes.NewBufferString("not json"))
	r.Header.Set("Content-Type", "application/json")

	storeHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSearchHandler(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "Kafka consumer lag detected in payments namespace", []string{"kafka"})
	storeDefault(t, db, "OOM crash in checkout service", []string{"oom"})

	body := `{"query":"kafka consumer","top_k":5}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/search", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	searchHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	contentBytes, _ := json.Marshal(resp.Content)
	var entries []memoryEntry
	if err := json.Unmarshal(contentBytes, &entries); err != nil {
		t.Fatalf("parse content: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 search result")
	}
	if entries[0].Content != "Kafka consumer lag detected in payments namespace" {
		t.Errorf("first result content = %q", entries[0].Content)
	}
}

func TestSearchHandler_MissingQuery(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	body := `{}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/search", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	searchHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSearchHandler_DefaultTopK(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "entry one", nil)

	body := `{"query":"entry"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/search", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	searchHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
}

func TestListHandler(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "first entry", nil)
	storeDefault(t, db, "second entry", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/list", nil)

	listHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	contentBytes, _ := json.Marshal(resp.Content)
	var entries []memoryEntry
	if err := json.Unmarshal(contentBytes, &entries); err != nil {
		t.Fatalf("parse content: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestListHandler_WithTags(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "kafka issue", []string{"kafka", "infra"})
	storeDefault(t, db, "redis issue", []string{"redis", "infra"})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/list?tags=kafka", nil)

	listHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	contentBytes, _ := json.Marshal(resp.Content)
	var entries []memoryEntry
	json.Unmarshal(contentBytes, &entries)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with kafka tag, got %d", len(entries))
	}
	if entries[0].Content != "kafka issue" {
		t.Errorf("entry content = %q", entries[0].Content)
	}
}

func TestListHandler_WithLimit(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeDefault(t, db, "entry 1", nil)
	storeDefault(t, db, "entry 2", nil)
	storeDefault(t, db, "entry 3", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/list?limit=2", nil)

	listHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	contentBytes, _ := json.Marshal(resp.Content)
	var entries []memoryEntry
	json.Unmarshal(contentBytes, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (limit=2), got %d", len(entries))
	}
}

// ── Stats handler tests ─────────────────────────────────────────────────────

func TestStatsHandler(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	storeMemory(db, "public entry", nil, "public", "agent-a", 0)
	storeMemory(db, "trusted entry", nil, "trusted", "agent-a", 0)
	storeMemory(db, "private entry", nil, "private", "agent-b", 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stats", nil)
	statsHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	contentBytes, _ := json.Marshal(resp.Content)
	var stats map[string]any
	json.Unmarshal(contentBytes, &stats)

	maxSeq, ok := stats["max_seq"].(float64)
	if !ok || maxSeq < 3 {
		t.Errorf("expected max_seq >= 3, got %v", stats["max_seq"])
	}
}

// ── Provenance handler tests ─────────────────────────────────────────────────

func TestProvenanceHandler(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	rootID, _, _, _ := storeMemory(db, "root", nil, "public", "a", 0)
	childID, _, _, _ := storeMemory(db, "child", nil, "public", "b", rootID)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/provenance?id="+itoa(childID), nil)
	provenanceHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp apiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatalf("expected success: %s", resp.Error)
	}

	contentBytes, _ := json.Marshal(resp.Content)
	var chain []memoryEntry
	json.Unmarshal(contentBytes, &chain)

	if len(chain) != 2 {
		t.Fatalf("expected chain of 2, got %d", len(chain))
	}
	if chain[0].Content != "root" || chain[1].Content != "child" {
		t.Errorf("chain = %v", chain)
	}
}

func TestProvenanceHandler_MissingID(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/provenance", nil)
	provenanceHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
