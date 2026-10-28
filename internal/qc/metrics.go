package qc

import (
	"fmt"
	"math"
	"sort"
)

type Agreement struct {
	N         int      `json:"n"`
	Matches   int      `json:"matches"`
	Agreement *float64 `json:"agreement"`
	Kappa     *float64 `json:"cohen_kappa"`
	Status    string   `json:"status"`
	Labels    []string `json:"labels"`
	Confusion [][]int  `json:"confusion"`
}

// Cohen calculates unweighted nominal kappa. Undefined kappa is null, never NaN.
func Cohen(a, b []string) (Agreement, error) {
	if len(a) != len(b) {
		return Agreement{}, fmt.Errorf("unequal annotation lengths")
	}
	r := Agreement{N: len(a), Status: "no_pairs"}
	if len(a) == 0 {
		return r, nil
	}
	labels := map[string]bool{}
	for i := range a {
		labels[a[i]] = true
		labels[b[i]] = true
	}
	for label := range labels {
		r.Labels = append(r.Labels, label)
	}
	sort.Strings(r.Labels)
	positions := map[string]int{}
	for i, label := range r.Labels {
		positions[label] = i
	}
	r.Confusion = make([][]int, len(r.Labels))
	for i := range r.Confusion {
		r.Confusion[i] = make([]int, len(r.Labels))
	}
	countsA, countsB := map[string]int{}, map[string]int{}
	for i := range a {
		countsA[a[i]]++
		countsB[b[i]]++
		r.Confusion[positions[a[i]]][positions[b[i]]]++
		if a[i] == b[i] {
			r.Matches++
		}
	}
	po := float64(r.Matches) / float64(r.N)
	r.Agreement = &po
	pe := 0.
	for _, label := range r.Labels {
		pe += float64(countsA[label]) * float64(countsB[label]) / (float64(r.N) * float64(r.N))
	}
	if math.Abs(1-pe) < 1e-12 {
		r.Status = "undefined_constant_labels"
		return r, nil
	}
	k := (po - pe) / (1 - pe)
	r.Kappa = &k
	r.Status = "ok"
	return r, nil
}

type Report struct {
	Total                      int            `json:"total_records"`
	AnnotatorA                 string         `json:"annotator_a"`
	AnnotatorB                 string         `json:"annotator_b"`
	HumanPair                  Agreement      `json:"human_pair"`
	HumanPairCoverage          float64        `json:"human_pair_coverage"`
	PredictionVsGold           Agreement      `json:"prediction_vs_expert_gold"`
	PredictionCoverage         float64        `json:"prediction_gold_coverage"`
	JudgeVsManual              Agreement      `json:"judge_vs_manual_correctness"`
	JudgeEligible              int            `json:"judge_eligible_with_gold"`
	JudgeCovered               int            `json:"judge_covered_non_uncertain"`
	JudgeCoverage              float64        `json:"judge_coverage"`
	JudgeConservativeAgreement *float64       `json:"judge_matches_over_all_eligible"`
	Abstentions                int            `json:"judge_abstentions"`
	PredictionSources          map[string]int `json:"prediction_sources"`
	JudgeSources               map[string]int `json:"judge_sources"`
	DatasetSHA256              string         `json:"dataset_sha256"`
	Notes                      []string       `json:"notes"`
}
type ReviewItem struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Reasons []string `json:"reasons"`
}

