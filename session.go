package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionManager provides a small, signed-cookie session storing the username.
type SessionManager struct {
	key          []byte
	cookieName   string
	lifetime     time.Duration
	cookiePath   string
	cookieDomain string
	cookieSecure bool
	sameSite     http.SameSite
}

func NewSessionManager(key []byte) *SessionManager {
	return &SessionManager{
		key:          key,
		cookieName:   "session",
		lifetime:     7 * 24 * time.Hour,
		cookiePath:   "/",
		cookieSecure: false, // set true when serving HTTPS behind a reverse proxy
		sameSite:     http.SameSiteLaxMode,
	}
}

// cookie payload: base64(username|expiryUnix|sig)
func (s *SessionManager) SetUsername(w http.ResponseWriter, username string) error {
	expiry := time.Now().Add(s.lifetime).Unix()
	payload := fmt.Sprintf("%s|%d", username, expiry)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	token := payload + "|" + base64.RawURLEncoding.EncodeToString(sig)
	cookie := &http.Cookie{
		Name:     s.cookieName,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(token)),
		Path:     s.cookiePath,
		Domain:   s.cookieDomain,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: s.sameSite,
		Expires:  time.Unix(expiry, 0),
	}
	http.SetCookie(w, cookie)
	return nil
}

func (s *SessionManager) GetUsernameFromRequest(r *http.Request) (string, error) {
	c, err := r.Cookie(s.cookieName)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", errors.New("invalid token")
	}
	username, expiryStr, sigB64 := parts[0], parts[1], parts[2]
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return "", err
	}
	if time.Now().Unix() > expiry {
		return "", errors.New("expired")
	}
	payload := username + "|" + expiryStr
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	given, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", err
	}
	if !hmac.Equal(expected, given) {
		return "", errors.New("bad signature")
	}
	return username, nil
}

func (s *SessionManager) Clear(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     s.cookiePath,
		Domain:   s.cookieDomain,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: s.sameSite,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

// Password helpers
func hashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password empty")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func comparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
