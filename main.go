// =============================================================================
// online-shop — single-binary HTTP server for the sbx-02 first-workload demo
// =============================================================================
//
// Routes (all served under /shop/ to match the Gateway HTTPRoute PathPrefix):
//   GET /shop/                  -> HTML catalog page (rendered from embedded template)
//   GET /shop/api/products      -> JSON array of all products
//   GET /shop/api/products/{id} -> JSON for one product or 404
//   GET /shop/healthz           -> "ok" (used by k8s liveness/readiness probes)
//
// Listens on :8080. Logs every request with method, path, status, and latency.
// No external dependencies — std lib only. Four products are hardcoded in the
// `products` map; this is a single-pod prototype, no DB or Redis.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Product is the single domain object — id, name, price (in pence), stock,
// plus a single emoji rendered in the HTML card. Emoji is intentionally an
// API field too — clients consuming /shop/api/products see the same icon.
type Product struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price int    `json:"price_pence"` // store integer pence to dodge float rounding
	Stock int    `json:"stock"`
	Emoji string `json:"emoji"`
}

// Hardcoded catalog. Swap for a DB the day a single pod stops being enough.
var products = []Product{
	{ID: "p1", Name: "Single-Origin Coffee Beans (250g)", Price: 1850, Stock: 12, Emoji: "☕"},
	{ID: "p2", Name: "Hand-Thrown Ceramic Mug", Price: 1200, Stock: 30, Emoji: "🫖"},
	{ID: "p3", Name: "Stainless French Press (1L)", Price: 3500, Stock: 6, Emoji: "🪴"},
	{ID: "p4", Name: "Manual Burr Grinder", Price: 3950, Stock: 22, Emoji: "⚙️"},
}

//go:embed index.html
var templateFS embed.FS

// loggingResponseWriter wraps http.ResponseWriter to capture the status code
// for the access log (the std http.ResponseWriter doesn't expose it).
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

// withLogging is middleware that prints "method path status duration" per request.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lrw.status, time.Since(start))
	})
}

// handleHealthz is the lightweight probe endpoint. Plain "ok" body, 200.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

// handleListProducts returns the full product catalog as JSON.
func handleListProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(products); err != nil {
		http.Error(w, "encoding failed", http.StatusInternalServerError)
	}
}

// handleGetProduct returns one product by id (taken from the trailing path
// segment), or 404 if no match. Pre-Go-1.22 routing for portability — the
// handler does its own path parsing rather than relying on PathValue.
func handleGetProduct(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/shop/api/products/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	for _, p := range products {
		if p.ID == id {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(p)
			return
		}
	}
	http.NotFound(w, r)
}

// templateFuncs adds helpers the html/template package doesn't ship with —
// here just `gbp` (pence integer → "12.50" GBP string) so the template can
// render prices without doing arithmetic in HTML.
var templateFuncs = template.FuncMap{
	"gbp": func(pence int) string { return fmt.Sprintf("%.2f", float64(pence)/100) },
}

// handleIndex renders the HTML catalog page from the embedded template.
// The template receives the product list directly.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Only serve the index for exactly /shop/ — anything else under /shop/
	// that doesn't match a more specific route falls through to 404.
	if r.URL.Path != "/shop/" && r.URL.Path != "/shop" {
		http.NotFound(w, r)
		return
	}
	tpl, err := template.New("index.html").Funcs(templateFuncs).ParseFS(templateFS, "index.html")
	if err != nil {
		http.Error(w, "template parse failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.Execute(w, products); err != nil {
		log.Printf("template execute: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()

	// More-specific routes first; std lib's mux uses longest-prefix match
	// but being explicit reads better. /shop/api/products/ has the trailing
	// slash so it matches anything below it (per std-mux semantics).
	mux.HandleFunc("/shop/healthz", handleHealthz)
	mux.HandleFunc("/shop/api/products/", handleGetProduct)
	mux.HandleFunc("/shop/api/products", handleListProducts)
	mux.HandleFunc("/shop/", handleIndex)

	addr := ":8080"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		addr = v
	}
	log.Printf("online-shop listening on %s (3 products in catalog)", addr)
	if err := http.ListenAndServe(addr, withLogging(mux)); err != nil {
		log.Fatalf("server: %v", err)
	}
}
