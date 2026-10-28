package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"order-management/authn"
	"order-management/model"
	"order-management/observability"
	"order-management/repository"
	"order-management/service"

	"github.com/gin-gonic/gin"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
)

// OrderController exposes REST endpoints for order operations.
type OrderController struct {
	service *service.OrderService
}

func NewOrderController(svc *service.OrderService) *OrderController {
	return &OrderController{service: svc}
}

func (c *OrderController) Register(r *gin.RouterGroup) {
	r.POST("/orders", c.Create)
	r.GET("/orders", c.List)
	r.GET("/orders/:id", c.Get)
	r.PUT("/orders/:id/status", c.UpdateStatus)
	r.PATCH("/orders/:id/status", c.UpdateStatus)
}

func (c *OrderController) Create(ctx *gin.Context) {
	observability.Trace(ctx.Request.Context(), "controller", "create_order")
	principal, ok := authn.FromGin(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	var req model.CreateOrderRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	order, err := c.service.Create(ctx.Request.Context(), req, principal)
	if err != nil {
		writeOrderError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, order)
}

func (c *OrderController) Get(ctx *gin.Context) {
	observability.Trace(ctx.Request.Context(), "controller", "get_order")
	principal, ok := authn.FromGin(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	id, err := parseID(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	order, err := c.service.Get(ctx.Request.Context(), id, principal)
	if err != nil {
		writeOrderError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, order)
}

func (c *OrderController) List(ctx *gin.Context) {
	observability.Trace(ctx.Request.Context(), "controller", "list_orders")
	principal, ok := authn.FromGin(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	limit, err := parseIntQuery(ctx, "limit", 20)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
		return
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	offset, err := parseIntQuery(ctx, "offset", 0)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
		return
	}
	if offset < 0 {
		offset = 0
	}

	orders, err := c.service.List(ctx.Request.Context(), limit, offset, principal)
	if err != nil {
		writeOrderError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, orders)
}

func (c *OrderController) UpdateStatus(ctx *gin.Context) {
	observability.Trace(ctx.Request.Context(), "controller", "update_order_status")
	principal, ok := authn.FromGin(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	id, err := parseID(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	var req model.UpdateStatusRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	order, err := c.service.UpdateStatus(ctx.Request.Context(), id, req.Status, principal)
	if err != nil {
		writeOrderError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, order)
}

func writeOrderError(ctx *gin.Context, err error) {
	_ = ctx.Error(err)
	switch {
	case errors.Is(err, authn.ErrForbidden):
		ctx.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, service.ErrInvalid):
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	case errors.Is(err, service.ErrProductUnavailable):
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "product service unavailable"})
	case errors.Is(err, repository.ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func parseID(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func parseIntQuery(ctx *gin.Context, key string, fallback int) (int, error) {
	value := ctx.Query(key)
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

// GraphQLRequest is the HTTP wrapper for GraphQL operations.
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func RegisterGraphQL(r gin.IRoutes, svc *service.OrderService) {
	orderType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Order",
		Fields: graphql.Fields{
			"id":          &graphql.Field{Type: graphql.Int},
			"customerId":  &graphql.Field{Type: graphql.Int},
			"productId":   &graphql.Field{Type: graphql.Int},
			"quantity":    &graphql.Field{Type: graphql.Int},
			"unitPrice":   &graphql.Field{Type: graphql.Float},
			"totalAmount": &graphql.Field{Type: graphql.Float},
			"status":      &graphql.Field{Type: graphql.String},
			"createdAt":   &graphql.Field{Type: graphql.String},
			"updatedAt":   &graphql.Field{Type: graphql.String},
		},
	})

	orderArgs := graphql.FieldConfigArgument{
		"id": &graphql.ArgumentConfig{Type: graphql.Int},
	}
	listArgs := graphql.FieldConfigArgument{
		"limit":  &graphql.ArgumentConfig{Type: graphql.Int},
		"offset": &graphql.ArgumentConfig{Type: graphql.Int},
	}
	createArgs := graphql.FieldConfigArgument{
		"customerId": &graphql.ArgumentConfig{Type: graphql.Int},
		"productId":  &graphql.ArgumentConfig{Type: graphql.Int},
		"quantity":   &graphql.ArgumentConfig{Type: graphql.Int},
	}
	updateArgs := graphql.FieldConfigArgument{
		"id":     &graphql.ArgumentConfig{Type: graphql.Int},
		"status": &graphql.ArgumentConfig{Type: graphql.String},
	}

	rootQuery := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"order": &graphql.Field{Type: orderType, Args: orderArgs, Resolve: func(params graphql.ResolveParams) (interface{}, error) {
				principal, ok := authn.FromContext(params.Context)
				if !ok {
					return nil, authn.ErrForbidden
				}
				id, ok := params.Args["id"].(int)
				if !ok || id <= 0 {
					return nil, service.ErrInvalid
				}
				return svc.Get(params.Context, int64(id), principal)
			}},
			"orders": &graphql.Field{Type: graphql.NewList(orderType), Args: listArgs, Resolve: func(params graphql.ResolveParams) (interface{}, error) {
				principal, ok := authn.FromContext(params.Context)
				if !ok {
					return nil, authn.ErrForbidden
				}
				limit := 20
				if v, ok := params.Args["limit"].(int); ok && v > 0 {
					limit = v
				}
				offset := 0
				if v, ok := params.Args["offset"].(int); ok && v >= 0 {
					offset = v
				}
				return svc.List(params.Context, limit, offset, principal)
			}},
		},
	})

	rootMutation := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"createOrder": &graphql.Field{Type: orderType, Args: createArgs, Resolve: func(params graphql.ResolveParams) (interface{}, error) {
				principal, ok := authn.FromContext(params.Context)
				if !ok {
					return nil, authn.ErrForbidden
				}
				customerID, _ := params.Args["customerId"].(int)
				productID, _ := params.Args["productId"].(int)
				quantity, _ := params.Args["quantity"].(int)
				return svc.Create(params.Context, model.CreateOrderRequest{CustomerID: int64(customerID), ProductID: int64(productID), Quantity: quantity}, principal)
			}},
			"updateOrderStatus": &graphql.Field{Type: orderType, Args: updateArgs, Resolve: func(params graphql.ResolveParams) (interface{}, error) {
				principal, ok := authn.FromContext(params.Context)
				if !ok {
					return nil, authn.ErrForbidden
				}
				id, _ := params.Args["id"].(int)
				status, _ := params.Args["status"].(string)
				return svc.UpdateStatus(params.Context, int64(id), model.OrderStatus(status), principal)
			}},
		},
	})

	schema, err := graphql.NewSchema(graphql.SchemaConfig{Query: rootQuery, Mutation: rootMutation})
	if err != nil {
		panic(fmt.Sprintf("graphql schema error: %v", err))
	}

	r.POST("/graphql", func(c *gin.Context) {
		observability.Trace(c.Request.Context(), "controller", "graphql")
		var req GraphQLRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": "invalid graphql request"}}})
			return
		}
		result := graphql.Do(graphql.Params{
			Schema:         schema,
			Context:        c.Request.Context(),
			RequestString:  req.Query,
			VariableValues: req.Variables,
		})
		if len(result.Errors) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": formatGraphQLErrors(result.Errors)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": result.Data})
	})
}

func formatGraphQLErrors(errs []gqlerrors.FormattedError) []map[string]string {
	out := make([]map[string]string, 0, len(errs))
	for _, err := range errs {
		out = append(out, map[string]string{"message": err.Message})
	}
	return out
}

func orderStatusString(value model.OrderStatus) string {
	return string(value)
}

func marshalOrder(order model.Order) map[string]interface{} {
	return map[string]interface{}{
		"id":          order.ID,
		"customerId":  order.CustomerID,
		"productId":   order.ProductID,
		"quantity":    order.Quantity,
		"unitPrice":   order.UnitPrice,
		"totalAmount": order.TotalAmount,
		"status":      orderStatusString(order.Status),
		"createdAt":   order.CreatedAt.Format(time.RFC3339),
		"updatedAt":   order.UpdatedAt.Format(time.RFC3339),
	}
}

func decodeGraphQLVariables(raw interface{}) map[string]interface{} {
	if raw == nil {
		return nil
	}
	if payload, ok := raw.(map[string]interface{}); ok {
		return payload
	}
	if payload, ok := raw.(string); ok && payload != "" {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &out); err == nil {
			return out
		}
	}
	return nil
}
