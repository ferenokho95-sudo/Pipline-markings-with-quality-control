package qc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	OnPlatform  = "on_platform"
	OffPlatform = "off_platform"
	Uncertain   = "uncertain"
)

// Message is the original data, with no reference labels exposed to the LLM.
type Message struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Record struct {
	ID          string            `json:"id"`
	Text        string            `json:"text"`
	Annotations map[string]string `json:"annotations"`
	Gold        string            `json:"gold,omitempty"`
}

type Prediction struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Reason       string `json:"reason"`
	Model        string `json:"model"`
	PromptSHA256 string `json:"prompt_sha256"`
	TextSHA256   string `json:"text_sha256"`
	Source       string `json:"source"`
}

type Judgement struct {
	ID              string `json:"id"`
	CandidateLabel  string `json:"candidate_label"`
	CandidateSHA256 string `json:"candidate_sha256"`
	Verdict         string `json:"verdict"`
	Reason          string `json:"reason"`
	Model           string `json:"model"`
	PromptSHA256    string `json:"prompt_sha256"`
	Source          string `json:"source"`
}

func Hash(s string) string                    { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func CandidateHash(text, label string) string { return Hash(text + "\x00" + label) }
func ValidLabel(s string) bool                { return s == OnPlatform || s == OffPlatform || s == Uncertain }
func ValidVerdict(s string) bool              { return s == "correct" || s == "incorrect" || s == Uncertain }
func ValidateMessages(rows []Message) error {
	if len(rows) == 0 {
		return fmt.Errorf("input contains no messages")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Text) == "" {
			return fmt.Errorf("id and text must not be empty")
		}
		if seen[r.ID] {
			return fmt.Errorf("duplicate message id %q", r.ID)
		}
		seen[r.ID] = true
	}
	return nil
}

func ValidateRecords(rows []Record) error {
	messages := make([]Message, 0, len(rows))
	for _, r := range rows {
		messages = append(messages, Message{r.ID, r.Text})
		if r.Gold != "" && !ValidLabel(r.Gold) {
			return fmt.Errorf("invalid gold for %s", r.ID)
		}
		for name, label := range r.Annotations {
			if strings.TrimSpace(name) == "" || !ValidLabel(label) {
				return fmt.Errorf("invalid annotation for %s", r.ID)
			}
		}
	}
	return ValidateMessages(messages)
}
