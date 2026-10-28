package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"example.com/labelqc/internal/qc"
)

func TestCLIEndToEndOffline(t *testing.T) {
	root := t.TempDir()
	demo := filepath.Join(root, "demo")
	if err := run(context.Background(), []string{"demo", "--out", demo}); err != nil {
		t.Fatal(err)
	}
	annotations := filepath.Join(root, "imported.jsonl")
	if err := run(context.Background(), []string{"import", "--in", filepath.Join(demo, "labelstudio-export.json"), "--out", annotations}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"metrics", "--in", annotations, "--predictions", filepath.Join(demo, "predictions.jsonl"), "--judgements", filepath.Join(demo, "judgements.jsonl"), "--a", "1", "--b", "2", "--out", filepath.Join(root, "report")}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"prepare", "--in", filepath.Join(demo, "messages.jsonl"), "--predictions", filepath.Join(demo, "predictions.jsonl"), "--out", filepath.Join(root, "prelabelled.json")}); err != nil {
		t.Fatal(err)
	}
}
func TestLabelResumePartial(t *testing.T) {
	root := t.TempDir()
	messages := filepath.Join(root, "messages.jsonl")
	if err := qc.WriteJSONL(messages, []qc.Message{{ID: "1", Text: "text one"}, {ID: "2", Text: "text two"}}); err != nil {
		t.Fatal(err)
	}
	count := 0
	failSecond := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if failSecond && count == 2 {
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"label":"on_platform","reason":"Нет перехода"}`}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	t.Setenv("LABELER_BASE_URL", server.URL+"/v1")
	t.Setenv("LABELER_MODEL", "test-model")
	t.Setenv("LABELER_API_KEY", "")
	output := filepath.Join(root, "first.jsonl")
	err := run(context.Background(), []string{"label", "--in", messages, "--out", output})
	if err == nil {
		t.Fatal("expected failure")
	}
	partial, err := qc.ReadJSONL[qc.Prediction](output + ".partial.jsonl")
	if err != nil || len(partial) != 1 {
		t.Fatalf("%+v %v", partial, err)
	}
	failSecond = false
	resumed := filepath.Join(root, "resumed.jsonl")
	if err = run(context.Background(), []string{"label", "--in", messages, "--out", resumed, "--existing", output + ".partial.jsonl"}); err != nil {
		t.Fatal(err)
	}
	rows, err := qc.ReadJSONL[qc.Prediction](resumed)
	if err != nil || len(rows) != 2 || count != 3 {
		t.Fatalf("%+v %d %v", rows, count, err)
	}
}
