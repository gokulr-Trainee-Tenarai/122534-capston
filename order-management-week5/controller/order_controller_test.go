package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"order-management/authn"
	"order-management/model"
	"order-management/repository"
	"order-management/service"

	"github.com/gin-gonic/gin"
)

var controllerTestSecret = []byte("controller-test-secret-at-least-32-bytes")

func controllerTestToken(t *testing.T, customerID int64) string {
	t.Helper()
	token, err := authn.IssueToken(controllerTestSecret, model.User{
		ID:         customerID,
		Username:   "customer",
		Role:       model.RoleCustomer,
		CustomerID: customerID,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

func TestGraphQLCreateReadListAndUpdateOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(authn.Middleware(controllerTestSecret))

	svc := service.NewOrderService(repository.NewMemoryRepository(), graphqlProductClient{})
	RegisterGraphQL(router, svc)

	token := controllerTestToken(t, 101)
	create := performGraphQLRequest(t, router, token,
		`mutation { createOrder(customerId: 101, productId: 501, quantity: 2) { id customerId productId quantity totalAmount status } }`)
	for _, field := range []string{`"createOrder"`, `"id":1`, `"totalAmount":25`, `"status":"CREATED"`} {
		if !strings.Contains(create.Body.String(), field) {
			t.Errorf("create response missing %q: %s", field, create.Body.String())
		}
	}

	query := performGraphQLRequest(t, router, token,
		`query { order(id: 1) { id status } orders(limit: 10, offset: 0) { id status } }`)
	if !strings.Contains(query.Body.String(), `"order"`) || !strings.Contains(query.Body.String(), `"orders"`) {
		t.Fatalf("query response omitted order fields: %s", query.Body.String())
	}

	update := performGraphQLRequest(t, router, token,
		`mutation { updateOrderStatus(id: 1, status: "CONFIRMED") { id status } }`)
	if !strings.Contains(update.Body.String(), `"status":"CONFIRMED"`) {
		t.Fatalf("update response did not contain new status: %s", update.Body.String())
	}
}

func TestGraphQLRejectsUnauthenticatedRequestAndEnforcesCustomerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(authn.Middleware(controllerTestSecret))
	svc := service.NewOrderService(repository.NewMemoryRepository(), graphqlProductClient{})
	RegisterGraphQL(router, svc)

	unauthenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString(`{"query":"{ orders { id } }"}`))
	router.ServeHTTP(unauthenticated, request)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GraphQL request returned %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	create := performGraphQLRequest(t, router, controllerTestToken(t, 101),
		`mutation { createOrder(customerId: 101, productId: 501, quantity: 1) { id } }`)
	if create.Code != http.StatusOK {
		t.Fatalf("order creation returned %d: %s", create.Code, create.Body.String())
	}

	otherCustomer := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString(`{"query":"{ order(id: 1) { id } }"}`))
	request.Header.Set("Authorization", controllerTestToken(t, 202))
	router.ServeHTTP(otherCustomer, request)
	if otherCustomer.Code != http.StatusBadRequest || !strings.Contains(otherCustomer.Body.String(), "order not found") {
		t.Fatalf("cross-customer query returned %d: %s", otherCustomer.Code, otherCustomer.Body.String())
	}
}

func performGraphQLRequest(t *testing.T, router http.Handler, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString(`{"query":`+quoteJSONString(query)+`}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GraphQL request returned %d: %s", response.Code, response.Body.String())
	}
	return response
}

func quoteJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

type graphqlProductClient struct{}

func (graphqlProductClient) Price(_ context.Context, _ int64) (float64, error) {
	return 12.5, nil
}
