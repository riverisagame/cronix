package model

import (
	"testing"
)

func TestExecutionLogStateTransition(t *testing.T) {
	log := &ExecutionLog{Status: "pending"}

	// pending -> running (should pass)
	if err := log.TransitionTo("running"); err != nil {
		t.Errorf("Expected transition to running to succeed, got %v", err)
	}
	if log.Status != "running" {
		t.Errorf("Expected status to be running, got %s", log.Status)
	}

	// running -> success (should pass)
	if err := log.TransitionTo("success"); err != nil {
		t.Errorf("Expected transition to success to succeed, got %v", err)
	}
	if log.Status != "success" {
		t.Errorf("Expected status to be success, got %s", log.Status)
	}

	// success -> running (should fail, terminal state)
	if err := log.TransitionTo("running"); err == nil {
		t.Errorf("Expected transition to running from success to fail")
	}

	// failed -> pending (should pass, for retry)
	log.Status = "failed"
	if err := log.TransitionTo("pending"); err != nil {
		t.Errorf("Expected transition to pending from failed to succeed, got %v", err)
	}
}
