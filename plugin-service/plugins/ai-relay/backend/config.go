package backend

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var routeSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var (
	ErrInvalidRouteConfig = errors.New("invalid relay route configuration")
	ErrRouteNotFound      = errors.New("relay route not found")
)

type RouteConfig struct {
	Platform     string            `json:"platform"`
	Slug         string            `json:"slug"`
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	ModelMap     map[string]string `json:"model_map,omitempty"`
	QualityMap   map[string]string `json:"quality_map,omitempty"`
	MaxN         int               `json:"max_n"`
	Enabled      bool              `json:"enabled"`
}

type RouteRepository interface {
	Get(ctx context.Context, platform, slug string) (RouteConfig, bool, error)
	List(ctx context.Context, platform string) ([]RouteConfig, error)
	Upsert(ctx context.Context, config RouteConfig) (RouteConfig, error)
	Delete(ctx context.Context, platform, slug string) error
}

type MemoryRouteRepository struct {
	mu     sync.RWMutex
	routes map[string]RouteConfig
}

func NewMemoryRouteRepository() *MemoryRouteRepository {
	return &MemoryRouteRepository{routes: make(map[string]RouteConfig)}
}

func NormalizeRouteConfig(config RouteConfig) (RouteConfig, error) {
	config.Platform = strings.ToLower(strings.TrimSpace(config.Platform))
	config.Slug = strings.ToLower(strings.TrimSpace(config.Slug))
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.DefaultModel = strings.TrimSpace(config.DefaultModel)
	if config.Platform == "" || !routeSlugPattern.MatchString(config.Platform) || !routeSlugPattern.MatchString(config.Slug) {
		return RouteConfig{}, ErrInvalidRouteConfig
	}
	parsedURL, err := url.Parse(config.BaseURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return RouteConfig{}, ErrInvalidRouteConfig
	}
	if config.DefaultModel == "" {
		return RouteConfig{}, ErrInvalidRouteConfig
	}
	if config.MaxN == 0 {
		config.MaxN = 4
	}
	if config.MaxN < 1 || config.MaxN > 10 {
		return RouteConfig{}, ErrInvalidRouteConfig
	}
	config.ModelMap = copyStringMap(config.ModelMap)
	config.QualityMap = copyStringMap(config.QualityMap)
	return config, nil
}

func (r *MemoryRouteRepository) Get(_ context.Context, platform, slug string) (RouteConfig, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	config, ok := r.routes[routeKey(platform, slug)]
	return copyRouteConfig(config), ok, nil
}

func (r *MemoryRouteRepository) List(_ context.Context, platform string) ([]RouteConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	platform = strings.ToLower(strings.TrimSpace(platform))
	configs := make([]RouteConfig, 0, len(r.routes))
	for _, config := range r.routes {
		if platform == "" || config.Platform == platform {
			configs = append(configs, copyRouteConfig(config))
		}
	}
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].Platform == configs[j].Platform {
			return configs[i].Slug < configs[j].Slug
		}
		return configs[i].Platform < configs[j].Platform
	})
	return configs, nil
}

func (r *MemoryRouteRepository) Upsert(_ context.Context, config RouteConfig) (RouteConfig, error) {
	normalized, err := NormalizeRouteConfig(config)
	if err != nil {
		return RouteConfig{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[routeKey(normalized.Platform, normalized.Slug)] = copyRouteConfig(normalized)
	return copyRouteConfig(normalized), nil
}

func (r *MemoryRouteRepository) Delete(_ context.Context, platform, slug string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := routeKey(platform, slug)
	if _, ok := r.routes[key]; !ok {
		return ErrRouteNotFound
	}
	delete(r.routes, key)
	return nil
}

func routeKey(platform, slug string) string {
	return strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.ToLower(strings.TrimSpace(slug))
}

func copyRouteConfig(config RouteConfig) RouteConfig {
	config.ModelMap = copyStringMap(config.ModelMap)
	config.QualityMap = copyStringMap(config.QualityMap)
	return config
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
