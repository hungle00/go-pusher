package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type GrantClaims struct {
	AppID    string `json:"app_id"`
	SocketID string `json:"socket_id"`
	Channel  string `json:"channel"`
	jwt.RegisteredClaims
}

type GrantService struct {
	key []byte
	ttl time.Duration
}

func NewGrantService(signingKey string, ttl time.Duration) (*GrantService, error) {
	if strings.TrimSpace(signingKey) == "" {
		return nil, errors.New("channel auth signing key is required")
	}
	if ttl <= 0 {
		return nil, errors.New("channel auth grant TTL must be positive")
	}
	return &GrantService{key: []byte(signingKey), ttl: ttl}, nil
}

func (s *GrantService) Issue(appID, socketID, channel string) (string, error) {
	if appID == "" || socketID == "" || !strings.HasPrefix(channel, "private-") {
		return "", errors.New("app ID, socket ID, and private channel are required")
	}
	now := time.Now()
	claims := GrantClaims{
		AppID:    appID,
		SocketID: socketID,
		Channel:  channel,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-pusher",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
			ID:        socketID + ":" + channel + ":" + now.UTC().Format(time.RFC3339Nano),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
}

func (s *GrantService) Verify(tokenString, appID, socketID, channel string) error {
	if tokenString == "" || appID == "" || socketID == "" || channel == "" {
		return errors.New("authorization, app ID, socket ID, and channel are required")
	}
	parsed, err := jwt.ParseWithClaims(tokenString, &GrantClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.key, nil
	}, jwt.WithIssuer("go-pusher"))
	if err != nil {
		return err
	}
	claims, ok := parsed.Claims.(*GrantClaims)
	if !ok || !parsed.Valid {
		return errors.New("invalid authorization")
	}
	if claims.AppID != appID || claims.SocketID != socketID || claims.Channel != channel {
		return errors.New("authorization does not match subscription")
	}
	return nil
}
