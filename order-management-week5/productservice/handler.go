package productservice

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"order-management/authn"
	"order-management/model"
	"order-management/observability"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	db     *gorm.DB
	secret []byte
}

func NewHandler(db *gorm.DB, secret []byte) *Handler {
	return &Handler{db: db, secret: secret}
}

func (h *Handler) RegisterRoutes(router *gin.Engine) {
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "product-service", "status": "live"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		sqlDB, err := h.db.DB()
		if err == nil {
			err = sqlDB.PingContext(c.Request.Context())
		}
		if err != nil {
			observability.RecordDatabaseError()
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.GET("/products/:id", authn.Middleware(h.secret), h.GetPrice)
	admin := router.Group("/products", authn.Middleware(h.secret), authn.RequireAdmin())
	admin.POST("", h.Create)
}

func (h *Handler) GetPrice(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}
	var product model.Product
	err = h.db.WithContext(c.Request.Context()).First(&product, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	if err != nil {
		observability.RecordDatabaseError()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "product service unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": product.ID, "name": product.Name, "price": product.Price})
}

func (h *Handler) Create(c *gin.Context) {
	var request struct {
		ID    int64   `json:"id" binding:"required,gt=0"`
		Name  string  `json:"name" binding:"required"`
		Price float64 `json:"price" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "positive id, name, and price are required"})
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-200 characters"})
		return
	}
	product := model.Product{ID: request.ID, Name: name, Price: request.Price}
	if err := h.db.WithContext(c.Request.Context()).Create(&product).Error; err != nil {
		observability.RecordDatabaseError()
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusConflict, gin.H{"error": "product id already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create product"})
		return
	}
	c.JSON(http.StatusCreated, product)
}

func BootstrapProduct(db *gorm.DB, id int64, name string, price float64) error {
	if id <= 0 && name == "" && price == 0 {
		return nil
	}
	if id <= 0 || price <= 0 || strings.TrimSpace(name) == "" {
		return errors.New("PRODUCT_BOOTSTRAP_ID, PRODUCT_BOOTSTRAP_NAME, and positive PRODUCT_BOOTSTRAP_PRICE must all be configured")
	}
	var existing model.Product
	err := db.First(&existing, id).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&model.Product{ID: id, Name: strings.TrimSpace(name), Price: price}).Error
}
