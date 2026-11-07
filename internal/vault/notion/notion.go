// Package notion is the Notion gateway: the same corpus in a Notion
// database, so the memory is read and written where the rest of the notes
// live.
//
// Each entry is a page. The title and the metadata are properties; the body
// is the content of the page.
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// DefaultAPIURL is the Notion API root.
const DefaultAPIURL = "https://api.notion.com/v1"

// DefaultTokenEnv is the environment variable the token is read from.
const DefaultTokenEnv = "NOTION_TOKEN"

// Property names on the Notion database.
const (
	PropTitle       = "Name"
	PropType        = "Type"
	PropTier        = "Tier"
	PropDate        = "Date"
	PropSource      = "Source"
	PropNative      = "Native"
	PropNativeType  = "NativeType"
	PropDescription = "Description"
	PropExtra       = "Extra"
)

// Store is a corpus kept in a Notion database.
type Store struct {
	token    string
	database string
	baseURL  string
	client   *http.Client
}

func init() {
	vault.Register("notion", newStore)
}

// newStore builds the Notion gateway from a configuration.
func newStore(cfg vault.Config) (vault.Vault, error) {
	token := cfg.NotionToken
	if token == "" {
		token = os.Getenv(DefaultTokenEnv)
	}
	if token == "" {
		return nil, fmt.Errorf("the notion gateway needs a token (set %s or notion.token_env)", DefaultTokenEnv)
	}
	if cfg.NotionDatabase == "" {
		return nil, errors.New("the notion gateway needs a database id")
	}
	return &Store{
		token:    token,
		database: cfg.NotionDatabase,
		baseURL:  DefaultAPIURL,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Kind is the registered name of the gateway.
func (s *Store) Kind() string { return "notion" }

// Root is where the store lives.
func (s *Store) Root() string { return "notion://database/" + s.database }

// Entries returns every entry in the database.
func (s *Store) Entries(ctx context.Context) ([]memory.Entry, error) {
	var pages []notionPage
	cursor := ""
	for {
		body := map[string]any{"page_size": 100}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var resp struct {
			Results    []notionPage `json:"results"`
			NextCursor string       `json:"next_cursor"`
			HasMore    bool         `json:"has_more"`
		}
		if err := s.post(ctx, "/databases/"+s.database+"/query", body, &resp); err != nil {
			return nil, err
		}
		pages = append(pages, resp.Results...)
		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	out := make([]memory.Entry, 0, len(pages))
	for _, p := range pages {
		e, err := s.entryOf(ctx, p)
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return typeOrder(out[i].Type) < typeOrder(out[j].Type)
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}

// Create stores a new entry and returns it as persisted.
func (s *Store) Create(ctx context.Context, e memory.Entry) (memory.Entry, error) {
	if !e.Type.Valid() {
		return e, fmt.Errorf("invalid entry type %q", e.Type)
	}
	body := map[string]any{
		"parent":     map[string]any{"database_id": s.database},
		"properties": s.properties(e),
		"children":   blocks(e.Body),
	}
	var page notionPage
	if err := s.post(ctx, "/pages", body, &page); err != nil {
		return e, err
	}
	e.Path = page.ID
	e.ModTime = page.LastEditedTime()
	return e, nil
}

// Update overwrites an existing entry.
func (s *Store) Update(ctx context.Context, e memory.Entry) error {
	if e.Path == "" {
		return errors.New("entry has no page id")
	}
	body := map[string]any{"properties": s.properties(e)}
	if err := s.patch(ctx, "/pages/"+e.Path, body, nil); err != nil {
		return err
	}
	if err := s.delete(ctx, "/blocks/"+e.Path+"/children", nil); err != nil {
		return err
	}
	if e.Body != "" {
		return s.patch(ctx, "/blocks/"+e.Path+"/children", map[string]any{"children": blocks(e.Body)}, nil)
	}
	return nil
}

// Search returns the pages that match the query.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]vault.Hit, error) {
	if limit <= 0 {
		limit = 30
	}
	body := map[string]any{
		"query":     query,
		"page_size": limit,
		"filter":    map[string]any{"property": "object", "value": "page"},
	}
	var resp struct {
		Results []struct {
			ID             string `json:"id"`
			LastEditedTime string `json:"last_edited_time"`
			Properties     map[string]struct {
				Title []struct {
					PlainText string `json:"plain_text"`
				} `json:"title"`
			} `json:"properties"`
			Highlight string `json:"highlight"`
		} `json:"results"`
	}
	if err := s.post(ctx, "/search", body, &resp); err != nil {
		return nil, err
	}
	var out []vault.Hit
	for _, r := range resp.Results {
		title := ""
		if prop, ok := r.Properties[PropTitle]; ok && len(prop.Title) > 0 {
			title = prop.Title[0].PlainText
		}
		out = append(out, vault.Hit{Path: r.ID, Text: title, Title: title})
	}
	return out, nil
}

// AppendLog appends one line to the write log.
func (s *Store) AppendLog(ctx context.Context, line string) error {
	body := map[string]any{
		"parent": map[string]any{"database_id": s.database},
		"properties": map[string]any{
			PropTitle: map[string]any{"title": []map[string]any{{"text": map[string]any{"content": line}}}},
			PropType:  map[string]any{"select": map[string]any{"name": "log"}},
		},
		"children": blocks(line),
	}
	return s.post(ctx, "/pages", body, nil)
}

// LogLines returns the lines of the write log, newest first.
func (s *Store) LogLines(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	body := map[string]any{
		"page_size": 100,
		"filter": map[string]any{
			"property": PropType,
			"select":   map[string]any{"equals": "log"},
		},
	}
	var resp struct {
		Results []notionPage `json:"results"`
	}
	if err := s.post(ctx, "/databases/"+s.database+"/query", body, &resp); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range resp.Results {
		text, err := s.children(ctx, p.ID)
		if err != nil || text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// entryOf converts a Notion page into an entry.
func (s *Store) entryOf(ctx context.Context, p notionPage) (memory.Entry, error) {
	e := memory.Entry{
		Title:       p.title(),
		Type:        memory.Type(p.selectValue(PropType)),
		Tier:        memory.Tier(p.selectValue(PropTier)),
		Date:        p.date(PropDate),
		Source:      p.richText(PropSource),
		Native:      p.selectValue(PropNative),
		NativeType:  p.selectValue(PropNativeType),
		Description: p.richText(PropDescription),
		Path:        p.ID,
		ModTime:     p.LastEditedTime(),
	}
	if raw := p.richText(PropExtra); raw != "" {
		extra := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &extra); err == nil {
			e.Extra = extra
		}
	}
	body, err := s.children(ctx, p.ID)
	if err == nil {
		e.Body = body
	}
	return e, nil
}

// properties renders the properties of an entry.
func (s *Store) properties(e memory.Entry) map[string]any {
	props := map[string]any{
		PropTitle: map[string]any{"title": []map[string]any{{"text": map[string]any{"content": e.Title}}}},
	}
	if e.Type != "" {
		props[PropType] = map[string]any{"select": map[string]any{"name": string(e.Type)}}
	}
	if e.Tier != "" {
		props[PropTier] = map[string]any{"select": map[string]any{"name": string(e.Tier)}}
	}
	if e.Date != "" {
		props[PropDate] = map[string]any{"date": map[string]any{"start": e.Date}}
	}
	if e.Source != "" {
		props[PropSource] = map[string]any{"rich_text": []map[string]any{{"text": map[string]any{"content": e.Source}}}}
	}
	if e.Native != "" {
		props[PropNative] = map[string]any{"select": map[string]any{"name": e.Native}}
	}
	if e.NativeType != "" {
		props[PropNativeType] = map[string]any{"select": map[string]any{"name": e.NativeType}}
	}
	if e.Description != "" {
		props[PropDescription] = map[string]any{"rich_text": []map[string]any{{"text": map[string]any{"content": e.Description}}}}
	}
	if len(e.Extra) > 0 {
		if data, err := json.Marshal(e.Extra); err == nil {
			props[PropExtra] = map[string]any{"rich_text": []map[string]any{{"text": map[string]any{"content": string(data)}}}}
		}
	}
	return props
}

// children reads the body of a page.
func (s *Store) children(ctx context.Context, pageID string) (string, error) {
	var resp struct {
		Results []struct {
			Type      string `json:"type"`
			Paragraph struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"paragraph"`
		} `json:"results"`
	}
	if err := s.get(ctx, "/blocks/"+pageID+"/children", &resp); err != nil {
		return "", err
	}
	var lines []string
	for _, b := range resp.Results {
		if b.Type != "paragraph" {
			continue
		}
		var text string
		for _, r := range b.Paragraph.RichText {
			text += r.PlainText
		}
		lines = append(lines, text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// blocks renders a body as Notion paragraph blocks.
func blocks(body string) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, map[string]any{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]any{
				"rich_text": []map[string]any{{"type": "text", "text": map[string]any{"content": line}}},
			},
		})
	}
	return out
}

// post sends a POST request.
func (s *Store) post(ctx context.Context, path string, body any, out any) error {
	return s.do(ctx, http.MethodPost, path, body, out)
}

// patch sends a PATCH request.
func (s *Store) patch(ctx context.Context, path string, body any, out any) error {
	return s.do(ctx, http.MethodPatch, path, body, out)
}

// get sends a GET request.
func (s *Store) get(ctx context.Context, path string, out any) error {
	return s.do(ctx, http.MethodGet, path, nil, out)
}

// delete sends a DELETE request.
func (s *Store) delete(ctx context.Context, path string, out any) error {
	return s.do(ctx, http.MethodDelete, path, nil, out)
}

// do sends a request to the Notion API.
func (s *Store) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Notion-Version", "2022-06-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notion %s %s: %s", method, path, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// notionPage is the part of a Notion page this gateway reads.
type notionPage struct {
	ID            string `json:"id"`
	LastEdited    string `json:"last_edited_time"`
	URL           string `json:"url"`
	PropertiesRaw map[string]struct {
		Type  string `json:"type"`
		Title []struct {
			PlainText string `json:"plain_text"`
		} `json:"title"`
		RichText []struct {
			PlainText string `json:"plain_text"`
		} `json:"rich_text"`
		Select struct {
			Name string `json:"name"`
		} `json:"select"`
		Date struct {
			Start string `json:"start"`
		} `json:"date"`
	} `json:"properties"`
}

// title returns the title of the page.
func (p notionPage) title() string {
	prop, ok := p.PropertiesRaw[PropTitle]
	if !ok || len(prop.Title) == 0 {
		return ""
	}
	return prop.Title[0].PlainText
}

// selectValue returns the value of a select property.
func (p notionPage) selectValue(name string) string {
	prop, ok := p.PropertiesRaw[name]
	if !ok {
		return ""
	}
	return prop.Select.Name
}

// richText returns the value of a rich text property.
func (p notionPage) richText(name string) string {
	prop, ok := p.PropertiesRaw[name]
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, r := range prop.RichText {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

// date returns the value of a date property.
func (p notionPage) date(name string) string {
	prop, ok := p.PropertiesRaw[name]
	if !ok {
		return ""
	}
	return prop.Date.Start
}

// LastEditedTime returns when the page was last changed.
func (p notionPage) LastEditedTime() time.Time {
	t, err := time.Parse(time.RFC3339, p.LastEdited)
	if err != nil {
		return time.Time{}
	}
	return t
}

// typeOrder gives every type a fixed position.
func typeOrder(t memory.Type) int {
	for i, known := range memory.Types {
		if t == known {
			return i
		}
	}
	return len(memory.Types)
}
