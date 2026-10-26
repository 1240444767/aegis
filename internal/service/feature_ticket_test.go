package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
	vipdomain "aegis/internal/domain/vip"
	apperrors "aegis/pkg/errors"
)

func TestFeatureTicketRequestValidation(t *testing.T) {
	ok, err := NormalizeFeatureTicketRequest(FeatureTicketRequest{Feature: " Custom_Splash ", Digest: "abc-_", Nonce: "0123456789abcdef"})
	if err != nil || ok.Feature != "custom_splash" {
		t.Fatalf("valid request rejected: %v %+v", err, ok)
	}
	bad := []FeatureTicketRequest{
		{Feature: "", Nonce: "0123456789abcdef"},
		{Feature: "custom_splash", Nonce: "short"},
		{Feature: "custom_splash", Nonce: "0123456789abcdef", Digest: "not base64url!"},
		{Feature: "custom_splash", Nonce: "0123456789abcdef+/"},
	}
	for _, req := range bad {
		if _, err := NormalizeFeatureTicketRequest(req); err == nil {
			t.Fatalf("invalid request accepted: %+v", req)
		}
	}
}

func TestBuildFeatureTicketExpiry(t *testing.T) {
	app := &appdomain.App{AppKey: "app_x"}
	req := FeatureTicketRequest{Feature: "custom_splash", Digest: "d", Nonce: "0123456789abcdef"}
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

	_, err := BuildFeatureTicket(app, req, &vipdomain.Verification{Granted: false}, "dev", now)
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != errCodeFeatureNotGranted {
		t.Fatalf("not granted should be 40230, got %v", err)
	}

	lifetime := &vipdomain.Verification{Granted: true, UserID: 42, Membership: vipdomain.MembershipView{IsVIP: true, IsLifetime: true}}
	ticket, err := BuildFeatureTicket(app, req, lifetime, " dev ", now)
	if err != nil {
		t.Fatal(err)
	}
	if !ticket.Lifetime || ticket.UserID != 42 || ticket.DeviceID != "dev" || ticket.Challenge != req.Nonce ||
		ticket.ExpiresAt != now.Add(featureTicketLifetimeTTL).Unix() {
		t.Fatalf("unexpected lifetime ticket %+v", ticket)
	}

	soon := now.Add(2 * time.Hour)
	timed := &vipdomain.Verification{Granted: true, Membership: vipdomain.MembershipView{IsVIP: true, ExpireAt: &soon}}
	ticket, err = BuildFeatureTicket(app, req, timed, "dev", now)
	if err != nil || ticket.Lifetime || ticket.ExpiresAt != soon.Unix() {
		t.Fatalf("timed ticket should end with membership: %+v %v", ticket, err)
	}
}

func TestFeatureTicketSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(FeatureTicket{V: 1, AppKey: "app_x", Feature: "custom_splash"})
	signature := signFeatureTicket(private, raw)
	if !VerifyFeatureTicket(public, raw, signature) {
		t.Fatal("signature does not verify")
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] ^= 1
	if VerifyFeatureTicket(public, tampered, signature) {
		t.Fatal("tampered ticket verified")
	}
	// 与版本管控裁决的签名域分开：同一把密钥签的裁决不能冒充凭证
	if VerifyFeatureTicket(public, raw, signClientVerdict(private, raw)) {
		t.Fatal("verdict signature accepted as ticket")
	}
}
