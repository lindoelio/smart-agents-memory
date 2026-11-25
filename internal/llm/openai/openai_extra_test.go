package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/llm"
)

// roundTripFunc lets a test answer a request without a server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// jsonResponse builds a minimal HTTP response with a body.
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestKind(t *testing.T) {
	if got := New("", "", "").Kind(); got != "openai" {
		t.Fatalf("the kind is %q", got)
	}
}

func TestNewDefaults(t *testing.T) {
	client := New("", "", "")
	if client.KeyEnv != DefaultKeyEnv {
		t.Fatalf("the key env is %q", client.KeyEnv)
	}
	if client.Model != "gpt-4o-mini" {
		t.Fatalf("the model is %q", client.Model)
	}
	if client.HTTP == nil || client.HTTP.Timeout == 0 {
		t.Fatal("the client has no timeout")
	}
	named := New("http://example.test/v1", "a-model", "A_KEY_ENV")
	if named.KeyEnv != "A_KEY_ENV" || named.Model != "a-model" {
		t.Fatalf("the explicit settings are %+v", named)
	}
}

func TestTheBaseURLDefaultsToTheLocalGatewayWithoutAKey(t *testing.T) {
	for name, tc := range map[string]struct {
		key  string
		want string
	}{
		"no key":   {"", "http://127.0.0.1:8080/v1/chat/completions"},
		"with key": {"secret", "https://api.openai.com/v1/chat/completions"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_BASE_URL_KEY", tc.key)
			client := New("", "a-model", "TEST_BASE_URL_KEY")
			var gotURL, gotAuth string
			client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotURL = r.URL.String()
				gotAuth = r.Header.Get("Authorization")
				return jsonResponse(http.StatusOK,
					`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`), nil
			})}
			var out map[string]bool
			if err := client.CompleteJSON(context.Background(), llm.Request{}, &out); err != nil {
				t.Fatal(err)
			}
			if gotURL != tc.want {
				t.Fatalf("the request URL is %q, want %q", gotURL, tc.want)
			}
			if tc.key == "" && gotAuth != "" {
				t.Fatalf("a request without a key carries %q", gotAuth)
			}
			if tc.key != "" && gotAuth != "Bearer "+tc.key {
				t.Fatalf("the authorization is %q", gotAuth)
			}
		})
	}
}

func TestTheBaseURLHasNoTrailingSlash(t *testing.T) {
	var gotURL string
	client := New("", "a-model", "TEST_TRAILING_KEY")
	t.Setenv("TEST_TRAILING_KEY", "")
	client.BaseURL = "http://example.test/v1/"
	client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResponse(http.StatusOK, `{"choices":[{"message":{"content":"{}"}}]}`), nil
	})}
	if err := client.CompleteJSON(context.Background(), llm.Request{}, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if gotURL != "http://example.test/v1/chat/completions" {
		t.Fatalf("the URL is %q", gotURL)
	}
}

func TestANilHTTPClientIsReplaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)
	}))
	defer server.Close()
	client := New(server.URL, "a-model", "TEST_NIL_KEY")
	client.HTTP = nil
	var out map[string]bool
	if err := client.CompleteJSON(context.Background(), llm.Request{}, &out); err != nil {
		t.Fatal(err)
	}
	if !out["ok"] {
		t.Fatalf("the output is %v", out)
	}
}

func TestABadRequestURLIsAnError(t *testing.T) {
	client := New("://bad url", "a-model", "TEST_BAD_URL_KEY")
	if err := client.CompleteJSON(context.Background(), llm.Request{}, &map[string]any{}); err == nil {
		t.Fatal("a bad base URL was accepted")
	}
}

func TestCompleteJSONReportsAMalformedResponse(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       "this is not JSON",
		"no choices":     `{"choices":[]}`,
		"content is not": `{"choices":[{"message":{"content":"not json at all"}}]}`,
		"empty content":  `{"choices":[{"message":{"content":""}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			client := New(server.URL, "a-model", "TEST_MALFORMED_KEY")
			if err := client.CompleteJSON(context.Background(), llm.Request{}, &map[string]any{}); err == nil {
				t.Fatal("a malformed response was accepted")
			}
		})
	}
}

func TestCompleteJSONReportsATimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer server.Close()
	client := New(server.URL, "a-model", "TEST_TIMEOUT_KEY")
	client.HTTP = &http.Client{Timeout: 20 * time.Millisecond}
	if err := client.CompleteJSON(context.Background(), llm.Request{}, &map[string]any{}); err == nil {
		t.Fatal("a timed-out request succeeded")
	}
}

func TestExtractJSONFencesAndSurroundingText(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"```{\"a\":1}```":         `{"a":1}`,
		"  [1,2]  ":               `[1,2]`,
		"prefix {\"a\":1} suffix": `{"a":1}`,
		"nothing to extract here": "nothing to extract here",
		"```\n```":                "",
		"```json\nno object\n```": "no object",
		"{\"a\":1} and {\"b\":2}": `{"a":1} and {"b":2}`,
		"trailing } in the text":  "trailing } in the text",
	}
	for input, want := range cases {
		if got := extractJSON(input); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", input, got, want)
		}
	}
}
