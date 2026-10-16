package api

import (
	"encoding/json"
	"fmt"
)

type NotificationShowSound string

func (s *NotificationShowSound) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	switch value {
	case "none", "done", "request":
		*s = NotificationShowSound(value)
		return nil
	}
	return fmt.Errorf("notification sound must be none, done, or request")
}

type NotificationShowParams struct {
	Title    *string               `json:"title"`
	Body     *string               `json:"body,omitempty"`
	Sound    NotificationShowSound `json:"sound,omitempty"`
	Position *string               `json:"position,omitempty"`
}

type NotificationShowResult struct {
	Type   string `json:"type"`
	Shown  bool   `json:"shown"`
	Reason string `json:"reason"`
}
