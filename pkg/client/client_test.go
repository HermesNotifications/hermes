package client_test

import (
	"net/http"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestNew(t *testing.T) {
	c := client.New("http://localhost:8080", "test-key")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := client.New("http://localhost:8080", "test-key", client.WithHTTPClient(custom))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &client.APIError{StatusCode: 400, Message: "bad input"}
	expected := "API error (400): bad input"
	if err.Error() != expected {
		t.Errorf("got %q, want %q", err.Error(), expected)
	}
}
