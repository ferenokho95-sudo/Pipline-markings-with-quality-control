package qc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type LSResult struct {
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	Type     string `json:"type"`
	Value    struct {
		Choices []string `json:"choices"`
	} `json:"value"`
}
type LSAnnotation struct {
	ID          int             `json:"id"`
	CompletedBy json.RawMessage `json:"completed_by"`
	Cancelled   bool            `json:"was_cancelled"`
	GroundTruth bool            `json:"ground_truth"`
	Result      []LSResult      `json:"result"`
}
type LSTask struct {
	ID   json.RawMessage `json:"id,omitempty"`
	Data struct {
		Text       string `json:"text"`
		ExternalID string `json:"external_id"`
	} `json:"data"`
	Annotations []LSAnnotation `json:"annotations,omitempty"`
	Predictions []LSPrediction `json:"predictions,omitempty"`
}
type LSPrediction struct {
	ModelVersion string     `json:"model_version"`
	Result       []LSResult `json:"result"`
}

func ChoiceResult(label string) LSResult {
	r := LSResult{FromName: "label", ToName: "text", Type: "choices"}
	r.Value.Choices = []string{label}
	return r
}
func annotatorID(raw json.RawMessage) (string, error) {
	var n json.Number
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return "", fmt.Errorf("completed_by missing or invalid")
	}
	switch x := v.(type) {
	case json.Number:
		n = x
	case string:
		if x != "" {
			return x, nil
		}
	case map[string]any:
		if id, ok := x["id"].(json.Number); ok {
			n = id
		}
	}
	if n != "" {
		return n.String(), nil
	}
	return "", fmt.Errorf("completed_by requires a user ID")
}
func ImportLabelStudio(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tasks []LSTask
	if err = json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("use full JSON export, not JSON_MIN: %w", err)
	}
	rows := make([]Record, 0, len(tasks))
	for _, task := range tasks {
		id := task.Data.ExternalID
		if id == "" {
			var n json.Number
			if err = json.Unmarshal(task.ID, &n); err != nil {
				return nil, fmt.Errorf("task requires data.external_id or numeric id")
			}
			id = n.String()
		}
		r := Record{ID: id, Text: task.Data.Text, Annotations: map[string]string{}}
		for _, a := range task.Annotations {
			if a.Cancelled {
				continue
			}
			label := ""
			for _, result := range a.Result {
				if result.FromName != "label" || result.ToName != "text" || result.Type != "choices" {
					continue
				}
				if label != "" || len(result.Value.Choices) != 1 || !ValidLabel(result.Value.Choices[0]) {
					return nil, fmt.Errorf("task %s annotation %d: expected one valid label", id, a.ID)
				}
				label = result.Value.Choices[0]
			}
			if label == "" {
				continue
			}
			if a.GroundTruth {
				if r.Gold != "" {
					return nil, fmt.Errorf("task %s has multiple ground truth annotations; adjudicate first", id)
				}
				r.Gold = label
				continue
			}
			user, err := annotatorID(a.CompletedBy)
			if err != nil {
				return nil, fmt.Errorf("task %s: %w", id, err)
			}
			if _, ok := r.Annotations[user]; ok {
				return nil, fmt.Errorf("task %s: duplicate annotation by %s; resolve revisions first", id, user)
			}
			r.Annotations[user] = label
		}
		rows = append(rows, r)
	}
	return rows, ValidateRecords(rows)
}
func ExportLabelStudio(messages []Message, predictions []Prediction) ([]LSTask, error) {
	if err := ValidateMessages(messages); err != nil {
		return nil, err
	}
	lookup, err := IndexPredictions(messages, predictions)
	if err != nil {
		return nil, err
	}
	tasks := make([]LSTask, 0, len(messages))
	for i, m := range messages {
		var t LSTask
		t.ID = json.RawMessage(strconv.Itoa(i + 1))
		t.Data.Text = m.Text
		t.Data.ExternalID = m.ID
		if p, ok := lookup[m.ID]; ok {
			t.Predictions = []LSPrediction{{ModelVersion: p.Model, Result: []LSResult{ChoiceResult(p.Label)}}}
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
func IndexPredictions(messages []Message, rows []Prediction) (map[string]Prediction, error) {
	known := map[string]string{}
	for _, m := range messages {
		known[m.ID] = m.Text
	}
	out := map[string]Prediction{}
	for _, p := range rows {
		text, ok := known[p.ID]
		if !ok {
			return nil, fmt.Errorf("prediction for unknown id %s", p.ID)
		}
		if _, ok = out[p.ID]; ok {
			return nil, fmt.Errorf("duplicate prediction %s", p.ID)
		}
		if !ValidLabel(p.Label) || p.TextSHA256 != Hash(text) {
			return nil, fmt.Errorf("invalid label or stale text hash for %s", p.ID)
		}
		out[p.ID] = p
	}
	return out, nil
}
