package qc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Demo uses handcrafted synthetic fixtures. It makes zero LLM requests.
func Demo(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("demo directory already exists; choose a new path")
	}
	texts := []string{
		"Обсудим доставку здесь, в чате площадки", "Напиши мне в Telegram: @example_user",
		"Номер заказа 89261234567, проверь статус", "Пиши на user [собака] example [точка] invalid",
		"Не переходи в WhatsApp, остаёмся здесь", "Пришли телефон, свяжемся напрямую",
		"Как настроить уведомления в Telegram?", "Давай продолжим по почте user@example.invalid",
		"В инструкции указан сайт example.invalid", "Позвони на +0 000 000 00 00, обсудим заказ",
		"Мне удобно переписываться только на площадке", "Добавляйся в Signal и отправь детали там",
	}
	messages := []Message{}
	records := []Record{}
	predictions := []Prediction{}
	judges := []Judgement{}
	lsTasks := []LSTask{}
	for i, text := range texts {
		id := fmt.Sprintf("demo-%02d", i+1)
		label := OnPlatform
		if i%2 == 1 {
			label = OffPlatform
		}
		other := label
		if i == 2 || i == 9 {
			other = flip(label)
		}
		records = append(records, Record{id, text, map[string]string{"1": label, "2": other}, label})
		messages = append(messages, Message{id, text})
		candidate := label
		if i == 3 || i == 10 {
			candidate = flip(label)
		}
		predictions = append(predictions, Prediction{id, candidate, "Синтетическая фикстура; не ответ модели", "fixture-labeler", Hash(LabelPrompt), Hash(text), "synthetic_fixture"})
		verdict := "correct"
		if candidate != label {
			verdict = "incorrect"
		}
		if i == 5 {
			verdict = "incorrect"
		}
		if i == 10 {
			verdict = "correct"
		}
		if i == 7 {
			verdict = Uncertain
		}
		judges = append(judges, Judgement{id, candidate, CandidateHash(text, candidate), verdict, "Синтетическая фикстура; не ответ Judge", "fixture-judge", Hash(JudgePrompt), "synthetic_fixture"})
		var task LSTask
		task.ID = json.RawMessage(fmt.Sprint(i + 1))
		task.Data.Text = text
		task.Data.ExternalID = id
		task.Annotations = []LSAnnotation{
			{ID: i*3 + 1, CompletedBy: json.RawMessage("1"), Result: []LSResult{ChoiceResult(label)}},
			{ID: i*3 + 2, CompletedBy: json.RawMessage(`{"id":2}`), Result: []LSResult{ChoiceResult(other)}},
			{ID: i*3 + 3, CompletedBy: json.RawMessage("3"), GroundTruth: true, Result: []LSResult{ChoiceResult(label)}},
		}
		lsTasks = append(lsTasks, task)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := WriteJSONL(filepath.Join(dir, "messages.jsonl"), messages); err != nil {
		return err
	}
	if err := WriteJSONL(filepath.Join(dir, "annotations.jsonl"), records); err != nil {
		return err
	}
	if err := WriteJSONL(filepath.Join(dir, "predictions.jsonl"), predictions); err != nil {
		return err
	}
	if err := WriteJSONL(filepath.Join(dir, "judgements.jsonl"), judges); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(dir, "labelstudio-export.json"), lsTasks); err != nil {
		return err
	}
	tasks, err := ExportLabelStudio(messages, nil)
	if err != nil {
		return err
	}
	if err = WriteJSON(filepath.Join(dir, "labelstudio-tasks.json"), tasks); err != nil {
		return err
	}
	report, queue, err := BuildReport(records, predictions, judges, "1", "2")
	if err != nil {
		return err
	}
	data, _ := json.Marshal(records)
	report.DatasetSHA256 = Hash(string(data))
	if err = WriteJSON(filepath.Join(dir, "report.json"), report); err != nil {
		return err
	}
	if err = WriteJSON(filepath.Join(dir, "review-queue.json"), queue); err != nil {
		return err
	}
	return WriteText(filepath.Join(dir, "REPORT.md"), Markdown(report))
}
func flip(label string) string {
	if label == OnPlatform {
		return OffPlatform
	}
	return OnPlatform
}
