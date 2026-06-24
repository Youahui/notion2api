package service

import "testing"

func TestIsAccountFailoverErrorIncludesPremiumFeatureUnavailable(t *testing.T) {
	err := &NotionInferenceStepError{
		ThreadID: "thread-1",
		Message:  "premium feature unavailable limit_type=cumulative current=84 total=75",
		SubType:  "premium-feature-unavailable",
	}

	if !isAccountFailoverError(err) {
		t.Fatal("expected premium feature quota errors to trigger account failover")
	}
}

func TestIsAccountFailoverErrorIncludesRetryableInferenceStep(t *testing.T) {
	err := &NotionInferenceStepError{
		ThreadID:  "thread-1",
		Message:   "model overloaded",
		SubType:   "inference_failed",
		Retryable: true,
	}

	if !isAccountFailoverError(err) {
		t.Fatal("expected retryable inference step errors to trigger account failover")
	}
}

func TestIsAccountFailoverErrorRejectsNonRetryableInferenceStep(t *testing.T) {
	err := &NotionInferenceStepError{
		ThreadID: "thread-1",
		Message:  "invalid request",
		SubType:  "validation_error",
	}

	if isAccountFailoverError(err) {
		t.Fatal("expected non-retryable inference step errors to stay on the selected account")
	}
}
