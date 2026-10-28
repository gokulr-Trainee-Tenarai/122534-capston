package tests

import (
	"context"
	"errors"
	"testing"

	"order-management/authn"
	"order-management/model"
	"order-management/repository"
	"order-management/service"
)

type fakeProduct struct {
	price float64
	err   error
}

func (f fakeProduct) Price(context.Context, int64) (float64, error) { return f.price, f.err }
func TestCreateOrder(t *testing.T) {
	repo := repository.NewMemoryRepository()
	svc := service.NewOrderService(repo, fakeProduct{price: 12.5})
	principal := authn.Principal{UserID: 1, Username: "customer", Role: model.RoleCustomer, CustomerID: 101}
	o, err := svc.Create(context.Background(), model.CreateOrderRequest{CustomerID: 101, ProductID: 501, Quantity: 2}, principal)
	if err != nil {
		t.Fatal(err)
	}
	if o.TotalAmount != 25 || o.Status != model.StatusCreated {
		t.Fatalf("unexpected order: %+v", o)
	}
}
func TestCreateRejectsInvalidInput(t *testing.T) {
	svc := service.NewOrderService(repository.NewMemoryRepository(), fakeProduct{price: 1})
	principal := authn.Principal{UserID: 1, Username: "customer", Role: model.RoleCustomer, CustomerID: 101}
	_, err := svc.Create(context.Background(), model.CreateOrderRequest{CustomerID: 0, ProductID: 1, Quantity: 1}, principal)
	if !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}
func TestUpdateStatus(t *testing.T) {
	repo := repository.NewMemoryRepository()
	o, _ := repo.Create(context.Background(), model.Order{CustomerID: 1, ProductID: 1, Quantity: 1, Status: model.StatusCreated})
	svc := service.NewOrderService(repo, fakeProduct{price: 1})
	principal := authn.Principal{UserID: 1, Username: "customer", Role: model.RoleCustomer, CustomerID: 1}
	updated, err := svc.UpdateStatus(context.Background(), o.ID, model.StatusConfirmed, principal)
	if err != nil || updated.Status != model.StatusConfirmed {
		t.Fatalf("update failed: %+v %v", updated, err)
	}
}

func TestCustomerCannotCreateOrAccessAnotherCustomersOrder(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRepository()
	svc := service.NewOrderService(repo, fakeProduct{price: 1})
	principal := authn.Principal{UserID: 1, Username: "customer", Role: model.RoleCustomer, CustomerID: 101}

	_, err := svc.Create(ctx, model.CreateOrderRequest{CustomerID: 202, ProductID: 1, Quantity: 1}, principal)
	if !errors.Is(err, authn.ErrForbidden) {
		t.Fatalf("expected forbidden error when creating another customer's order, got %v", err)
	}

	foreignOrder, err := repo.Create(ctx, model.Order{CustomerID: 202, ProductID: 1, Quantity: 1, Status: model.StatusCreated})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, foreignOrder.ID, principal); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected foreign order to be hidden, got %v", err)
	}
}
