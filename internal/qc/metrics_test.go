package qc

import (
	"math"
	"testing"
)

func TestCohenKnownValues(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"perfect", []string{"a", "a", "b", "b"}, []string{"a", "a", "b", "b"}, 1},
		{"inverse", []string{"a", "a", "b", "b"}, []string{"b", "b", "a", "a"}, -1},
		{"partial", []string{"a", "a", "b", "b"}, []string{"a", "a", "a", "b"}, .5},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Cohen(tt.a, tt.b)
			if err != nil || r.Kappa == nil || math.Abs(*r.Kappa-tt.want) > 1e-10 {
				t.Fatalf("got %+v, %v", r, err)
			}
		})
	}
}
func TestCohenUndefined(t *testing.T) {
	r, err := Cohen([]string{"a", "a"}, []string{"a", "a"})
	if err != nil || r.Kappa != nil || r.Status != "undefined_constant_labels" || *r.Agreement != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Cohen(nil, nil)
	if err != nil || r.Kappa != nil || r.Agreement != nil || r.Status != "no_pairs" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = Cohen([]string{"a"}, nil); err == nil {
		t.Fatal("unequal lengths accepted")
	}
}
func TestReportCoverageAndNoGoldLeak(t *testing.T) {
	rows := []Record{
		{"1", "Напиши в Telegram", map[string]string{"a": OffPlatform, "b": OffPlatform}, OffPlatform},
		{"2", "Не пиши в Telegram", map[string]string{"a": OnPlatform, "b": Uncertain}, OnPlatform},
		{"3", "Заказ 123", map[string]string{"a": OnPlatform}, ""},
	}
	preds := []Prediction{}
	judges := []Judgement{}
	for i, r := range rows {
		preds = append(preds, Prediction{ID: r.ID, Label: OffPlatform, TextSHA256: Hash(r.Text), Source: "synthetic_fixture"})
		v := "correct"
		if i == 1 {
			v = Uncertain
		}
		judges = append(judges, Judgement{ID: r.ID, CandidateLabel: OffPlatform, CandidateSHA256: CandidateHash(r.Text, OffPlatform), Verdict: v, Source: "synthetic_fixture"})
	}
	report, queue, err := BuildReport(rows, preds, judges, "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if report.HumanPair.N != 1 || report.JudgeEligible != 2 || report.JudgeCovered != 1 || report.Abstentions != 1 || report.JudgeCoverage != .5 || *report.JudgeConservativeAgreement != .5 {
		t.Fatalf("%+v", report)
	}
	if len(queue) != 2 {
		t.Fatalf("queue=%+v", queue)
	}
	judges[0].CandidateSHA256 = "stale"
	if _, _, err = BuildReport(rows, preds, judges, "a", "b"); err == nil {
		t.Fatal("stale judge accepted")
	}
}
func TestPredictionValidation(t *testing.T) {
	m := []Message{{"a", "text"}}
	valid := Prediction{ID: "a", Label: OnPlatform, TextSHA256: Hash("text")}
	if _, err := IndexPredictions(m, []Prediction{valid, valid}); err == nil {
		t.Fatal("duplicate accepted")
	}
	valid.TextSHA256 = Hash("changed")
	if _, err := IndexPredictions(m, []Prediction{valid}); err == nil {
		t.Fatal("stale hash accepted")
	}
	valid.ID = "unknown"
	if _, err := IndexPredictions(m, []Prediction{valid}); err == nil {
		t.Fatal("unknown ID accepted")
	}
}

func TestUnknownAnnotatorRejected(t *testing.T) {
	rows := []Record{{ID: "1", Text: "text", Annotations: map[string]string{"a": OnPlatform, "b": OnPlatform}}}
	if _, _, err := BuildReport(rows, nil, nil, "typo", "b"); err == nil {
		t.Fatal("unknown annotator accepted")
	}
	r, queue, err := BuildReport(rows, nil, nil, "a", "b")
	if err != nil || r.PredictionVsGold.N != 0 || len(queue) != 1 {
		t.Fatalf("%+v %+v %v", r, queue, err)
	}
}
