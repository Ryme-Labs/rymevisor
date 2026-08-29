package gateway

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
)

type ServiceConfig struct {
	ControlPlaneURL string
	NetworkURL      string
	StorageURL      string
	SchedulerURL    string
}

type Gateway struct {
	router chi.Router
	config ServiceConfig
}

func NewGateway(cfg ServiceConfig) *Gateway {
	r := chi.NewRouter()
	g := &Gateway{router: r, config: cfg}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// WebSocket endpoints (same API key via header X-API-Key or ?api_key=)
	r.Get("/ws/logs", g.HandleLogs)
	r.Get("/ws/console", g.HandleConsole)
	r.Get("/api/v1/ws/logs", g.HandleLogs)
	r.Get("/api/v1/ws/console", g.HandleConsole)
	r.Get("/api/v1/ws/console/{id}", g.HandleConsole)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/vms", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Put("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Delete("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/power-on", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/power-off", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/reboot", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/resize", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/snapshot", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/clone", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/restore-snapshot", g.proxy(cfg.ControlPlaneURL))
		})

		r.Route("/nodes", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Put("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/drain", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/heartbeat", g.proxy(cfg.ControlPlaneURL))
		})

		r.Route("/networks", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.NetworkURL))
			r.Get("/", g.proxy(cfg.NetworkURL))
			r.Get("/{id}", g.proxy(cfg.NetworkURL))
			r.Delete("/{id}", g.proxy(cfg.NetworkURL))
			r.Post("/{id}/subnets", g.proxy(cfg.NetworkURL))
			r.Post("/{id}/firewall-rules", g.proxy(cfg.NetworkURL))
		})
		r.Delete("/subnets/{id}", g.proxy(cfg.NetworkURL))
		r.Delete("/firewall-rules/{id}", g.proxy(cfg.NetworkURL))
		r.Route("/floating-ips", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.NetworkURL))
			r.Get("/", g.proxy(cfg.NetworkURL))
			r.Delete("/{id}", g.proxy(cfg.NetworkURL))
		})

		r.Route("/storage", func(r chi.Router) {
			r.Route("/pools", func(r chi.Router) {
				r.Post("/", g.proxy(cfg.StorageURL))
				r.Get("/", g.proxy(cfg.StorageURL))
				r.Get("/{id}", g.proxy(cfg.StorageURL))
			})
			r.Route("/volumes", func(r chi.Router) {
				r.Post("/", g.proxy(cfg.StorageURL))
				r.Get("/", g.proxy(cfg.StorageURL))
				r.Get("/{id}", g.proxy(cfg.StorageURL))
				r.Delete("/{id}", g.proxy(cfg.StorageURL))
				r.Put("/{id}/resize", g.proxy(cfg.StorageURL))
				r.Post("/{id}/clone", g.proxy(cfg.StorageURL))
				r.Post("/{id}/snapshots", g.proxy(cfg.StorageURL))
			})
			r.Delete("/snapshots/{id}", g.proxy(cfg.StorageURL))
			r.Post("/snapshots/{id}/restore", g.proxy(cfg.StorageURL))
		})

		r.Route("/scheduler", func(r chi.Router) {
			r.Post("/schedule", g.proxy(cfg.SchedulerURL))
			r.Delete("/cancel", g.proxy(cfg.SchedulerURL))
			r.Get("/jobs", g.proxy(cfg.SchedulerURL))
		})

		r.Route("/images", func(r chi.Router) {
			r.Get("/official", g.proxy(cfg.ControlPlaneURL))
			r.Post("/pull", g.proxy(cfg.ControlPlaneURL))
			r.Post("/import", g.proxy(cfg.ControlPlaneURL))
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Delete("/{id}", g.proxy(cfg.ControlPlaneURL))
		})

		r.Route("/flavors", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Delete("/{id}", g.proxy(cfg.ControlPlaneURL))
		})

		r.Route("/keypairs", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Delete("/{id}", g.proxy(cfg.ControlPlaneURL))
		})

		r.Route("/backups", func(r chi.Router) {
			r.Post("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/", g.proxy(cfg.ControlPlaneURL))
			r.Get("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Delete("/{id}", g.proxy(cfg.ControlPlaneURL))
			r.Post("/{id}/restore", g.proxy(cfg.ControlPlaneURL))
		})
	})

	return g
}

func (g *Gateway) proxy(targetURL string) http.HandlerFunc {
	target, err := url.Parse("http://" + targetURL)
	if err != nil {
		log.Printf("invalid target URL %q: %v", targetURL, err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

func (g *Gateway) ServeHTTP() http.Handler {
	return g.router
}
