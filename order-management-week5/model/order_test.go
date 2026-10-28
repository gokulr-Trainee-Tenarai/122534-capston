package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserJSONOmitsPasswordHash(t *testing.T) {
	user := User{
		ID:           1,
		Username:     "customer",
		PasswordHash: "must-not-be-returned",
		Role:         RoleCustomer,
		CustomerID:   101,
	}
	encoded, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), user.PasswordHash) || strings.Contains(string(encoded), "password") {
		t.Fatalf("serialized user leaked password data: %s", encoded)
	}
}

func TestOrderStatusesAndUserRoles(t *testing.T) {
	if StatusCreated != "CREATED" || StatusConfirmed != "CONFIRMED" || StatusCancelled != "CANCELLED" {
		t.Fatalf("unexpected order status constants: %q, %q, %q", StatusCreated, StatusConfirmed, StatusCancelled)
	}
	if RoleAdmin != "ADMIN" || RoleCustomer != "CUSTOMER" {
		t.Fatalf("unexpected user roles: %q, %q", RoleAdmin, RoleCustomer)
	}
}
