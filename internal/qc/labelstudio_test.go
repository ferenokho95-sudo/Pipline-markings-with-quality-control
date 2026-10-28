package qc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDemoAndImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := Demo(dir); err != nil {
		t.Fatal(err)
	}
	rows, err := ImportLabelStudio(filepath.Join(dir, "labelstudio-export.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 12 || rows[0].Annotations["1"] != OnPlatform || rows[0].Annotations["2"] != OnPlatform || rows[0].Gold != OnPlatform {
		t.Fatalf("%+v", rows)
	}
	preds, err := ReadJSONL[Prediction](filepath.Join(dir, "predictions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	judges, err := ReadJSONL[Judgement](filepath.Join(dir, "judgements.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := BuildReport(rows, preds, judges, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if r.HumanPair.Matches != 10 || r.JudgeVsManual.N != 11 || r.JudgeVsManual.Matches != 9 {
		t.Fatalf("%+v", r)
	}
}
func TestImportCancelledAndDuplicates(t *testing.T) {
	task := LSTask{}
	task.ID = json.RawMessage("1")
	task.Data.Text = "hello"
	task.Data.ExternalID = "external"
	annotation := LSAnnotation{ID: 1, CompletedBy: json.RawMessage("7"), Result: []LSResult{ChoiceResult(OnPlatform)}}
	cancelled := annotation
	cancelled.Cancelled = true
	task.Annotations = []LSAnnotation{annotation, cancelled}
	path := filepath.Join(t.TempDir(), "ls.json")
	b, _ := json.Marshal([]LSTask{task})
	os.WriteFile(path, b, 0600)
	rows, err := ImportLabelStudio(path)
	if err != nil || len(rows) != 1 || rows[0].ID != "external" || len(rows[0].Annotations) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	task.Annotations = append(task.Annotations, annotation)
	b, _ = json.Marshal([]LSTask{task})
	os.WriteFile(path, b, 0600)
	if _, err = ImportLabelStudio(path); err == nil {
		t.Fatal("duplicate annotator accepted")
	}
}
func TestExportPredictionsKeepExternalID(t *testing.T) {
	messages := []Message{{"external-1", "Не пиши в тг"}}
	preds := []Prediction{{ID: "external-1", Label: OnPlatform, TextSHA256: Hash(messages[0].Text), Model: "test"}}
	tasks, err := ExportLabelStudio(messages, preds)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Data.ExternalID != "external-1" || len(tasks[0].Predictions) != 1 || tasks[0].Predictions[0].Result[0].FromName != "label" {
		t.Fatalf("%+v", tasks)
	}
}
func TestIORejectsUnknownFieldsAndOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	if err := WriteText(p, "{\"id\":\"a\",\"text\":\"text\",\"lable\":1}\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSONL[Message](p); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := WriteText(p, "replace"); err == nil {
		t.Fatal("existing file overwritten")
	}
}
