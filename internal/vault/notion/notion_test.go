package notion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// fakePage is one page of the fake Notion database.
type fakePage struct {
	ID         string
	Title      string
	Type       string
	Tier       string
	Date       string
	Source     string
	Native     string
	NativeType string
	Extra      string
	Body       string
	LastEdited time.Time
}

// fakeAPI is a small stand-in for the Notion API: it keeps the pages in
// memory and speaks the part of the protocol this gateway uses.
type fakeAPI struct {
	pages map[string]*fakePage
	next  int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{pages: map[string]*fakePage{}}
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/pages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Properties map[string]any `json:"properties"`
			Children   []any          `json:"children"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.next++
		page := &fakePage{ID: fmt.Sprintf("page-%d", f.next), LastEdited: time.Now()}
		page.Title = titleOf(body.Properties)
		page.Type = selectOf(body.Properties, PropType)
		page.Tier = selectOf(body.Properties, PropTier)
		page.Native = selectOf(body.Properties, PropNative)
		page.NativeType = selectOf(body.Properties, PropNativeType)
		page.Source = richTextOf(body.Properties, PropSource)
		page.Extra = richTextOf(body.Properties, PropExtra)
		if date, ok := body.Properties[PropDate].(map[string]any); ok {
			if d, ok := date["date"].(map[string]any); ok {
				page.Date, _ = d["start"].(string)
			}
		}
		page.Body = bodyOf(body.Children)
		f.pages[page.ID] = page
		writeJSON(w, page)
	})

	mux.HandleFunc("/v1/pages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/pages/")
		page, ok := f.pages[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if title := titleOf(body.Properties); title != "" {
			page.Title = title
		}
		if v := selectOf(body.Properties, PropType); v != "" {
			page.Type = v
		}
		if v := selectOf(body.Properties, PropTier); v != "" {
			page.Tier = v
		}
		page.LastEdited = time.Now()
		writeJSON(w, page)
	})

	mux.HandleFunc("/v1/databases/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/query") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var results []any
		for _, page := range f.pages {
			results = append(results, page)
		}
		writeJSON(w, map[string]any{"results": results, "has_more": false})
	})

	mux.HandleFunc("/v1/blocks/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/blocks/"), "/children")
		page, ok := f.pages[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"results": childrenOf(page.Body)})
		case http.MethodDelete:
			page.Body = ""
			writeJSON(w, map[string]any{})
		case http.MethodPatch:
			var body struct {
				Children []any `json:"children"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			page.Body = bodyOf(body.Children)
			writeJSON(w, map[string]any{})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		var results []any
		for _, page := range f.pages {
			results = append(results, page)
		}
		writeJSON(w, map[string]any{"results": results})
	})

	return mux
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// titleOf reads the title property of a payload.
func titleOf(props map[string]any) string {
	prop, ok := props[PropTitle].(map[string]any)
	if !ok {
		return ""
	}
	list, ok := prop["title"].([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	item, ok := list[0].(map[string]any)
	if !ok {
		return ""
	}
	text, ok := item["text"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := text["content"].(string)
	return content
}

// selectOf reads a select property of a payload.
func selectOf(props map[string]any, name string) string {
	prop, ok := props[name].(map[string]any)
	if !ok {
		return ""
	}
	sel, ok := prop["select"].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := sel["name"].(string)
	return value
}

// richTextOf reads a rich text property of a payload.
func richTextOf(props map[string]any, name string) string {
	prop, ok := props[name].(map[string]any)
	if !ok {
		return ""
	}
	list, ok := prop["rich_text"].([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	item, ok := list[0].(map[string]any)
	if !ok {
		return ""
	}
	text, ok := item["text"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := text["content"].(string)
	return content
}

// bodyOf reads the text of the children of a page.
func bodyOf(children []any) string {
	var lines []string
	for _, child := range children {
		c, ok := child.(map[string]any)
		if !ok {
			continue
		}
		paragraph, ok := c["paragraph"].(map[string]any)
		if !ok {
			continue
		}
		rich, ok := paragraph["rich_text"].([]any)
		if !ok || len(rich) == 0 {
			continue
		}
		item, ok := rich[0].(map[string]any)
		if !ok {
			continue
		}
		text, ok := item["text"].(map[string]any)
		if !ok {
			continue
		}
		content, _ := text["content"].(string)
		lines = append(lines, content)
	}
	return strings.Join(lines, "\n")
}

// childrenOf renders the body of a page as children.
func childrenOf(body string) []any {
	var out []any
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, map[string]any{
			"type": "paragraph",
			"paragraph": map[string]any{
				"rich_text": []any{map[string]any{"plain_text": line}},
			},
		})
	}
	return out
}

// MarshalJSON renders a fake page the way the Notion API does.
func (p *fakePage) MarshalJSON() ([]byte, error) {
	props := map[string]any{
		PropTitle: map[string]any{
			"title": []any{map[string]any{"plain_text": p.Title}},
		},
		PropType:       map[string]any{"select": map[string]any{"name": p.Type}},
		PropTier:       map[string]any{"select": map[string]any{"name": p.Tier}},
		PropNative:     map[string]any{"select": map[string]any{"name": p.Native}},
		PropNativeType: map[string]any{"select": map[string]any{"name": p.NativeType}},
		PropSource:     map[string]any{"rich_text": []any{map[string]any{"plain_text": p.Source}}},
		PropExtra:      map[string]any{"rich_text": []any{map[string]any{"plain_text": p.Extra}}},
	}
	if p.Date != "" {
		props[PropDate] = map[string]any{"date": map[string]any{"start": p.Date}}
	}
	return json.Marshal(map[string]any{
		"id":               p.ID,
		"last_edited_time": p.LastEdited.Format(time.RFC3339),
		"properties":       props,
	})
}

// newTestStore builds a store pointing at the fake API.
func newTestStore(t *testing.T) (*Store, *fakeAPI) {
	t.Helper()
	api := newFakeAPI()
	server := httptest.NewServer(api.handler(t))
	t.Cleanup(server.Close)
	return &Store{
		token:    "test-token",
		database: "database-id",
		baseURL:  server.URL + "/v1",
		client:   server.Client(),
	}, api
}

// TestTheNotionGatewayCarriesTheCorpus checks the round trip through the
// fake Notion API.
func TestTheNotionGatewayCarriesTheCorpus(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := t.Context()

	entry := memory.Entry{
		Title:      "CI is not the test loop",
		Type:       memory.TypeUser,
		Date:       "2026-11-07",
		Body:       "CI confirms, it does not find the bug.",
		Tier:       memory.TierHot,
		Source:     memory.DefaultSource,
		Native:     "minimax-user",
		NativeType: "user",
	}
	if _, err := store.Create(ctx, entry); err != nil {
		t.Fatalf("cannot create the page: %v", err)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("cannot read the corpus: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the corpus has %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.Title != entry.Title {
		t.Fatalf("the title is %q, want %q", got.Title, entry.Title)
	}
	if got.Type != entry.Type {
		t.Fatalf("the type is %q, want %q", got.Type, entry.Type)
	}
	if got.Tier != entry.Tier {
		t.Fatalf("the tier is %q, want %q", got.Tier, entry.Tier)
	}
	if got.Body != entry.Body {
		t.Fatalf("the body is %q, want %q", got.Body, entry.Body)
	}
	if got.ModTime.IsZero() {
		t.Fatal("the entry has no modification time")
	}
}

// TestTheNotionGatewayUpdatesAPage checks the update path.
func TestTheNotionGatewayUpdatesAPage(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := t.Context()

	entry := memory.Entry{
		Title:  "A lesson",
		Type:   memory.TypeLesson,
		Body:   "The first body.",
		Tier:   memory.TierCold,
		Source: memory.DefaultSource,
	}
	stored, err := store.Create(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	stored.Body = "The second body."
	if err := store.Update(ctx, stored); err != nil {
		t.Fatalf("cannot update the page: %v", err)
	}
	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Body != "The second body." {
		t.Fatalf("the update did not reach the page: %+v", entries)
	}
}

// TestTheNotionGatewaySearches checks the search path.
func TestTheNotionGatewaySearches(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := t.Context()
	if _, err := store.Create(ctx, memory.Entry{
		Title: "CI is not the test loop",
		Type:  memory.TypeUser,
		Body:  "CI confirms, it does not find the bug.",
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(ctx, "CI", 10)
	if err != nil {
		t.Fatalf("cannot search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("the search found %d hits, want 1", len(hits))
	}
	if hits[0].Title != "CI is not the test loop" {
		t.Fatalf("the hit is %q", hits[0].Title)
	}
}

// TestTheNotionGatewayNeedsAToken checks the configuration guard.
func TestTheNotionGatewayNeedsAToken(t *testing.T) {
	_, err := newStore(vault.Config{NotionDatabase: "database-id"})
	if err == nil {
		t.Fatal("the gateway opened without a token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("the error does not mention the token: %v", err)
	}
}
