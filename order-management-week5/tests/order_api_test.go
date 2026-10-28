package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"order-management/authn"
	"order-management/controller"
	"order-management/model"
	"order-management/repository"
	"order-management/service"

	"github.com/gin-gonic/gin"
)

var testJWTSecret = []byte("integration-test-jwt-secret-at-least-32-bytes")

func testBearerToken(t *testing.T, role model.UserRole, customerID int64) string {
	t.Helper()
	token, err := authn.IssueToken(testJWTSecret, model.User{
		ID:         1,
		Username:   "test-user",
		Role:       role,
		CustomerID: customerID,
	}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

func TestOrderAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMemoryRepository()
	svc := service.NewOrderService(repo, fakeProduct{price: 12.5})
	router := gin.New()
	router.Use(authn.Middleware(testJWTSecret))
	api := router.Group("/api/v1")
	controller.NewOrderController(svc).Register(api)

	token := testBearerToken(t, model.RoleCustomer, 101)
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(
		`{"customerId":101,"productId":501,"quantity":2}`,
	))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", token)
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", createResponse.Code, createResponse.Body.String())
	}

	var created model.Order
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}

	getResponse := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+strconv.FormatInt(created.ID, 10), nil)
	getRequest.Header.Set("Authorization", token)
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get returned %d: %s", getResponse.Code, getResponse.Body.String())
	}
	var fetched model.Order
	if err := json.Unmarshal(getResponse.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode fetched order: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected fetched order ID %d, got %d", created.ID, fetched.ID)
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	listRequest.Header.Set("Authorization", token)
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed []model.Order
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode order list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("expected list to contain order ID %d, got %+v", created.ID, listed)
	}

	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/orders/"+strconv.FormatInt(created.ID, 10)+"/status",
		bytes.NewBufferString(`{"status":"CONFIRMED"}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Authorization", token)
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT status update returned %d: %s", updateResponse.Code, updateResponse.Body.String())
	}

	var updated model.Order
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated order: %v", err)
	}
	if updated.Status != model.StatusConfirmed {
		t.Fatalf("expected status %q, got %q", model.StatusConfirmed, updated.Status)
	}
}

func TestJWTProtectsRESTAndGraphQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(authn.Middleware(testJWTSecret))
	api := router.Group("/api/v1")
	graphqlAPI := router.Group("")

	svc := service.NewOrderService(repository.NewMemoryRepository(), fakeProduct{price: 12.5})
	controller.NewOrderController(svc).Register(api)
	controller.RegisterGraphQL(graphqlAPI, svc)

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/orders"},
		{method: http.MethodPost, path: "/graphql"},
	} {
		request := httptest.NewRequest(route.method, route.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated request to %s returned %d, want %d", route.path, response.Code, http.StatusUnauthorized)
		}
	}
}

func TestCreateOrdersConcurrently(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(authn.Middleware(testJWTSecret))
	api := router.Group("/api/v1")
	controller.NewOrderController(service.NewOrderService(
		repository.NewMemoryRepository(),
		fakeProduct{price: 12.5},
	)).Register(api)
	token := testBearerToken(t, model.RoleAdmin, 0)

	type result struct {
		orderID int64
		err     string
	}
	const requestCount = 100
	results := make(chan result, requestCount)
	var workers sync.WaitGroup
	workers.Add(requestCount)

	for i := 0; i < requestCount; i++ {
		go func(customerID int) {
			defer workers.Done()
			body := bytes.NewBufferString(
				`{"customerId":` + strconv.Itoa(customerID+1) + `,"productId":501,"quantity":2}`,
			)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", body)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				results <- result{err: "create returned " + strconv.Itoa(response.Code) + ": " + response.Body.String()}
				return
			}

			var created model.Order
			if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
				results <- result{err: "decode order: " + err.Error()}
				return
			}
			results <- result{orderID: created.ID}
		}(i)
	}

	workers.Wait()
	close(results)

	seenIDs := make(map[int64]struct{}, requestCount)
	for result := range results {
		if result.err != "" {
			t.Error(result.err)
			continue
		}
		if _, exists := seenIDs[result.orderID]; exists {
			t.Errorf("duplicate order ID returned: %d", result.orderID)
		}
		seenIDs[result.orderID] = struct{}{}
	}
	if len(seenIDs) != requestCount {
		t.Fatalf("expected %d unique created orders, got %d", requestCount, len(seenIDs))
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/orders?limit=100", nil)
	listRequest.Header.Set("Authorization", token)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list returned %d after concurrent creates: %s", listResponse.Code, listResponse.Body.String())
	}
	var orders []model.Order
	if err := json.Unmarshal(listResponse.Body.Bytes(), &orders); err != nil {
		t.Fatalf("decode concurrent order list: %v", err)
	}
	if len(orders) != requestCount {
		t.Fatalf("expected %d persisted orders, got %d", requestCount, len(orders))
	}
}
