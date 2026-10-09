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
	grant, err := service.Issue("app-1", "socket-1", "private-orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(grant, "app-1", "socket-1", "private-orders"); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
}

func TestGrantRejectsDifferentSubscription(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Issue("app-1", "socket-1", "private-orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(grant, "app-1", "socket-1", "private-payments"); err == nil {
		t.Fatal("grant for another channel was accepted")
	}
}

func TestGrantRejectsExpired(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Issue("app-1", "socket-1", "private-orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := service.Verify(grant, "app-1", "socket-1", "private-orders"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired grant was not rejected as expected: %v", err)
	}
}

func TestPresenceGrantCarriesSignedIdentity(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	data := &ChannelData{UserID: " user-7 ", UserInfo: []byte(`{"name":"Ada"}`)}
	grant, err := service.Issue("app-1", "socket-1", "presence-room-1", data)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.Verify(grant, "app-1", "socket-1", "presence-room-1")
	if err != nil {
		t.Fatalf("valid presence grant rejected: %v", err)
	}
	if claims.ChannelData.UserID != "user-7" || string(claims.ChannelData.UserInfo) != `{"name":"Ada"}` {
		t.Fatalf("unexpected signed channel data: %#v", claims.ChannelData)
	}
}

func TestPresenceGrantRequiresUserID(t *testing.T) {
	service, err := NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue("app-1", "socket-1", "presence-room-1", &ChannelData{}); err == nil {
		t.Fatal("presence grant without user ID was accepted")
	}
}
