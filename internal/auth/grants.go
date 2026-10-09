package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type GrantClaims struct {
	AppID       string       `json:"app_id"`
	SocketID    string       `json:"socket_id"`
	Channel     string       `json:"channel"`
	ChannelData *ChannelData `json:"channel_data,omitempty"`
	jwt.RegisteredClaims
}

type ChannelData struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info,omitempty"`
}

const maxPresenceUserInfoBytes = 4 << 10

func (data *ChannelData) Validate() error {
	if data == nil || strings.TrimSpace(data.UserID) == "" {
		return errors.New("presence channel requires a user ID")
	}
	if len(data.UserInfo) > maxPresenceUserInfoBytes || (len(data.UserInfo) > 0 && (!json.Valid(data.UserInfo) || bytes.Equal(bytes.TrimSpace(data.UserInfo), []byte("null")))) {
		return errors.New("presence user info must be valid JSON under 4 KiB")
	}
	return nil
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

func (s *GrantService) Issue(appID, socketID, channel string, channelData *ChannelData) (string, error) {
	if channelData != nil {
		copy := *channelData
		copy.UserID = strings.TrimSpace(copy.UserID)
		copy.UserInfo = append(json.RawMessage(nil), copy.UserInfo...)
		channelData = &copy
	}
	isPrivate := strings.HasPrefix(channel, "private-")
	isPresence := strings.HasPrefix(channel, "presence-")
	if appID == "" || socketID == "" || (!isPrivate && !isPresence) {
		return "", errors.New("app ID, socket ID, and private or presence channel are required")
	}
	if isPresence {
		if err := channelData.Validate(); err != nil {
			return "", err
		}
	}
	if isPrivate && channelData != nil {
		return "", errors.New("private channel does not accept presence channel data")
	}
	if channelData != nil {
		if err := channelData.Validate(); err != nil {
			return "", err
		}
	}
	now := time.Now()
	claims := GrantClaims{
		AppID:       appID,
		SocketID:    socketID,
		Channel:     channel,
		ChannelData: channelData,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-pusher",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
			ID:        socketID + ":" + channel + ":" + now.UTC().Format(time.RFC3339Nano),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
}

func (s *GrantService) Verify(tokenString, appID, socketID, channel string) (*GrantClaims, error) {
	if tokenString == "" || appID == "" || socketID == "" || channel == "" {
		return nil, errors.New("authorization, app ID, socket ID, and channel are required")
	}
	parsed, err := jwt.ParseWithClaims(tokenString, &GrantClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.key, nil
	}, jwt.WithIssuer("go-pusher"))
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*GrantClaims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid authorization")
	}
	if claims.AppID != appID || claims.SocketID != socketID || claims.Channel != channel {
		return nil, errors.New("authorization does not match subscription")
	}
	if strings.HasPrefix(channel, "presence-") && (claims.ChannelData == nil || claims.ChannelData.UserID == "") {
		return nil, errors.New("presence authorization has no user identity")
	}
	if strings.HasPrefix(channel, "private-") && claims.ChannelData != nil {
		return nil, errors.New("private authorization contains unexpected channel data")
	}
	return claims, nil
}
