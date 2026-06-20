package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "fanticbet/docs/swagger" // сгенерированная Swagger-спека (swag init)
	"fanticbet/internal/config"
	"fanticbet/internal/handler"
	"fanticbet/internal/handler/middleware"
	"fanticbet/internal/repository"
	"fanticbet/internal/security"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	swaggerfiles "github.com/swaggo/files"
	ginswagger "github.com/swaggo/gin-swagger"
)

// @title                       FanticBet API
// @version                     1.0
// @description                 REST API платформы ставок на фантики. На текущем этапе (M1) реализованы аутентификация и профиль пользователя.
// @BasePath                    /api/v1
// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
// @description                 Введите access-токен в формате: Bearer <access_token>

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to database
	pool, err := pgxpool.New(context.Background(), cfg.DBDSN)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Dependency injection: репозитории → сервисы → хендлеры.
	accessTTL := time.Duration(cfg.JWTTTLMinutes) * time.Minute
	refreshTTL := time.Duration(cfg.RefreshTTLDays) * 24 * time.Hour

	txMgr := repository.NewTxManager(pool)
	userRepo := repository.NewUserRepository(pool)
	refreshRepo := repository.NewRefreshTokenRepository(pool)
	walletRepo := repository.NewWalletRepository(pool)
	walletTxRepo := repository.NewWalletTransactionRepository(pool)

	jwtMgr, err := security.NewJWTManager(cfg.JWTSecret, accessTTL)
	if err != nil {
		log.Fatalf("Failed to init JWT manager: %v", err)
	}

	authSvc := service.NewAuthService(txMgr, userRepo, refreshRepo, walletRepo, walletTxRepo, jwtMgr, cfg.SignupBonus, accessTTL, refreshTTL)
	userSvc := service.NewUserService(userRepo, walletRepo, walletTxRepo)

	authH := handler.NewAuthHandler(authSvc, cfg.CookieSecure, cfg.CookieDomain, accessTTL, refreshTTL)
	userH := handler.NewUserHandler(userSvc)

	// Setup router
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(os.Getenv("GIN_MODE"))
	}

	r := gin.New()

	// Global middleware
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(cfg.CORSAllowedOrigins))

	// Health endpoint
	r.GET("/health", handler.Health(pool))

	// Swagger UI: интерактивная документация на /swagger/index.html (только dev).
	if gin.Mode() != gin.ReleaseMode {
		r.GET("/swagger/*any", ginswagger.WrapHandler(swaggerfiles.Handler))
	}

	// API v1
	v1 := r.Group("/api/v1")
	{
		v1.GET("/ping", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "pong"})
		})

		// Аутентификация (без AuthRequired).
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authH.Register)
			auth.POST("/login", authH.Login)
			auth.POST("/refresh", authH.Refresh)
			auth.POST("/logout", authH.Logout)
		}

		// Профиль текущего пользователя (за AuthRequired).
		me := v1.Group("/me", middleware.AuthRequired(jwtMgr))
		{
			me.GET("", userH.GetMe)
			me.PATCH("", userH.UpdateMe)
			me.GET("/transactions", userH.Transactions)
		}
	}

	// HTTP server with graceful shutdown
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.AppPort),
		Handler: r,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server starting on port %s", cfg.AppPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
