package analyzer

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsRetryable(t *testing.T) {
	if !isRetryable(fmt.Errorf("wrap: %w", &httpStatusError{Provider: "gemini", Status: 429})) {
		t.Error("429 should be retryable")
	}
	if !isRetryable(&httpStatusError{Provider: "groq", Status: 503}) {
		t.Error("503 should be retryable")
	}
	if isRetryable(&httpStatusError{Provider: "groq", Status: 400, Body: "error code 429 in message"}) {
		t.Error("400 with '429' in body must not be retryable")
	}
	if isRetryable(fmt.Errorf("plain 429 text")) {
		t.Error("untyped error must not be retryable")
	}
}

func TestIsTransient(t *testing.T) {
	if !IsTransient(fmt.Errorf("%w: all 3 AI endpoints", ErrAIBusy)) {
		t.Error("ErrAIBusy geçici sayılmalı")
	}
	if !IsTransient(&httpStatusError{Provider: "gemini", Status: 429}) {
		t.Error("429 geçici sayılmalı")
	}
	if IsTransient(&httpStatusError{Provider: "gemini", Status: 400}) {
		t.Error("400 kalıcı sayılmalı")
	}
	if IsTransient(errors.New("parsing score JSON")) {
		t.Error("parse hatası kalıcı sayılmalı")
	}
}
