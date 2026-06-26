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
	"fanticbet/internal/oddsapi"
	"fanticbet/internal/repository"
	"fanticbet/internal/security"
	"fanticbet/internal/service"
	"fanticbet/internal/worker"

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
	authIdentityRepo := repository.NewAuthIdentityRepository(pool)
	eventRepo := repository.NewEventRepository(pool)
	marketRepo := repository.NewMarketRepository(pool)
	outcomeRepo := repository.NewOutcomeRepository(pool)
	betRepo := repository.NewBetRepository(pool)
	leagueRepo := repository.NewLeagueRepository(pool)

	jwtMgr, err := security.NewJWTManager(cfg.JWTSecret, accessTTL)
	if err != nil {
		log.Fatalf("Failed to init JWT manager: %v", err)
	}

	authSvc := service.NewAuthService(txMgr, userRepo, refreshRepo, walletRepo, walletTxRepo, jwtMgr, cfg.SignupBonus, accessTTL, refreshTTL)
	userSvc := service.NewUserService(userRepo, walletRepo, walletTxRepo)
	eventSvc := service.NewEventService(eventRepo, marketRepo, outcomeRepo, leagueRepo)
	bettingSvc := service.NewBettingService(txMgr, betRepo, outcomeRepo, marketRepo, eventRepo, walletRepo, walletTxRepo, cfg.BetMin, cfg.BetMax)
	// SettlementService — расчёт завершённых событий. Используется воркером (не
	// хендлером): тот же набор репозиториев, что у BettingService, плюс tx-менеджер.
	settlementSvc := service.NewSettlementService(txMgr, eventRepo, marketRepo, outcomeRepo, betRepo, walletRepo, walletTxRepo, nil)
	oauthSvc := service.NewOAuthService(txMgr, userRepo, walletRepo, walletTxRepo, authIdentityRepo, refreshRepo, jwtMgr, cfg.SignupBonus, accessTTL, refreshTTL)
	// StatsService — социальная часть (M5): публичный профиль со статистикой,
	// история ставок и лидерборд с in-memory кэшем. Порог числа ставок для
	// попадания в топ берётся из конфига LEADERBOARD_MIN_BETS.
	statsSvc := service.NewStatsService(userRepo, betRepo, repository.NewStatsRepository(pool), cfg.LeaderboardMinBets)
	// AdminService — админка (M6 + M8): создание/правка/отмена/расчёт кастомных
	// событий, управление чемпионатами (leagues) и ручная корректировка баланса.
	// Отмена и расчёт делегируются в SettlementService (идемпотентные выплаты).
	adminSvc := service.NewAdminService(txMgr, eventRepo, marketRepo, outcomeRepo, leagueRepo, walletRepo, walletTxRepo, settlementSvc, nil)

	yandexCfg, vkCfg := handler.NewOAuthConfigs(
		cfg.YandexClientID, cfg.YandexClientSecret, cfg.YandexRedirectURI,
		cfg.VKClientID, cfg.VKClientSecret, cfg.VKRedirectURI,
	)

	authH := handler.NewAuthHandler(authSvc, cfg.CookieSecure, cfg.CookieDomain, accessTTL, refreshTTL)
	userH := handler.NewUserHandler(userSvc)
	eventH := handler.NewEventHandler(eventSvc)
	betH := handler.NewBetHandler(bettingSvc)
	oauthH := handler.NewOAuthHandler(oauthSvc, yandexCfg, vkCfg, cfg.CookieSecure, cfg.CookieDomain, cfg.FrontendURL, accessTTL, refreshTTL)
	statsH := handler.NewStatsHandler(statsSvc)
	adminH := handler.NewAdminHandler(adminSvc)

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

			// OAuth: Яндекс и VK.
			auth.GET("/:provider/login", oauthH.Login)
			auth.GET("/:provider/callback", oauthH.Callback)
		}

		// Каталог событий (публичный, без авторизации): виды спорта, чемпионаты
		// и лента.
		v1.GET("/sports", eventH.Sports)
		v1.GET("/leagues", eventH.ListLeagues)
		v1.GET("/events", eventH.List)
		v1.GET("/events/:id", eventH.Get)

		// Социальная часть (публичная, без авторизации): профиль со статистикой,
		// история ставок любого пользователя и лидерборд (architecture.md:197).
		v1.GET("/users/:id", statsH.GetUser)
		v1.GET("/users/:id/bets", statsH.UserBets)
		v1.GET("/leaderboard", statsH.Leaderboard)

		// Размещение ставки (требует авторизации). Отдельный маршрут, а не под
		// /me, т.к. это действие над событием, а не над профилем.
		v1.POST("/bets", middleware.AuthRequired(jwtMgr), betH.Place)

		// Профиль текущего пользователя (за AuthRequired).
		me := v1.Group("/me", middleware.AuthRequired(jwtMgr))
		{
			me.GET("", userH.GetMe)
			me.PATCH("", userH.UpdateMe)
			me.GET("/transactions", userH.Transactions)
			me.GET("/bets", betH.List)
		}

		// Админка (M6 + M8): за AuthRequired + AdminRequired (роль admin). Создание,
		// правка/отмена, ручной расчёт кастомных событий, управление чемпионатами
		// (leagues) и корректировка баланса.
		admin := v1.Group("/admin", middleware.AuthRequired(jwtMgr), middleware.AdminRequired())
		{
			admin.POST("/events", adminH.CreateEvent)
			admin.PATCH("/events/:id", adminH.EditEvent)
			admin.POST("/events/:id/settle", adminH.SettleEvent)
			admin.POST("/events/:id/featured", adminH.SetFeatured)
			admin.POST("/users/:id/adjust", adminH.AdjustBalance)

			// Чемпионаты (лиги) — справочник для событий (M8).
			admin.GET("/leagues", adminH.ListLeagues)
			admin.POST("/leagues", adminH.CreateLeague)
			admin.PATCH("/leagues/:id", adminH.EditLeague)
			admin.DELETE("/leagues/:id", adminH.DeleteLeague)

			// Спортивные матчи (source='manual', M8): создание, правка/отмена,
			// ввод финального счёта → расчёт ML/TOTALS, ручной перевод статуса.
			admin.POST("/matches", adminH.CreateMatch)
			admin.PATCH("/matches/:id", adminH.EditMatch)
			admin.POST("/matches/:id/scores", adminH.SetMatchScores)
			admin.POST("/matches/:id/status", adminH.SetMatchStatus)
		}
	}

	// Фоновые воркеры синхронизации с Odds-API. Запускаем только при наличии
	// ключа: без него все запросы к API вернут 401, смысла крутить воркеры нет.
	var runner *worker.Runner
	if cfg.OddsAPIKey == "" {
		log.Println("ODDS_API_KEY is empty, skipping Odds-API workers")
	} else {
		oddsClient := oddsapi.New(cfg.OddsAPIKey)
		oddsWindow := time.Duration(cfg.OddsSyncWindowHours) * time.Hour

		eventSync := worker.NewEventSyncWorker(eventRepo, marketRepo, oddsClient, cfg.Sports, nil)
		oddsSync := worker.NewOddsSyncWorker(eventRepo, marketRepo, outcomeRepo, oddsClient, cfg.Bookmaker, oddsWindow, nil)
		settlement := worker.NewSettlementWorker(eventRepo, oddsClient, settlementSvc, nil)

		runner = worker.NewRunner(nil)
		if err := runner.Register(worker.Job{
			Name:     "event-sync",
			Schedule: cfg.EventSyncSchedule,
			Timeout:  2 * time.Minute,
			Run:      eventSync.Run,
		}); err != nil {
			log.Fatalf("Failed to register event-sync worker: %v", err)
		}
		if err := runner.Register(worker.Job{
			Name:     "odds-sync",
			Schedule: cfg.OddsSyncSchedule,
			Timeout:  2 * time.Minute,
			Run:      oddsSync.Run,
		}); err != nil {
			log.Fatalf("Failed to register odds-sync worker: %v", err)
		}
		if err := runner.Register(worker.Job{
			Name:     "settlement",
			Schedule: cfg.SettlementSchedule,
			Timeout:  2 * time.Minute,
			Run:      settlement.Run,
		}); err != nil {
			log.Fatalf("Failed to register settlement worker: %v", err)
		}
		runner.Start()
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

	// Останавливаем воркеры: прекращаем планирование и даём текущим итерациям
	// завершиться (или прерываем их по таймауту).
	if runner != nil {
		runner.Stop(10 * time.Second)
	}

	log.Println("Server exited")
}
