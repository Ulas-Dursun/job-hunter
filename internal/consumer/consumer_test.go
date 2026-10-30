package consumer

import (
	"testing"
)

func TestStringField(t *testing.T) {
	vals := map[string]any{
		"title":   "Backend Go Engineer",
		"company": "JobHunter Inc",
		"count":   100,
	}

	if stringField(vals, "title") != "Backend Go Engineer" {
		t.Errorf("expected Backend Go Engineer")
	}
	if stringField(vals, "company") != "JobHunter Inc" {
		t.Errorf("expected JobHunter Inc")
	}
	if stringField(vals, "non_existent") != "" {
		t.Errorf("expected empty string")
	}
	if stringField(vals, "count") != "" {
		t.Errorf("expected empty string for non-string value")
	}
}
