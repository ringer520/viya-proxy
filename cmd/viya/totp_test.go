package main

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B uses the ASCII secret "12345678901234567890", which is
// this base32 string. The published vectors are 8 digits; Viya issues 6, so the
// assertion compares the trailing six digits.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, vector := range vectors {
		code, err := totpCode(rfcSecret, time.Unix(vector.unix, 0))
		if err != nil {
			t.Fatalf("totpCode(%d): %v", vector.unix, err)
		}
		if code != vector.want {
			t.Errorf("totpCode(%d) = %q, want %q", vector.unix, code, vector.want)
		}
		if !verifyTOTP(rfcSecret, code, time.Unix(vector.unix, 0)) {
			t.Errorf("verifyTOTP rejected the RFC code for %d", vector.unix)
		}
	}
}

func TestVerifyTOTPAcceptsAdjacentStepsOnly(t *testing.T) {
	at := time.Unix(1111111109, 0)
	current, err := totpCode(rfcSecret, at)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyTOTP(rfcSecret, current, at) {
		t.Error("current step should verify")
	}

	previous, _ := totpCode(rfcSecret, at.Add(-totpPeriod))
	if !verifyTOTP(rfcSecret, previous, at) {
		t.Error("previous step should verify within the skew window")
	}
	next, _ := totpCode(rfcSecret, at.Add(totpPeriod))
	if !verifyTOTP(rfcSecret, next, at) {
		t.Error("next step should verify within the skew window")
	}

	far, _ := totpCode(rfcSecret, at.Add(-3*totpPeriod))
	if verifyTOTP(rfcSecret, far, at) {
		t.Error("a code three steps away must not verify")
	}
	if verifyTOTP(rfcSecret, "000000", at) && current != "000000" {
		t.Error("an unrelated code must not verify")
	}
}

func TestVerifyTOTPRejectsMalformedInput(t *testing.T) {
	at := time.Unix(1111111109, 0)
	for _, code := range []string{"", "12345", "1234567", "abcdef", "12 34 56"} {
		if verifyTOTP(rfcSecret, code, at) {
			t.Errorf("verifyTOTP accepted malformed code %q", code)
		}
	}
	if verifyTOTP("not-base32-!!!", "123456", at) {
		t.Error("verifyTOTP accepted a malformed secret")
	}
}

func TestNewTOTPSecretIsUsable(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("secret length = %d, want 32 base32 characters", len(secret))
	}
	if strings.ContainsAny(secret, "= ") {
		t.Fatalf("secret %q should be unpadded base32", secret)
	}
	now := time.Now()
	code, err := totpCode(secret, now)
	if err != nil {
		t.Fatalf("generated secret does not decode: %v", err)
	}
	if !verifyTOTP(secret, code, now) {
		t.Error("generated secret rejected its own code")
	}
}

func TestTOTPProvisioningURIContainsRequiredFields(t *testing.T) {
	uri := totpProvisioningURI(totpIssuer, "admin", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{"otpauth://totp/", "secret=JBSWY3DPEHPK3PXP", "issuer=Viya", "digits=6", "period=30", "algorithm=SHA1"} {
		if !strings.Contains(uri, want) {
			t.Errorf("provisioning URI %q is missing %q", uri, want)
		}
	}
}
