package authservice

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"order-management/authn"
	"order-management/model"
	"order-management/observability"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const accessTokenLifetime = time.Hour

type Handler struct {
	db     *gorm.DB
	secret []byte
}

func NewHandler(db *gorm.DB, secret []byte) *Handler {
	return &Handler{db: db, secret: secret}
}

func (h *Handler) RegisterRoutes(router *gin.Engine) {
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "auth-service", "status": "live"})
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
	router.POST("/auth/login", h.Login)
	admin := router.Group("/auth", authn.Middleware(h.secret), authn.RequireAdmin())
	admin.POST("/users", h.CreateCustomer)
}

func (h *Handler) Login(c *gin.Context) {
	var request struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	var user model.User
	err := h.db.WithContext(c.Request.Context()).Where("username = ?", strings.TrimSpace(request.Username)).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err != nil {
		observability.RecordDatabaseError()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authentication service unavailable"})
		return
	}

	token, err := authn.IssueToken(h.secret, user, accessTokenLifetime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue access token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"accessToken": token,
		"tokenType":   "Bearer",
		"expiresIn":   int(accessTokenLifetime.Seconds()),
	})
}

func (h *Handler) CreateCustomer(c *gin.Context) {
	var request struct {
		Username   string `json:"username" binding:"required"`
		Password   string `json:"password" binding:"required"`
		CustomerID int64  `json:"customerId" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username, password, and positive customerId are required"})
		return
	}
	username := strings.TrimSpace(request.Username)
	if len(username) < 3 || len(username) > 64 || len(request.Password) < 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 3-64 characters and password at least 12 characters"})
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user"})
		return
	}

	user := model.User{
		Username:     username,
		PasswordHash: string(passwordHash),
		Role:         model.RoleCustomer,
		CustomerID:   request.CustomerID,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		observability.RecordDatabaseError()
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"customerId": user.CustomerID,
	})
}

func BootstrapAdmin(db *gorm.DB, username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || len(password) < 12 {
		return errors.New("BOOTSTRAP_ADMIN_USERNAME must be 3-64 characters and BOOTSTRAP_ADMIN_PASSWORD at least 12 characters")
	}
	var existing model.User
	err := db.Where("username = ?", username).First(&existing).Error
	if err == nil {
		if existing.Role != model.RoleAdmin {
			return errors.New("bootstrap username is already assigned to a non-admin user")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := model.User{
		Username:     username,
		PasswordHash: string(passwordHash),
		Role:         model.RoleAdmin,
	}
	return db.Create(&admin).Error
}
