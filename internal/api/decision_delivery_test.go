package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecisionDeliveryIsAnOptionalAdditionalField(t *testing.T) {
	raw, err := json.Marshal(Decision{Status: "answered", Delivery: &DecisionDelivery{Status: "pending", Text: "回答文"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"answered"`) || !strings.Contains(string(raw), `"delivery":{"status":"pending","text":"回答文"`) {
		t.Fatal(string(raw))
	}
	raw, err = json.Marshal(Decision{Status: "pending"})
	if err != nil || strings.Contains(string(raw), `"delivery"`) {
		t.Fatal(string(raw), err)
	}
}
