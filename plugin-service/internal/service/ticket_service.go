package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

var (
	ErrInvalidTicket = errors.New("invalid launch ticket")
	ErrExpiredTicket = errors.New("expired launch ticket")
)

type TicketService struct {
	secret []byte
	now    func() time.Time
}

func NewTicketService(secret string) *TicketService {
	return &TicketService{
		secret: []byte(secret),
		now:    time.Now,
	}
}

func (s *TicketService) CreateTicket(claims model.LaunchClaims, ttl time.Duration) (string, error) {
	now := s.now().UTC()
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(ttl).Unix()
	if claims.Nonce == "" {
		claims.Nonce = randomHex(16)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.sign(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func (s *TicketService) VerifyTicket(ticket string) (*model.LaunchClaims, error) {
	payload, signature, ok := strings.Cut(strings.TrimSpace(ticket), ".")
	if !ok || payload == "" || signature == "" {
		return nil, ErrInvalidTicket
	}
	expected := s.sign(payload)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, ErrInvalidTicket
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidTicket
	}
	var claims model.LaunchClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, ErrInvalidTicket
	}
	if claims.ExpiresAt <= s.now().UTC().Unix() {
		return nil, ErrExpiredTicket
	}
	if claims.UserID <= 0 || claims.Plugin == "" {
		return nil, ErrInvalidTicket
	}
	if claims.Role != model.RoleAdmin {
		claims.Role = model.RoleUser
	}
	return &claims, nil
}

func (s *TicketService) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}
