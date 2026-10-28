package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"example.com/labelqc/internal/qc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func usage() {
	fmt.Println(`labelqc — контроль качества разметки (Go, стандартная библиотека)
Commands:
  demo     --out demo                        offline synthetic demonstration
  prepare  --in messages.jsonl --out tasks.json [--predictions predictions.jsonl]
  import   --in labelstudio-export.json --out annotations.jsonl
  label    --in messages.jsonl --out predictions.jsonl [--existing prior.partial.jsonl]
  judge    --in messages.jsonl --predictions predictions.jsonl --out judgements.jsonl [--existing prior.partial.jsonl]
  metrics  --in annotations.jsonl --predictions predictions.jsonl --judgements judgements.jsonl --a 1 --b 2 --out reports/run1

Run from the project root. No command overwrites an existing output.
LLM configuration: LABELER_BASE_URL, LABELER_MODEL, LABELER_API_KEY;
JUDGE_BASE_URL, JUDGE_MODEL, JUDGE_API_KEY (fallback: LLM_BASE_URL/MODEL/API_KEY).
Base URL includes /v1 for an OpenAI-compatible server. demo requires no server.`)
}
func require(values ...string) error {
	for _, s := range values {
		if s == "" {
			return errors.New("required flag is missing; run with -h")
		}
	}
	return nil
}
func freeOutput(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("output exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		usage()
		return nil
	}
	cmd := args[0]
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	in := f.String("in", "", "input JSONL (full JSON for import)")
	out := f.String("out", "", "output path/directory")
	predPath := f.String("predictions", "", "prediction JSONL")
	judgePath := f.String("judgements", "", "judge JSONL")
	a := f.String("a", "", "first human annotator ID")
	b := f.String("b", "", "second human annotator ID")
	existing := f.String("existing", "", "reuse validated partial results from a previous run")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if cmd == "demo" {
		if err := require(*out); err != nil {
			return err
		}
		return qc.Demo(*out)
	}
	if err := require(*in, *out); err != nil {
		return err
	}
	switch cmd {
	case "import":
		rows, err := qc.ImportLabelStudio(*in)
		if err != nil {
			return err
		}
		return qc.WriteJSONL(*out, rows)
	case "prepare", "label", "judge":
		messages, err := qc.ReadJSONL[qc.Message](*in)
		if err != nil {
			return err
		}
		if err = qc.ValidateMessages(messages); err != nil {
			return err
		}
		if err = freeOutput(*out); err != nil {
			return err
		}
		var preds []qc.Prediction
		if *predPath != "" {
			preds, err = qc.ReadJSONL[qc.Prediction](*predPath)
			if err != nil {
				return err
			}
		}
		if cmd == "prepare" {
			tasks, err := qc.ExportLabelStudio(messages, preds)
			if err != nil {
				return err
			}
			return qc.WriteJSON(*out, tasks)
		}
		if err = freeOutput(*out + ".partial.jsonl"); err != nil {
			return err
		}
		role := "LABELER"
		if cmd == "judge" {
			role = "JUDGE"
			if err = require(*predPath); err != nil {
				return err
			}
		}
		client, err := configuredClient(role)
		if err != nil {
			return err
		}
		if cmd == "label" {
			return label(ctx, client, messages, *existing, *out)
		}
		return judge(ctx, client, messages, preds, *existing, *out)
	case "metrics":
		if err := require(*predPath, *judgePath, *a, *b); err != nil {
			return err
		}
		records, err := qc.ReadJSONL[qc.Record](*in)
		if err != nil {
			return err
		}
		preds, err := qc.ReadJSONL[qc.Prediction](*predPath)
		if err != nil {
			return err
		}
		judges, err := qc.ReadJSONL[qc.Judgement](*judgePath)
		if err != nil {
			return err
		}
		report, queue, err := qc.BuildReport(records, preds, judges, *a, *b)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(records)
		report.DatasetSHA256 = qc.Hash(string(data))
		for _, name := range []string{"report.json", "review-queue.json", "REPORT.md"} {
			if err = freeOutput(filepath.Join(*out, name)); err != nil {
				return err
			}
		}
		if err = qc.WriteJSON(filepath.Join(*out, "report.json"), report); err != nil {
			return err
		}
		if err = qc.WriteJSON(filepath.Join(*out, "review-queue.json"), queue); err != nil {
			return err
		}
		if err = qc.WriteText(filepath.Join(*out, "REPORT.md"), qc.Markdown(report)); err != nil {
			return err
		}
		fmt.Print(qc.Markdown(report))
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
func configuredClient(role string) (*qc.Client, error) {
	env := func(name string) string {
		if v := os.Getenv(role + "_" + name); v != "" {
			return v
		}
		return os.Getenv("LLM_" + name)
	}
	base := env("BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:11434/v1"
	}
	return qc.NewClient(base, env("API_KEY"), env("MODEL"))
}
func label(ctx context.Context, c *qc.Client, messages []qc.Message, prior, out string) error {
	rows := []qc.Prediction{}
	if prior != "" {
		var err error
		rows, err = qc.ReadJSONL[qc.Prediction](prior)
		if err != nil {
			return err
		}
	}
	lookup, err := qc.IndexPredictions(messages, rows)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.Model != c.Model || p.PromptSHA256 != qc.Hash(qc.LabelPrompt) || p.Source != "llm_api" {
			return fmt.Errorf("existing label result has different model/prompt/source: %s", p.ID)
		}
	}
	for _, m := range messages {
		if _, ok := lookup[m.ID]; ok {
			continue
		}
		p, err := c.Label(ctx, m)
		if err != nil {
			if len(rows) > 0 {
				if writeErr := qc.WriteJSONL(out+".partial.jsonl", rows); writeErr != nil {
					return fmt.Errorf("%w; cannot save completed results: %v", err, writeErr)
				}
			}
			return fmt.Errorf("task %s: %w; completed rows, if any, saved to %s.partial.jsonl", m.ID, err, out)
		}
		rows = append(rows, p)
	}
	return qc.WriteJSONL(out, rows)
}
func judge(ctx context.Context, c *qc.Client, messages []qc.Message, preds []qc.Prediction, prior, out string) error {
	pmap, err := qc.IndexPredictions(messages, preds)
	if err != nil {
		return err
	}
	if len(pmap) != len(messages) {
		return errors.New("judge requires a candidate for every input message")
	}
	rows := []qc.Judgement{}
	if prior != "" {
		rows, err = qc.ReadJSONL[qc.Judgement](prior)
		if err != nil {
			return err
		}
	}
	lookup := map[string]qc.Judgement{}
	texts := map[string]string{}
	for _, m := range messages {
		texts[m.ID] = m.Text
	}
	for _, j := range rows {
		text, ok := texts[j.ID]
		p := pmap[j.ID]
		_, duplicate := lookup[j.ID]
		if !ok || duplicate || j.Model != c.Model || j.PromptSHA256 != qc.Hash(qc.JudgePrompt) || j.Source != "llm_api" || !qc.ValidVerdict(j.Verdict) || j.CandidateLabel != p.Label || j.CandidateSHA256 != qc.CandidateHash(text, p.Label) || (p.Label == qc.Uncertain && j.Verdict != qc.Uncertain) {
			return fmt.Errorf("invalid or stale existing judge result: %s", j.ID)
		}
		lookup[j.ID] = j
	}
	for _, m := range messages {
		if _, ok := lookup[m.ID]; ok {
			continue
		}
		j, err := c.Judge(ctx, m, pmap[m.ID])
		if err != nil {
			if len(rows) > 0 {
				if writeErr := qc.WriteJSONL(out+".partial.jsonl", rows); writeErr != nil {
					return fmt.Errorf("%w; cannot save completed results: %v", err, writeErr)
				}
			}
			return fmt.Errorf("task %s: %w; completed rows, if any, saved to %s.partial.jsonl", m.ID, err, out)
		}
		rows = append(rows, j)
	}
	return qc.WriteJSONL(out, rows)
}
