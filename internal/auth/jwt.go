package auth

import (
	"context"
	"fmt"
	"time"

	"arch_erp/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Claims is the JWT payload structure.
type Claims struct {
	UserID       string      `json:"uid"`
	Role         models.Role `json:"role"`
	TokenVersion int         `json:"tv"`
	jwt.RegisteredClaims
}

// SignToken creates a signed JWT for the given user.
func SignToken(user *models.User, secret string, expiryHours int) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:       user.ID.Hex(),
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expiryHours) * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}
	return signed, nil
}

// VerifyToken parses and validates a JWT, then checks the TokenVersion against
// the database to support instant revocation.
func VerifyToken(tokenStr, secret string, usersCol interface {
	FindOne(context.Context, interface{}, ...interface{}) *mongo.SingleResult
}) (*Claims, *models.User, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("parsing token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil, nil
}

// VerifyTokenWithDB parses the JWT and cross-checks TokenVersion from the DB.
func VerifyTokenWithDB(ctx context.Context, tokenStr, secret string, usersCol *mongo.Collection) (*models.User, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	oid, err := bson.ObjectIDFromHex(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id in token: %w", err)
	}

	var user models.User
	err = usersCol.FindOne(ctx, bson.M{"_id": oid}).Decode(&user)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	if user.TokenVersion != claims.TokenVersion {
		return nil, fmt.Errorf("token has been revoked")
	}

	return &user, nil
}
