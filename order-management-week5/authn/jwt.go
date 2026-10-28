package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"order-management/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Principal struct {
	UserID     int64
	Username   string
	Role       model.UserRole
	CustomerID int64
}

type Claims struct {
	UserID     int64          `json:"uid"`
	Role       model.UserRole `json:"role"`
	CustomerID int64          `json:"customer_id"`
	jwt.RegisteredClaims
}

type principalContextKey struct{}
type bearerTokenContextKey struct{}

func IssueToken(secret []byte, user model.User, lifetime time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:     user.ID,
		Role:       user.Role,
		CustomerID: user.CustomerID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

func Middleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bearer token required"})
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(
			strings.TrimPrefix(header, "Bearer "),
			claims,
			func(token *jwt.Token) (any, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, jwt.ErrSignatureInvalid
				}
				return secret, nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithExpirationRequired(),
		)
		if err != nil || token == nil || !token.Valid || claims.UserID <= 0 ||
			(claims.Role != model.RoleAdmin && claims.Role != model.RoleCustomer) ||
			(claims.Role == model.RoleCustomer && claims.CustomerID <= 0) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		principal := Principal{
			UserID:     claims.UserID,
			Username:   claims.Subject,
			Role:       claims.Role,
			CustomerID: claims.CustomerID,
		}
		c.Set("principal", principal)
		bearerToken := strings.TrimPrefix(header, "Bearer ")
		ctx := context.WithValue(c.Request.Context(), principalContextKey{}, principal)
		ctx = context.WithValue(ctx, bearerTokenContextKey{}, bearerToken)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := FromGin(c)
		if !ok || principal.Role != model.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator role required"})
			return
		}
		c.Next()
	}
}

func FromGin(c *gin.Context) (Principal, bool) {
	value, ok := c.Get("principal")
	if !ok {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	return principal, ok
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func BearerTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(bearerTokenContextKey{}).(string)
	return token
}

func CustomerScope(principal Principal) *int64 {
	if principal.Role == model.RoleAdmin {
		return nil
	}
	customerID := principal.CustomerID
	return &customerID
}

var ErrForbidden = errors.New("forbidden")