func BuildReport(records []Record, preds []Prediction, judges []Judgement, a, b string) (Report, []ReviewItem, error) {
	if a == "" || b == "" || a == b {
		return Report{}, nil, fmt.Errorf("choose two distinct annotator IDs")
	}
	if err := ValidateRecords(records); err != nil {
		return Report{}, nil, err
	}
	seenAnnotators := map[string]bool{}
	for _, row := range records {
		for id := range row.Annotations {
			seenAnnotators[id] = true
		}
	}
	if !seenAnnotators[a] || !seenAnnotators[b] {
		return Report{}, nil, fmt.Errorf("annotator IDs must occur in the input annotations")
	}
	messages := make([]Message, 0, len(records))
	for _, r := range records {
		messages = append(messages, Message{r.ID, r.Text})
	}
	pmap, err := IndexPredictions(messages, preds)
	if err != nil {
		return Report{}, nil, err
	}
	rmap := map[string]Record{}
	for _, r := range records {
		rmap[r.ID] = r
	}
	jmap := map[string]Judgement{}
	for _, j := range judges {
		r, ok := rmap[j.ID]
		if !ok {
			return Report{}, nil, fmt.Errorf("judge unknown id %s", j.ID)
		}
		p, ok := pmap[j.ID]
		if !ok {
			return Report{}, nil, fmt.Errorf("judge has no candidate for %s", j.ID)
		}
		if _, ok = jmap[j.ID]; ok {
			return Report{}, nil, fmt.Errorf("duplicate judge id %s", j.ID)
		}
		if !ValidVerdict(j.Verdict) || j.CandidateLabel != p.Label || j.CandidateSHA256 != CandidateHash(r.Text, p.Label) {
			return Report{}, nil, fmt.Errorf("stale or invalid judge for %s", j.ID)
		}
		if p.Label == Uncertain && j.Verdict != Uncertain {
			return Report{}, nil, fmt.Errorf("judge must abstain on uncertain candidate %s", j.ID)
		}
		jmap[j.ID] = j
	}
	report := Report{Total: len(records), AnnotatorA: a, AnnotatorB: b, PredictionSources: map[string]int{}, JudgeSources: map[string]int{}, Notes: []string{
		"Cohen kappa uses only items with two definite labels; missing/uncertain excluded and coverage reported.",
		"Gold is an independently adjudicated reference, not majority vote or a judge output.",
		"Judge correctness is compared with candidate_label == expert_gold; gold is never sent to the judge.",
		"All matrices: rows are first argument/reference; columns are second argument/prediction; label order is explicit.",
		"Fixture sources are synthetic test data and are not evidence of actual human or LLM quality.",
	}}
	for _, p := range preds {
		report.PredictionSources[p.Source]++
	}
	for _, j := range judges {
		report.JudgeSources[j.Source]++
	}
	var ha, hb, pa, pb, ja, jb []string
	queue := []ReviewItem{}
	for _, r := range records {
		reasons := []string{}
		x, xok := r.Annotations[a]
		y, yok := r.Annotations[b]
		if xok && yok && x != Uncertain && y != Uncertain {
			ha = append(ha, x)
			hb = append(hb, y)
			if x != y {
				reasons = append(reasons, "human_disagreement")
			}
		}
		if !xok || !yok {
			reasons = append(reasons, "missing_human_annotation")
		}
		if x == Uncertain || y == Uncertain {
			reasons = append(reasons, "human_uncertain")
		}
		p, hasP := pmap[r.ID]
		if !hasP {
			reasons = append(reasons, "missing_prediction")
		}
		goldOK := r.Gold != "" && r.Gold != Uncertain
		if !goldOK {
			reasons = append(reasons, "missing_or_uncertain_gold")
		}
		if hasP && p.Label == Uncertain {
			reasons = append(reasons, "llm_uncertain")
		}
		if hasP && goldOK && p.Label != Uncertain {
			pa = append(pa, r.Gold)
			pb = append(pb, p.Label)
			if p.Label != r.Gold {
				reasons = append(reasons, "prediction_differs_from_gold")
			}
		}
		if hasP && goldOK {
			report.JudgeEligible++
			j, ok := jmap[r.ID]
			if !ok {
				reasons = append(reasons, "missing_judge")
			} else if j.Verdict == Uncertain {
				report.Abstentions++
				reasons = append(reasons, "judge_uncertain")
			} else {
				truth := "incorrect"
				if p.Label == r.Gold {
					truth = "correct"
				}
				ja = append(ja, truth)
				jb = append(jb, j.Verdict)
				report.JudgeCovered++
				if truth != j.Verdict {
					reasons = append(reasons, "judge_differs_from_manual")
				}
			}
		}
		if len(reasons) > 0 {
			queue = append(queue, ReviewItem{r.ID, r.Text, reasons})
		}
	}
	report.HumanPair, _ = Cohen(ha, hb)
	report.PredictionVsGold, _ = Cohen(pa, pb)
	report.JudgeVsManual, _ = Cohen(ja, jb)
	report.HumanPairCoverage = float64(len(ha)) / float64(len(records))
	report.PredictionCoverage = float64(len(pa)) / float64(len(records))
	if report.JudgeEligible > 0 {
		report.JudgeCoverage = float64(report.JudgeCovered) / float64(report.JudgeEligible)
		v := float64(report.JudgeVsManual.Matches) / float64(report.JudgeEligible)
		report.JudgeConservativeAgreement = &v
	}
	return report, queue, nil
}
func DisplayNumber(v *float64) string {
	if v == nil {
		return "не определено"
	}
	return fmt.Sprintf("%.4f", *v)
}
func Markdown(r Report) string {
	return fmt.Sprintf("# Контроль качества разметки\n\nИсточник меток проверяйте в report.json. Демо содержит синтетические фикстуры, не реальные оценки LLM.\n\n| Проверка | Пар | Совпадение | Cohen κ |\n|---|---:|---:|---:|\n| Разметчик %s / %s | %d | %s | %s |\n| LLM / экспертный эталон | %d | %s | %s |\n| Judge / экспертная оценка корректности | %d | %s | %s |\n\nДоли совпадений показаны от 0 до 1. Покрытие пары разметчиков: %.1f%%. Покрытие LLM/эталона по всем заданиям: %.1f%%. Покрытие Judge среди заданий с кандидатом и эталоном: %.1f%% (%d/%d). Воздержаний Judge: %d. Доля совпадений Judge по ВСЕМ подходящим заданиям, включая пропуски и воздержания в знаменателе: %s.\n\nПропуски и uncertain исключаются из условных метрик. Не интерпретируйте высокий результат без покрытия и матриц ошибок. κ при постоянных одинаковых метках не определён. Эталон должен быть задан независимым экспертом; приоритет отдаётся ручной проверке спорных случаев.\n", r.AnnotatorA, r.AnnotatorB, r.HumanPair.N, DisplayNumber(r.HumanPair.Agreement), DisplayNumber(r.HumanPair.Kappa), r.PredictionVsGold.N, DisplayNumber(r.PredictionVsGold.Agreement), DisplayNumber(r.PredictionVsGold.Kappa), r.JudgeVsManual.N, DisplayNumber(r.JudgeVsManual.Agreement), DisplayNumber(r.JudgeVsManual.Kappa), r.HumanPairCoverage*100, r.PredictionCoverage*100, r.JudgeCoverage*100, r.JudgeCovered, r.JudgeEligible, r.Abstentions, DisplayNumber(r.JudgeConservativeAgreement))
}
