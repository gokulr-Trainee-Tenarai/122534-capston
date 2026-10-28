package productservice

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

var productServiceTestSecret = []byte("product-service-test-secret-at-least-32-bytes")

func productServiceTestToken(t *testing.T, role model.UserRole) string {
	t.Helper()
	token, err := authn.IssueToken(productServiceTestSecret, model.User{
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

func TestProductRoutesHealthAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, productServiceTestSecret).RegisterRoutes(router)

	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"status":"live"`) {
		t.Fatalf("live endpoint returned %d: %s", live.Code, live.Body.String())
	}

	price := httptest.NewRecorder()
	router.ServeHTTP(price, httptest.NewRequest(http.MethodGet, "/products/501", nil))
	if price.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated product lookup returned %d, want %d", price.Code, http.StatusUnauthorized)
	}

	create := httptest.NewRequest(http.MethodPost, "/products", strings.NewReader(`{"id":501,"name":"product","price":1}`))
	create.Header.Set("Authorization", productServiceTestToken(t, model.RoleCustomer))
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusForbidden {
		t.Fatalf("customer product creation returned %d, want %d", createResponse.Code, http.StatusForbidden)
	}
}

func TestProductRoutesRejectInvalidInputsBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, productServiceTestSecret).RegisterRoutes(router)
	adminToken := productServiceTestToken(t, model.RoleAdmin)

	invalidID := httptest.NewRequest(http.MethodGet, "/products/not-a-number", nil)
	invalidID.Header.Set("Authorization", adminToken)
	invalidIDResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidIDResponse, invalidID)
	if invalidIDResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid product ID returned %d, want %d", invalidIDResponse.Code, http.StatusBadRequest)
	}

	invalidProduct := httptest.NewRequest(http.MethodPost, "/products", strings.NewReader(`{"id":-1,"name":" ","price":0}`))
	invalidProduct.Header.Set("Authorization", adminToken)
	invalidProductResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidProductResponse, invalidProduct)
	if invalidProductResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid product returned %d, want %d", invalidProductResponse.Code, http.StatusBadRequest)
	}
}

func TestBootstrapProductValidationAndOptionalConfiguration(t *testing.T) {
	if err := BootstrapProduct(nil, 0, "", 0); err != nil {
		t.Fatalf("empty optional bootstrap config returned error: %v", err)
	}
	if err := BootstrapProduct(nil, 501, "", 12.5); err == nil {
		t.Fatal("expected incomplete bootstrap product settings to be rejected")
	}
}
