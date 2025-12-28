package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateToken creates a new JWT token with the given client ID and secret
func GenerateToken(clientID, secret string, expirationDays int) (string, error) {
	if clientID == "" {
		return "", fmt.Errorf("client ID cannot be empty")
	}
	if secret == "" {
		return "", fmt.Errorf("secret cannot be empty")
	}

	claims := jwt.MapClaims{
		"sub": clientID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour * 24 * time.Duration(expirationDays)).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
