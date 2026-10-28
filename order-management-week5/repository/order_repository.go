package repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"order-management/model"
	"order-management/observability"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("order not found")

type OrderRepository interface {
	Create(context.Context, model.Order) (model.Order, error)
	Get(context.Context, int64, *int64) (model.Order, error)
	List(context.Context, int, int, *int64) ([]model.Order, error)
	UpdateStatus(context.Context, int64, model.OrderStatus, *int64) (model.Order, error)
	Ping(context.Context) error
}

type GormOrderRepository struct {
	db *gorm.DB
}

func NewGormOrderRepository(db *gorm.DB) *GormOrderRepository {
	return &GormOrderRepository{db: db}
}

func (r *GormOrderRepository) Ping(ctx context.Context) error {
	observability.Trace(ctx, "repository", "ping")
	observability.Trace(ctx, "postgres", "ping")
	sqlDB, err := r.db.DB()
	if err == nil {
		err = sqlDB.PingContext(ctx)
	}
	if err != nil {
		observability.RecordDatabaseError()
	}
	return err
}

func (r *GormOrderRepository) Create(ctx context.Context, order model.Order) (model.Order, error) {
	observability.Trace(ctx, "repository", "create_order")
	observability.Trace(ctx, "postgres", "insert_order")
	if err := r.db.WithContext(ctx).Create(&order).Error; err != nil {
		observability.RecordDatabaseError()
		return model.Order{}, err
	}
	return order, nil
}

func (r *GormOrderRepository) Get(ctx context.Context, id int64, customerID *int64) (model.Order, error) {
	observability.Trace(ctx, "repository", "get_order")
	observability.Trace(ctx, "postgres", "select_order")
	query := r.db.WithContext(ctx)
	if customerID != nil {
		query = query.Where("customer_id = ?", *customerID)
	}
	var order model.Order
	err := query.First(&order, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return order, ErrNotFound
	}
	if err != nil {
		observability.RecordDatabaseError()
		return order, err
	}
	return order, nil
}

func (r *GormOrderRepository) List(ctx context.Context, limit, offset int, customerID *int64) ([]model.Order, error) {
	observability.Trace(ctx, "repository", "list_orders")
	observability.Trace(ctx, "postgres", "select_orders")
	query := r.db.WithContext(ctx)
	if customerID != nil {
		query = query.Where("customer_id = ?", *customerID)
	}
	var orders []model.Order
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&orders).Error; err != nil {
		observability.RecordDatabaseError()
		return nil, err
	}
	return orders, nil
}

func (r *GormOrderRepository) UpdateStatus(ctx context.Context, id int64, status model.OrderStatus, customerID *int64) (model.Order, error) {
	observability.Trace(ctx, "repository", "update_order_status")
	observability.Trace(ctx, "postgres", "update_order_status")
	var order model.Order
	query := r.db.WithContext(ctx).Model(&order).Where("id = ?", id)
	if customerID != nil {
		query = query.Where("customer_id = ?", *customerID)
	}
	result := query.Clauses(clause.Returning{}).Updates(map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		observability.RecordDatabaseError()
		return model.Order{}, result.Error
	}
	if result.RowsAffected == 0 {
		return model.Order{}, ErrNotFound
	}
	return order, nil
}

type MemoryRepository struct {
	mu     sync.RWMutex
	orders map[int64]model.Order
	nextID int64
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{orders: map[int64]model.Order{}, nextID: 1}
}

func (r *MemoryRepository) Ping(ctx context.Context) error {
	observability.Trace(ctx, "repository", "ping")
	return nil
}

func (r *MemoryRepository) Create(ctx context.Context, order model.Order) (model.Order, error) {
	observability.Trace(ctx, "repository", "create_order")
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	order.ID = r.nextID
	r.nextID++
	order.CreatedAt = now
	order.UpdatedAt = now
	r.orders[order.ID] = order
	return order, nil
}

func (r *MemoryRepository) Get(ctx context.Context, id int64, customerID *int64) (model.Order, error) {
	observability.Trace(ctx, "repository", "get_order")
	r.mu.RLock()
	defer r.mu.RUnlock()
	order, ok := r.orders[id]
	if !ok || (customerID != nil && order.CustomerID != *customerID) {
		return order, ErrNotFound
	}
	return order, nil
}

func (r *MemoryRepository) List(ctx context.Context, limit, offset int, customerID *int64) ([]model.Order, error) {
	observability.Trace(ctx, "repository", "list_orders")
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]int64, 0, len(r.orders))
	for id, order := range r.orders {
		if customerID == nil || order.CustomerID == *customerID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	out := make([]model.Order, 0, limit)
	for i := offset; i < len(ids) && len(out) < limit; i++ {
		out = append(out, r.orders[ids[i]])
	}
	return out, nil
}

func (r *MemoryRepository) UpdateStatus(ctx context.Context, id int64, status model.OrderStatus, customerID *int64) (model.Order, error) {
	observability.Trace(ctx, "repository", "update_order_status")
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[id]
	if !ok || (customerID != nil && order.CustomerID != *customerID) {
		return order, ErrNotFound
	}
	order.Status = status
	order.UpdatedAt = time.Now().UTC()
	r.orders[id] = order
	return order, nil
}
