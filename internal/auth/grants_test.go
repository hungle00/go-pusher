package auth

import (
	"strings"
	"testing"
	"time"
)

func TestGrantRoundTrip(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Issue("app-1", "socket-1", "private-orders")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(grant, "app-1", "socket-1", "private-orders"); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
}

func TestGrantRejectsDifferentSubscription(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Issue("app-1", "socket-1", "private-orders")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(grant, "app-1", "socket-1", "private-payments"); err == nil {
		t.Fatal("grant for another channel was accepted")
	}
}

func TestGrantRejectsExpired(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Issue("app-1", "socket-1", "private-orders")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := service.Verify(grant, "app-1", "socket-1", "private-orders"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired grant was not rejected as expected: %v", err)
	}
}
