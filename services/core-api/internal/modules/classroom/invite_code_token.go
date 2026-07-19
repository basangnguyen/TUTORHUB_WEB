package classroom

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	classInviteCodeTokenPrefix  = "thciv1_"
	classInviteCodeTokenPurpose = "class-invite-code-v1"
	classInviteCodeTokenBytes   = 32
)

type ClassInviteCodeTokenCodec interface {
	RandomToken() (string, error)
	Digest(string, string) []byte
}

func generateClassInviteCodeToken(
	codec ClassInviteCodeTokenCodec,
) (string, []byte, error) {
	if codec == nil {
		return "", nil, fmt.Errorf("class invite code codec is required")
	}
	randomValue, err := codec.RandomToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate class invite code token: %w", err)
	}
	if _, err := normalizeClassInviteCodeRandomValue(randomValue); err != nil {
		return "", nil, err
	}
	token := classInviteCodeTokenPrefix + randomValue
	hash := codec.Digest(classInviteCodeTokenPurpose, token)
	if len(hash) != 32 {
		return "", nil, fmt.Errorf("class invite code digest must contain 32 bytes")
	}
	return token, append([]byte(nil), hash...), nil
}

func digestClassInviteCodeToken(
	codec ClassInviteCodeTokenCodec,
	rawToken string,
) ([]byte, error) {
	if codec == nil {
		return nil, ErrClassInviteCodeUnavailable
	}
	token, err := normalizeClassInviteCodeToken(rawToken)
	if err != nil {
		return nil, err
	}
	hash := codec.Digest(classInviteCodeTokenPurpose, token)
	if len(hash) != 32 {
		return nil, ErrClassInviteCodeUnavailable
	}
	return append([]byte(nil), hash...), nil
}

func normalizeClassInviteCodeToken(rawToken string) (string, error) {
	token := strings.TrimSpace(rawToken)
	if !strings.HasPrefix(token, classInviteCodeTokenPrefix) {
		return "", ErrClassInviteCodeUnavailable
	}
	randomValue := strings.TrimPrefix(token, classInviteCodeTokenPrefix)
	if _, err := normalizeClassInviteCodeRandomValue(randomValue); err != nil {
		return "", ErrClassInviteCodeUnavailable
	}
	return token, nil
}

func normalizeClassInviteCodeRandomValue(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != classInviteCodeTokenBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return "", ErrClassInviteCodeUnavailable
	}
	return value, nil
}
