package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type PopupSize struct{ Cells, Percent int }

func (s *PopupSize) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if !strings.HasSuffix(value, "%") {
			return fmt.Errorf("popup size requires cells or 1%%..100%%")
		}
		n, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
		if err != nil || n < 1 || n > 100 || strconv.Itoa(n)+"%" != value {
			return fmt.Errorf("popup percentage must be 1..100")
		}
		s.Percent, s.Cells = n, 0
		return nil
	}
	var n int
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&n); err != nil {
		return err
	}
	if n < 0 || n > 65535 {
		return fmt.Errorf("popup cells must be 0..65535")
	}
	s.Cells, s.Percent = n, 0
	return nil
}

func (s PopupSize) MarshalJSON() ([]byte, error) {
	if s.Percent > 0 {
		return json.Marshal(strconv.Itoa(s.Percent) + "%")
	}
	return json.Marshal(s.Cells)
}

type PaneCommandExecuteParams struct {
	PaneID  string     `json:"pane_id"`
	Command string     `json:"command"`
	Action  string     `json:"action"`
	Width   *PopupSize `json:"width,omitempty"`
	Height  *PopupSize `json:"height,omitempty"`
}

type CommandExecutedResult struct {
	Type            string    `json:"type"`
	Pane            *PaneInfo `json:"pane"`
	PopupTerminalID *string   `json:"popup_terminal_id"`
}

type PopupTarget struct {
	TerminalID string `json:"terminal_id"`
}
type PopupScrollParams struct {
	TerminalID       string `json:"terminal_id"`
	OffsetFromBottom uint64 `json:"offset_from_bottom"`
}
type PopupSelectionReadParams struct {
	TerminalID      string        `json:"terminal_id"`
	Anchor          PaneTextPoint `json:"anchor"`
	Cursor          PaneTextPoint `json:"cursor"`
	ContentRevision uint64        `json:"content_revision"`
}
type PopupTerminalResult struct {
	Type            string         `json:"type"`
	TerminalID      string         `json:"terminal_id"`
	ContentRevision uint64         `json:"content_revision"`
	Scroll          PaneScrollInfo `json:"scroll"`
}
type PopupSelectionResult struct {
	Type            string `json:"type"`
	TerminalID      string `json:"terminal_id"`
	ContentRevision uint64 `json:"content_revision"`
	Text            string `json:"text"`
}
