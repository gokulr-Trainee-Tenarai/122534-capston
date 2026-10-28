package repository

import (
	"context"
	"testing"

	"order-management/model"
)

func TestMemoryRepositoryScopesOrderAccessByCustomer(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	created, err := repo.Create(ctx, model.Order{
		CustomerID: 101,
		ProductID:  501,
		Quantity:   2,
		Status:     model.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}

	owner := int64(101)
	otherCustomer := int64(202)
	if _, err := repo.Get(ctx, created.ID, &owner); err != nil {
		t.Fatalf("owner could not fetch order: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID, &otherCustomer); err != ErrNotFound {
		t.Fatalf("other customer fetch error = %v, want ErrNotFound", err)
	}
}

func TestMemoryRepositoryScopesListAndStatusUpdates(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	first, err := repo.Create(ctx, model.Order{CustomerID: 101, Status: model.StatusCreated})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, model.Order{CustomerID: 202, Status: model.StatusCreated})
	if err != nil {
		t.Fatal(err)
	}

	owner := int64(101)
	orders, err := repo.List(ctx, 10, 0, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ID != first.ID {
		t.Fatalf("owner-scoped list = %+v, want only order %d", orders, first.ID)
	}

	updated, err := repo.UpdateStatus(ctx, first.ID, model.StatusConfirmed, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != model.StatusConfirmed {
		t.Fatalf("updated status = %q, want %q", updated.Status, model.StatusConfirmed)
	}

	otherCustomer := int64(202)
	if _, err := repo.UpdateStatus(ctx, first.ID, model.StatusCancelled, &otherCustomer); err != ErrNotFound {
		t.Fatalf("unauthorized update error = %v, want ErrNotFound", err)
	}
}
