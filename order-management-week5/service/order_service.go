package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"order-management/authn"
	"order-management/consul"
	"order-management/model"
	"order-management/observability"
	"order-management/repository"

	"github.com/sony/gobreaker"
)

var ErrInvalid = errors.New("invalid input")
var ErrProductUnavailable = errors.New("product service unavailable")

const (
	defaultProductTimeout = 2 * time.Second
	maxProductRetries     = 5
	initialRetryBackoff   = 100 * time.Millisecond
	circuitFailureLimit   = 5
	circuitResetTimeout   = 15 * time.Second
)

var errCallerCanceled = errors.New("product request canceled by caller")

type ProductClient interface {
	Price(context.Context, int64) (float64, error)
}

type ProductServiceResolver interface {
	ResolveProductService(context.Context) (string, error)
}

type staticProductResolver string

func (r staticProductResolver) ResolveProductService(context.Context) (string, error) {
	if strings.TrimSpace(string(r)) == "" {
		return "", fmt.Errorf("PRODUCT_SERVICE_URL is not configured")
	}
	return strings.TrimRight(string(r), "/"), nil
}

type consulProductResolver struct {
	client  *consul.Client
	service string
}

func (r consulProductResolver) ResolveProductService(ctx context.Context) (string, error) {
	return r.client.Next(ctx, r.service)
}

type HTTPProductClient struct {
	resolver ProductServiceResolver
	client   *http.Client
	timeout  time.Duration
	retries  int
	breaker  *gobreaker.CircuitBreaker
}

func NewHTTPProductClient(url string, timeout time.Duration, retries int) *HTTPProductClient {
	return NewResolvingHTTPProductClient(staticProductResolver(url), timeout, retries)
}

func NewConsulProductClient(client *consul.Client, serviceName string, timeout time.Duration, retries int) *HTTPProductClient {
	return NewResolvingHTTPProductClient(consulProductResolver{client: client, service: serviceName}, timeout, retries)
}

func NewResolvingHTTPProductClient(resolver ProductServiceResolver, timeout time.Duration, retries int) *HTTPProductClient {
	if timeout <= 0 {
		timeout = defaultProductTimeout
	}
	if retries < 0 {
		retries = 0
	}
	if retries > maxProductRetries {
		retries = maxProductRetries
	}

	return &HTTPProductClient{
		resolver: resolver,
		client:   &http.Client{Timeout: timeout},
		timeout:  timeout,
		retries:  retries,
		breaker: gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        "product-service",
			MaxRequests: 1,
			Interval:    30 * time.Second,
			Timeout:     circuitResetTimeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= circuitFailureLimit
			},
			IsSuccessful: func(err error) bool {
				return err == nil || errors.Is(err, errCallerCanceled)
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				log.Printf("event=circuit_breaker_state name=%q from=%q to=%q", name, from, to)
			},
		}),
	}
}

func (p *HTTPProductClient) Price(ctx context.Context, id int64) (float64, error) {
	if p.resolver == nil {
		return 0, fmt.Errorf("%w: product service resolver is not configured", ErrProductUnavailable)
	}
	v, err := p.breaker.Execute(func() (interface{}, error) {
		observability.Trace(ctx, "product_service", "get_price")
		for attempt := 0; attempt <= p.retries; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("%w: %w", errCallerCanceled, err)
			}
			baseURL, err := p.resolver.ResolveProductService(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("%w: %w", errCallerCanceled, ctx.Err())
				}
				if attempt >= p.retries {
					return nil, fmt.Errorf("resolve product service: %w", err)
				}
				if err := p.waitBeforeRetry(ctx, attempt, err); err != nil {
					return nil, err
				}
				continue
			}
			price, err := p.fetchPrice(ctx, baseURL, id)
			if err == nil {
				return price, nil
			}
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%w: %w", errCallerCanceled, ctx.Err())
			}
			if attempt >= p.retries || !isRetryableProductError(err) {
				return nil, err
			}

			if err := p.waitBeforeRetry(ctx, attempt, err); err != nil {
				return nil, err
			}
		}
		return nil, fmt.Errorf("product retry loop ended unexpectedly")
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrProductUnavailable, err)
	}
	price, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("%w: product service returned an invalid result", ErrProductUnavailable)
	}
	return price, nil
}

