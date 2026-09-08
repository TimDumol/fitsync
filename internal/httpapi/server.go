// Package httpapi contains the initial fitnessd HTTP surface.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/timdumol/fitsync/internal/config"
)

// NewServer builds a server with explicit network timeouts and request-ID
// logging. Application handlers are intentionally limited to healthz here;
// authenticated APIs are added in later plan steps.
func NewServer(cfg config.Config, logger *slog.Logger, apis ...ServerInterface) *http.Server {
	return newServer(cfg, logger, firstAPI(apis), nil, nil, "")
}

// NewServerWithIngest includes the token-authenticated Garmin endpoint.
func NewServerWithIngest(cfg config.Config, logger *slog.Logger, api ServerInterface, ingestion http.Handler) *http.Server {
	return newServer(cfg, logger, api, ingestion, nil, "")
}

// NewServerWithAuth adds the authenticated MCP endpoint and protected-resource
// metadata. The MCP handler is already wrapped by its OIDC middleware.
func NewServerWithAuth(cfg config.Config, logger *slog.Logger, api ServerInterface, ingestion, mcpHandler http.Handler, issuer string) *http.Server {
	return newServer(cfg, logger, api, ingestion, mcpHandler, issuer)
}

type mobileRouteProvider interface {
	MobileBatchHandler() http.Handler
	MobileStatusHandler() http.Handler
}

func newServer(cfg config.Config, logger *slog.Logger, api ServerInterface, ingestion, mcpHandler http.Handler, issuer string) *http.Server {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	if issuer != "" {
		mux.HandleFunc("GET /.well-known/oauth-protected-resource", protectedResourceMetadata(cfg.Resource, issuer))
	}
	if mcpHandler != nil {
		mux.Handle("/mcp", mcpHandler)
	}
	if api != nil {
		mux = muxWithAPI(mux, api)
	}
	if ingestion != nil {
		mux.Handle("POST /api/v1/ingest/garmin/batch", ingestion)
		if mobile, ok := ingestion.(mobileRouteProvider); ok {
			mux.Handle("POST /api/v2/athletes/{athlete_id}/ingest/garmin/batch", mobile.MobileBatchHandler())
			mux.Handle("GET /api/v2/athletes/{athlete_id}/garmin/sync-status", mobile.MobileStatusHandler())
		}
	}
	h := requestLogging(logger, mux)
	return &http.Server{
		Addr:              cfg.Address,
		Handler:           h,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadBodyTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func protectedResourceMetadata(resource, issuer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":              resource,
			"authorization_servers": []string{issuer},
			"authorization_server":  issuer,
			"scopes_supported":      []string{"fitness:read", "fitness:write"},
		})
	}
}

func firstAPI(apis []ServerInterface) ServerInterface {
	if len(apis) == 0 {
		return nil
	}
	return apis[0]
}

func muxWithAPI(mux *http.ServeMux, api ServerInterface) *http.ServeMux {
	apiMux := http.NewServeMux()
	HandlerWithOptions(api, StdHTTPServerOptions{
		BaseRouter: apiMux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "invalid request parameters")
		},
	})
	if productionAPI, ok := api.(*API); ok {
		mux.Handle("/api/", productionAPI.AuthMiddleware(apiMux))
	} else {
		mux.Handle("/api/", apiMux)
	}
	return mux
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if len(requestID) == 0 || len(requestID) > 128 {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

// Shutdown stops the server within the configured deadline.
func Shutdown(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return server.Shutdown(ctx)
}
