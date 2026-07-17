package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"

	_ "github.com/lib/pq"
)

type SQLRouteRepository struct {
	db *sql.DB
}

func NewSQLRouteRepository(db *sql.DB) *SQLRouteRepository {
	return &SQLRouteRepository{db: db}
}

func NewRouteRepository(database config.DatabaseConfig) (RouteRepository, error) {
	if !database.Enabled {
		return NewMemoryRouteRepository(), nil
	}
	db, err := sql.Open("postgres", database.DSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := EnsureRouteSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return NewSQLRouteRepository(db), nil
}

func EnsureRouteSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil relay route database")
	}
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS plugin_ai_relay_routes (
		platform TEXT NOT NULL,
		slug TEXT NOT NULL,
		base_url TEXT NOT NULL,
		default_model TEXT NOT NULL,
		model_map JSONB NOT NULL DEFAULT '{}'::jsonb,
		quality_map JSONB NOT NULL DEFAULT '{}'::jsonb,
		max_n INTEGER NOT NULL DEFAULT 4,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (platform, slug)
	)`)
	return err
}

func (r *SQLRouteRepository) Get(ctx context.Context, platform, slug string) (RouteConfig, bool, error) {
	row := r.db.QueryRowContext(ctx, routeSelectSQL+` WHERE platform = $1 AND slug = $2`, strings.ToLower(strings.TrimSpace(platform)), strings.ToLower(strings.TrimSpace(slug)))
	config, err := scanRouteConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RouteConfig{}, false, nil
	}
	if err != nil {
		return RouteConfig{}, false, err
	}
	return config, true, nil
}

func (r *SQLRouteRepository) List(ctx context.Context, platform string) ([]RouteConfig, error) {
	query := routeSelectSQL
	args := []any{}
	if platform = strings.ToLower(strings.TrimSpace(platform)); platform != "" {
		query += ` WHERE platform = $1`
		args = append(args, platform)
	}
	query += ` ORDER BY platform, slug`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	configs := []RouteConfig{}
	for rows.Next() {
		config, err := scanRouteConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, rows.Err()
}

func (r *SQLRouteRepository) Upsert(ctx context.Context, config RouteConfig) (RouteConfig, error) {
	normalized, err := NormalizeRouteConfig(config)
	if err != nil {
		return RouteConfig{}, err
	}
	modelMap, err := marshalRouteMap(normalized.ModelMap)
	if err != nil {
		return RouteConfig{}, err
	}
	qualityMap, err := marshalRouteMap(normalized.QualityMap)
	if err != nil {
		return RouteConfig{}, err
	}
	row := r.db.QueryRowContext(ctx, `INSERT INTO plugin_ai_relay_routes (
		platform, slug, base_url, default_model, model_map, quality_map, max_n, enabled
	) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)
	ON CONFLICT (platform, slug) DO UPDATE SET
		base_url = EXCLUDED.base_url,
		default_model = EXCLUDED.default_model,
		model_map = EXCLUDED.model_map,
		quality_map = EXCLUDED.quality_map,
		max_n = EXCLUDED.max_n,
		enabled = EXCLUDED.enabled,
		updated_at = NOW()
	RETURNING platform, slug, base_url, default_model, model_map, quality_map, max_n, enabled`,
		normalized.Platform,
		normalized.Slug,
		normalized.BaseURL,
		normalized.DefaultModel,
		modelMap,
		qualityMap,
		normalized.MaxN,
		normalized.Enabled,
	)
	return scanRouteConfig(row)
}

func (r *SQLRouteRepository) Delete(ctx context.Context, platform, slug string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM plugin_ai_relay_routes WHERE platform = $1 AND slug = $2`, strings.ToLower(strings.TrimSpace(platform)), strings.ToLower(strings.TrimSpace(slug)))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrRouteNotFound
	}
	return nil
}

const routeSelectSQL = `SELECT platform, slug, base_url, default_model, model_map, quality_map, max_n, enabled FROM plugin_ai_relay_routes`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRouteConfig(row rowScanner) (RouteConfig, error) {
	var config RouteConfig
	var modelMap, qualityMap []byte
	if err := row.Scan(&config.Platform, &config.Slug, &config.BaseURL, &config.DefaultModel, &modelMap, &qualityMap, &config.MaxN, &config.Enabled); err != nil {
		return RouteConfig{}, err
	}
	if err := json.Unmarshal(modelMap, &config.ModelMap); err != nil {
		return RouteConfig{}, fmt.Errorf("decode model map: %w", err)
	}
	if err := json.Unmarshal(qualityMap, &config.QualityMap); err != nil {
		return RouteConfig{}, fmt.Errorf("decode quality map: %w", err)
	}
	return config, nil
}

func marshalRouteMap(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
