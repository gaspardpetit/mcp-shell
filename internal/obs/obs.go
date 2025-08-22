package obs

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

var (
	maxConcurrency = 4
	sem            chan struct{}

	defaultRPS     = 5.0
	defaultTimeout = 60 * time.Second

	rateLimiters sync.Map // map[string]*rate.Limiter

	calls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_calls_total",
			Help: "Total tool calls",
		},
		[]string{"tool"},
	)
	errors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_errors_total",
			Help: "Tool call errors",
		},
		[]string{"tool"},
	)
	timeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_timeouts_total",
			Help: "Tool call timeouts",
		},
		[]string{"tool"},
	)
	durations = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tool_duration_seconds",
			Help:    "Tool call duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tool"},
	)
)

func init() {
	if v := os.Getenv("MAX_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConcurrency = n
		}
	}
	sem = make(chan struct{}, maxConcurrency)

	if v := os.Getenv("DEFAULT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			defaultRPS = f
		}
	}
	if v := os.Getenv("DEFAULT_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			defaultTimeout = time.Duration(ms) * time.Millisecond
		}
	}

	prometheus.MustRegister(calls, errors, timeouts, durations)
}

func getLimiter(tool string) *rate.Limiter {
	if lim, ok := rateLimiters.Load(tool); ok {
		return lim.(*rate.Limiter)
	}
	rps := defaultRPS
	envName := "RATE_LIMIT_" + strings.ToUpper(strings.ReplaceAll(tool, ".", "_"))
	if v := os.Getenv(envName); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rps = f
		}
	}
	lim := rate.NewLimiter(rate.Limit(rps), int(rps))
	rateLimiters.Store(tool, lim)
	return lim
}

// Middleware enforces concurrency, rate limits, default timeouts, and records metrics.
func Middleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tool := req.Params.Name

		// rate limit
		lim := getLimiter(tool)
		if err := lim.Wait(ctx); err != nil {
			errors.WithLabelValues(tool).Inc()
			return nil, err
		}

		// concurrency
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			errors.WithLabelValues(tool).Inc()
			return nil, ctx.Err()
		}
		defer func() { <-sem }()

		// default timeout
		ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()

		calls.WithLabelValues(tool).Inc()
		start := time.Now()
		res, err := next(ctx2, req)
		durations.WithLabelValues(tool).Observe(time.Since(start).Seconds())

		if err != nil {
			if ctx2.Err() == context.DeadlineExceeded {
				timeouts.WithLabelValues(tool).Inc()
			} else {
				errors.WithLabelValues(tool).Inc()
			}
			return res, err
		}

		if res != nil && res.StructuredContent != nil {
			data, _ := json.Marshal(res.StructuredContent)
			var out struct {
				ExitCode int    `json:"exit_code"`
				Error    string `json:"error"`
			}
			if err := json.Unmarshal(data, &out); err == nil {
				if out.Error != "" || out.ExitCode != 0 {
					errors.WithLabelValues(tool).Inc()
					if out.ExitCode == 124 {
						timeouts.WithLabelValues(tool).Inc()
					}
				}
			}
		}

		return res, err
	}
}

// MetricsHandler exposes Prometheus metrics.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
