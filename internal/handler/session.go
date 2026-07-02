package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "qiniu_playground_session"
	oauthStateCookie  = "qiniu_playground_oauth_state"
	sessionMaxAge     = 7 * 24 * time.Hour
)

type sessionSigner struct {
	secret []byte
}

func newSessionSigner(secret string) sessionSigner {
	return sessionSigner{secret: []byte(secret)}
}

func (s sessionSigner) Sign(accountID string, now time.Time) string {
	payload := fmt.Sprintf("%s.%d", accountID, now.Unix())
	sig := s.sign(payload)
	return payload + "." + sig
}

func (s sessionSigner) Verify(value string, now time.Time) (string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid session format")
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(s.sign(payload))) {
		return "", fmt.Errorf("invalid session signature")
	}
	issuedAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid session timestamp")
	}
	if now.Sub(time.Unix(issuedAt, 0)) > sessionMaxAge {
		return "", fmt.Errorf("session expired")
	}
	return parts[0], nil
}

func (s sessionSigner) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
