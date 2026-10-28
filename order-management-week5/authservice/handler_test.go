package authservice

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"order-management/authn"
	"order-management/model"

	"github.com/gin-gonic/gin"
)

var authServiceTestSecret = []byte("auth-service-test-secret-at-least-32-bytes")

func authServiceTestToken(t *testing.T, role model.UserRole) string {
	t.Helper()
	token, err := authn.IssueToken(authServiceTestSecret, model.User{
		ID:         1,
		Username:   "test-user",
		Role:       role,
		CustomerID: 101,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

func TestAuthServiceHealthAndCustomerAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, authServiceTestSecret).RegisterRoutes(router)

	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"status":"live"`) {
		t.Fatalf("live endpoint returned %d: %s", live.Code, live.Body.String())
	}

	for _, test := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "unauthenticated", status: http.StatusUnauthorized},
		{name: "customer is forbidden", token: authServiceTestToken(t, model.RoleCustomer), status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/auth/users", strings.NewReader(`{}`))
			if test.token != "" {
				request.Header.Set("Authorization", test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("POST /auth/users returned %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestAuthServiceRejectsMalformedLoginAndInvalidCustomer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, authServiceTestSecret).RegisterRoutes(router)

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{}`)))
	if login.Code != http.StatusBadRequest {
		t.Fatalf("malformed login returned %d, want %d", login.Code, http.StatusBadRequest)
	}

	request := httptest.NewRequest(http.MethodPost, "/auth/users", strings.NewReader(`{"username":"ab","password":"short","customerId":0}`))
	request.Header.Set("Authorization", authServiceTestToken(t, model.RoleAdmin))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid customer request returned %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestBootstrapAdminValidatesCredentialsBeforeDatabaseAccess(t *testing.T) {
	if err := BootstrapAdmin(nil, "a", "short"); err == nil {
		t.Fatal("expected invalid bootstrap credentials to be rejected")
	}
}