func (p *HTTPProductClient) waitBeforeRetry(ctx context.Context, attempt int, lastErr error) error {
	backoff := initialRetryBackoff << attempt
	log.Printf("event=product_service_retry request_id=%q attempt=%d max_retries=%d backoff=%s error=%q",
		observability.RequestID(ctx), attempt+1, p.retries, backoff, lastErr)
	timer := time.NewTimer(backoff)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return fmt.Errorf("%w: %w", errCallerCanceled, ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (p *HTTPProductClient) fetchPrice(ctx context.Context, baseURL string, id int64) (float64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/products/%d", strings.TrimRight(baseURL, "/"), id)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create product request: %w", err)
	}
	if bearerToken := authn.BearerTokenFromContext(ctx); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request product price: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, productStatusError{statusCode: resp.StatusCode}
	}

	var body struct {
		Price     float64 `json:"price"`
		UnitPrice float64 `json:"unitPrice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode product price: %w", err)
	}
	price := body.Price
	if price == 0 {
		price = body.UnitPrice
	}
	if price <= 0 {
		return 0, fmt.Errorf("product service returned invalid price")
	}
	return price, nil
}

type productStatusError struct {
	statusCode int
}

func (e productStatusError) Error() string {
	return fmt.Sprintf("product service returned status %d", e.statusCode)
}

func isRetryableProductError(err error) bool {
	var statusErr productStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode == http.StatusTooManyRequests || statusErr.statusCode >= http.StatusInternalServerError
	}
	return true
}

type OrderService struct {
	repo     repository.OrderRepository
	products ProductClient
}

func NewOrderService(r repository.OrderRepository, p ProductClient) *OrderService {
	return &OrderService{repo: r, products: p}
}

func (s *OrderService) Create(ctx context.Context, req model.CreateOrderRequest, principal authn.Principal) (model.Order, error) {
	observability.Trace(ctx, "service", "create_order")
	if req.CustomerID <= 0 || req.ProductID <= 0 || req.Quantity < 1 || req.Quantity > 10000 {
		return model.Order{}, ErrInvalid
	}
	if principal.Role != model.RoleAdmin && principal.CustomerID != req.CustomerID {
		return model.Order{}, authn.ErrForbidden
	}
	price, err := s.products.Price(ctx, req.ProductID)
	if err != nil {
		return model.Order{}, err
	}
	return s.repo.Create(ctx, model.Order{CustomerID: req.CustomerID, ProductID: req.ProductID, Quantity: req.Quantity, UnitPrice: price, TotalAmount: price * float64(req.Quantity), Status: model.StatusCreated})
}
func (s *OrderService) Get(ctx context.Context, id int64, principal authn.Principal) (model.Order, error) {
	observability.Trace(ctx, "service", "get_order")
	if id <= 0 {
		return model.Order{}, ErrInvalid
	}
	return s.repo.Get(ctx, id, authn.CustomerScope(principal))
}
func (s *OrderService) List(ctx context.Context, limit, offset int, principal authn.Principal) ([]model.Order, error) {
	observability.Trace(ctx, "service", "list_orders")
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(ctx, limit, offset, authn.CustomerScope(principal))
}
func (s *OrderService) UpdateStatus(ctx context.Context, id int64, status model.OrderStatus, principal authn.Principal) (model.Order, error) {
	observability.Trace(ctx, "service", "update_order_status")
	if id <= 0 {
		return model.Order{}, ErrInvalid
	}
	if status != model.StatusCreated && status != model.StatusConfirmed && status != model.StatusCancelled {
		return model.Order{}, ErrInvalid
	}
	return s.repo.UpdateStatus(ctx, id, status, authn.CustomerScope(principal))
}
