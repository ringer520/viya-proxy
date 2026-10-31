package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238) is implemented directly instead of pulling in a dependency:
// the algorithm is small, and a hand-rolled version keeps the module graph and
// the offline build story unchanged.
const (
	totpPeriod = 30 * time.Second
	totpDigits = 6
	// totpSkew accepts the previous and next step so slight clock drift between
	// the phone and the server does not lock the operator out.
	totpSkew = 1
	// totpSecretBytes is 160 bits, the length RFC 4226 recommends for SHA-1.
	totpSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a base32 secret without padding, matching what
// authenticator apps expect.
func newTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(buf), nil
}

func hotpCode(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, value%mod)
}

func totpCode(secret string, at time.Time) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", errors.New("invalid TOTP secret")
	}
	return hotpCode(key, uint64(at.Unix()/int64(totpPeriod.Seconds()))), nil
}

// verifyTOTP reports whether code is valid for the step containing now, or for
// the immediately adjacent steps.
func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return false
	}
	counter := now.Unix() / int64(totpPeriod.Seconds())
	for delta := int64(-totpSkew); delta <= int64(totpSkew); delta++ {
		if counter+delta < 0 {
			continue
		}
		expected := hotpCode(key, uint64(counter+delta))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// totpProvisioningURI builds the otpauth:// URI an authenticator app scans or
// accepts as a manual entry.
func totpProvisioningURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + query.Encode()
}
