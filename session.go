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

// SessionManager provides a small, signed-cookie session storing the user ID.
type SessionManager struct {
    key           []byte
    cookieName    string
    lifetime      time.Duration
    cookiePath    string
    cookieDomain  string
    cookieSecure  bool
    sameSite      http.SameSite
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

// cookie payload: base64(userID|expiryUnix|sig)
func (s *SessionManager) SetUserID(w http.ResponseWriter, userID int64) error {
    expiry := time.Now().Add(s.lifetime).Unix()
    payload := fmt.Sprintf("%d|%d", userID, expiry)
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

func (s *SessionManager) GetUserIDFromRequest(r *http.Request) (int64, error) {
    c, err := r.Cookie(s.cookieName)
    if err != nil { return 0, err }
    raw, err := base64.RawURLEncoding.DecodeString(c.Value)
    if err != nil { return 0, err }
    parts := strings.Split(string(raw), "|")
    if len(parts) != 3 { return 0, errors.New("invalid token") }
    userIDStr, expiryStr, sigB64 := parts[0], parts[1], parts[2]
    expiry, err := strconv.ParseInt(expiryStr, 10, 64)
    if err != nil { return 0, err }
    if time.Now().Unix() > expiry { return 0, errors.New("expired") }
    payload := userIDStr + "|" + expiryStr
    mac := hmac.New(sha256.New, s.key)
    mac.Write([]byte(payload))
    expected := mac.Sum(nil)
    given, err := base64.RawURLEncoding.DecodeString(sigB64)
    if err != nil { return 0, err }
    if !hmac.Equal(expected, given) { return 0, errors.New("bad signature") }
    uid, err := strconv.ParseInt(userIDStr, 10, 64)
    if err != nil { return 0, err }
    return uid, nil
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
    if err != nil { return "", err }
    return string(b), nil
}

func comparePassword(hash, password string) error {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

