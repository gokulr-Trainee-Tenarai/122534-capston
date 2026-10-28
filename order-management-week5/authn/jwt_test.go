package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"order-management/model"

	"github.com/gin-gonic/gin"
)

func TestJWTMiddlewareValidatesAndSetsPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret-long-enough-for-jwt-validation")
	tokenString, err := IssueToken(secret, model.User{
		ID:         9,
		Username:   "customer",
		Role:       model.RoleCustomer,
		CustomerID: 101,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/orders", Middleware(secret), func(c *gin.Context) {
		principal, ok := FromGin(c)
		if !ok {
			t.Fatal("principal was not added to Gin context")
		}
		if got := BearerTokenFromContext(c.Request.Context()); got != tokenString {
			t.Errorf("context bearer token = %q, want original token", got)
		}
		c.JSON(http.StatusOK, gin.H{"customerId": principal.CustomerID, "role": principal.Role})
	})
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("Authorization", "Bearer "+tokenString)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid token returned %d: %s", response.Code, response.Body.String())
	}
}

func TestJWTMiddlewareRejectsMissingExpiredAndInvalidTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret-long-enough-for-jwt-validation")
	expired, err := IssueToken(secret, model.User{
		ID:       9,
		Username: "admin",
		Role:     model.RoleAdmin,
	}, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/protected", Middleware(secret), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for _, bearer := range []string{"", "Bearer malformed", "Bearer " + expired} {
		request := httptest.NewRequest(http.MethodGet, "/protected", nil)
		if bearer != "" {
			request.Header.Set("Authorization", bearer)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("token %q returned %d, want %d", bearer, response.Code, http.StatusUnauthorized)
		}
	}
}
