package qc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func completion(w http.ResponseWriter, content, finish string) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": finish}}})
}
func TestLLMLabelAndBlindJudge(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("bad request %s", r.URL.Path)
		}
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 2 {
			t.Fatal("expected system and user messages")
		}
		user := request.Messages[1].Content
		for _, bad := range []string{"gold", "annotations", "secret-human-reference", "fixture-labeler"} {
			if strings.Contains(user, bad) {
				t.Errorf("leaked %q", bad)
			}
		}
		calls++
		if calls == 1 {
			completion(w, `{"label":"off_platform","reason":"Просьба перейти"}`, "stop")
		} else {
			if !strings.Contains(user, "candidate_label") {
				t.Error("missing candidate")
			}
			completion(w, `{"verdict":"correct","reason":"Соответствует рубрике"}`, "stop")
		}
	}))
	defer server.Close()
	c, err := NewClient(server.URL+"/v1", "test-key", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	m := Message{"1", "Пиши в Telegram"}
	p, err := c.Label(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	j, err := c.Judge(context.Background(), m, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Label != OffPlatform || j.Verdict != "correct" || j.CandidateSHA256 != CandidateHash(m.Text, p.Label) {
		t.Fatalf("%+v %+v", p, j)
	}
}
func TestLLMRejectsMalformedOutputs(t *testing.T) {
	for _, body := range []string{`{"label":"alien","reason":"x"}`, "```json\n{}\n```", `{"label":"on_platform","reason":"x","extra":1}`, `{"label":"on_platform","reason":"x"} {}`} {
		t.Run(fmt.Sprintf("body-%s", body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completion(w, body, "stop") }))
			defer server.Close()
			c, _ := NewClient(server.URL, "", "test")
			if _, err := c.Label(context.Background(), Message{"1", "text"}); err == nil {
				t.Fatal("invalid output accepted")
			}
		})
	}
}
func TestLLMRejectsTruncationAndHTTPFailure(t *testing.T) {
	for _, status := range []int{200, 401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				completion(w, `{"label":"on_platform","reason":"x"}`, "length")
			}))
			defer server.Close()
			c, _ := NewClient(server.URL, "", "test")
			c.Retries = 0
			if _, err := c.Label(context.Background(), Message{"1", "x"}); err == nil {
				t.Fatal("error accepted")
			}
		})
	}
}
func TestClientConfig(t *testing.T) {
	for _, base := range []string{"file:///tmp/secret", "https://user:password@example.invalid/v1", "https://example.invalid/v1?api_key=x"} {
		if _, err := NewClient(base, "", "model"); err == nil {
			t.Fatal("invalid URL accepted")
		}
	}
	if _, err := NewClient("http://localhost:11434/v1", "", ""); err == nil {
		t.Fatal("empty model accepted")
	}
}
