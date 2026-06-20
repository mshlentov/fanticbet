package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/security"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newJWT — менеджер для тестов с фиксированным секретом и 15-мин TTL.
func newJWT(t *testing.T) *security.JWTManager {
	t.Helper()
	jwt, err := security.NewJWTManager("test-secret", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return jwt
}

func TestAuthRequired(t *testing.T) {
	jwt := newJWT(t)
	validToken, err := jwt.Issue(42, domain.RoleUser, time.Now())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantUserID int64 // проверяем, только если 200
	}{
		{name: "valid", header: "Bearer " + validToken, wantStatus: http.StatusOK, wantUserID: 42},
		{name: "missing", header: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic " + validToken, wantStatus: http.StatusUnauthorized},
		{name: "no token", header: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "garbage token", header: "Bearer not.a.jwt", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedID int64
			var captured bool

			r := gin.New()
			r.GET("/protected", AuthRequired(jwt), func(c *gin.Context) {
				capturedID, captured = UserIDFromContext(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				if !captured || capturedID != tt.wantUserID {
					t.Errorf("user_id in context: got %d (ok=%v), want %d", capturedID, captured, tt.wantUserID)
				}
			}
		})
	}
}

func TestAdminRequired(t *testing.T) {
	jwt := newJWT(t)

	tests := []struct {
		name       string
		role       domain.Role
		wantStatus int
	}{
		{name: "admin allowed", role: domain.RoleAdmin, wantStatus: http.StatusOK},
		{name: "user forbidden", role: domain.RoleUser, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := jwt.Issue(1, tt.role, time.Now())
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}

			r := gin.New()
			r.GET("/admin", AuthRequired(jwt), AdminRequired(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// AdminRequired без AuthRequired (нет роли в контексте) → 401, а не паника.
func TestAdminRequired_WithoutAuth(t *testing.T) {
	r := gin.New()
	r.GET("/admin", AdminRequired(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
