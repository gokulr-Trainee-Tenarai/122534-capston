package model

import "time"

type OrderStatus string

const (
	StatusCreated   OrderStatus = "CREATED"
	StatusConfirmed OrderStatus = "CONFIRMED"
	StatusCancelled OrderStatus = "CANCELLED"
)

type Order struct {
	ID          int64       `json:"id" gorm:"primaryKey"`
	CustomerID  int64       `json:"customerId" gorm:"not null;check:customer_id > 0;index:idx_orders_customer_id"`
	ProductID   int64       `json:"productId" gorm:"not null;check:product_id > 0;index:idx_orders_product_id"`
	Quantity    int         `json:"quantity" gorm:"not null;check:quantity BETWEEN 1 AND 10000"`
	UnitPrice   float64     `json:"unitPrice" gorm:"not null;type:numeric(12,2);check:unit_price > 0"`
	TotalAmount float64     `json:"totalAmount" gorm:"not null;type:numeric(14,2);check:total_amount > 0"`
	Status      OrderStatus `json:"status" gorm:"not null;check:status IN ('CREATED','CONFIRMED','CANCELLED');index:idx_orders_status"`
	CreatedAt   time.Time   `json:"createdAt" gorm:"not null;index:idx_orders_created_at,sort:desc"`
	UpdatedAt   time.Time   `json:"updatedAt" gorm:"not null"`
}

type UserRole string

const (
	RoleAdmin    UserRole = "ADMIN"
	RoleCustomer UserRole = "CUSTOMER"
)

type User struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"not null;uniqueIndex"`
	PasswordHash string    `json:"-" gorm:"not null"`
	Role         UserRole  `json:"role" gorm:"not null;check:role IN ('ADMIN','CUSTOMER')"`
	CustomerID   int64     `json:"customerId" gorm:"not null;default:0"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Product struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"not null"`
	Price     float64   `json:"price" gorm:"not null;type:numeric(12,2);check:price > 0"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type CreateOrderRequest struct {
	CustomerID int64 `json:"customerId"`
	ProductID  int64 `json:"productId"`
	Quantity   int   `json:"quantity"`
}
type UpdateStatusRequest struct {
	Status OrderStatus `json:"status"`
}
