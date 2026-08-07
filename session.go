package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Session mirrors the Flask session keys used by the original app.
type Session struct {
	Nickname        string `json:"nickname,omitempty"`
	LoggedIn        bool   `json:"logged_in,omitempty"`
	AcceptanceToken string `json:"acceptance_token,omitempty"`
	IsAdmin         bool   `json:"is_admin,omitempty"`
	Permanent       bool   `json:"permanent,omitempty"`
}

const sessionCookieName = "session"

func signValue(v string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(v))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeSession(s *Session) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	payload := base64.URLEncoding.EncodeToString(b)
	return payload + "." + signValue(payload), nil
}

func decodeSession(cookie string) *Session {
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		return nil
	}
	if !hmac.Equal([]byte(signValue(parts[0])), []byte(parts[1])) {
		return nil
	}
	b, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil
	}
	return &s
}

func getSession(c *fiber.Ctx) *Session {
	cookie := c.Cookies(sessionCookieName)
	if cookie == "" {
		return &Session{}
	}
	if s := decodeSession(cookie); s != nil {
		return s
	}
	return &Session{}
}

func setSession(c *fiber.Ctx, s *Session) {
	enc, err := encodeSession(s)
	if err != nil {
		return
	}
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    enc,
		Path:     "/",
		HTTPOnly: true,
		MaxAge:   7 * 24 * 60 * 60,
		SameSite: "Lax",
	})
}

func clearSession(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		MaxAge:   -1,
		SameSite: "Lax",
	})
}

// sessionFromRequest reads the session cookie from a raw *http.Request.
// Used by the socket.io connect handler.
func sessionFromRequest(r *http.Request) *Session {
	if r == nil {
		return &Session{}
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return &Session{}
	}
	if s := decodeSession(cookie.Value); s != nil {
		return s
	}
	return &Session{}
}
