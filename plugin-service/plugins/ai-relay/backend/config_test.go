package backend

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
)

func TestMemoryRouteRepositoryStoresRouteWithoutCredential(t *testing.T) {
	repository := NewMemoryRouteRepository()
	saved, err := repository.Upsert(context.Background(), RouteConfig{
		Platform: "agnes",
		Slug:     "team-a",
		BaseURL:  "https://apihub.agnes-ai.com/v1",
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if saved.Platform != "agnes" || saved.Slug != "team-a" {
		t.Fatalf("saved route = %#v", saved)
	}

	loaded, ok, err := repository.Get(context.Background(), "agnes", "team-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || loaded.BaseURL != "https://apihub.agnes-ai.com/v1" {
		t.Fatalf("loaded route = %#v, ok = %v", loaded, ok)
	}
}

func TestRouteConfigDoesNotRequireImageLimit(t *testing.T) {
	config, err := NormalizeRouteConfig(RouteConfig{
		Platform: "agnes",
		Slug:     "team-a",
		BaseURL:  "https://apihub.agnes-ai.com/v1",
	})
	if err != nil {
		t.Fatalf("NormalizeRouteConfig() error = %v", err)
	}
	if config.Platform != "agnes" || config.Slug != "team-a" {
		t.Fatalf("config = %#v", config)
	}
}

func TestRouteConfigDefaultsNameAndFiltersConfigurations(t *testing.T) {
	repository := NewMemoryRouteRepository()
	primary, err := repository.Upsert(context.Background(), RouteConfig{
		Platform: "agnes", Slug: "primary", BaseURL: "https://apihub.agnes-ai.com/v1",
	})
	if err != nil || primary.Name != "primary" {
		t.Fatalf("primary = %#v, err = %v", primary, err)
	}
	_, err = repository.Upsert(context.Background(), RouteConfig{
		Platform: "agnes", Slug: "backup", Name: "Backup route", BaseURL: "https://apihub.agnes-ai.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := repository.List(context.Background(), RouteQuery{Platform: "agnes", Search: "primary"})
	if err != nil || len(routes) != 1 || routes[0].Slug != "primary" {
		t.Fatalf("routes = %#v, err = %v", routes, err)
	}
}

func TestNewRouteRepositoryUsesMemoryWithoutDatabase(t *testing.T) {
	repository, err := NewRouteRepository(config.DatabaseConfig{})
	if err != nil {
		t.Fatalf("NewRouteRepository() error = %v", err)
	}
	if _, ok := repository.(*MemoryRouteRepository); !ok {
		t.Fatalf("repository type = %T, want *MemoryRouteRepository", repository)
	}
}

func TestSQLRouteRepositoryUpsertsConfigurationWithoutCredential(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS plugin_ai_relay_routes")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE plugin_ai_relay_routes DROP COLUMN IF EXISTS default_model")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE plugin_ai_relay_routes DROP COLUMN IF EXISTS model_map")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE plugin_ai_relay_routes DROP COLUMN IF EXISTS quality_map")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE plugin_ai_relay_routes DROP COLUMN IF EXISTS max_n")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE plugin_ai_relay_routes DROP COLUMN IF EXISTS enabled")).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := EnsureRouteSchema(context.Background(), db); err != nil {
		t.Fatalf("EnsureRouteSchema() error = %v", err)
	}
	mock.ExpectQuery("INSERT INTO plugin_ai_relay_routes").
		WithArgs("agnes", "team-a", "team-a", "https://apihub.agnes-ai.com/v1").
		WillReturnRows(sqlmock.NewRows([]string{"platform", "slug", "name", "base_url"}).
			AddRow("agnes", "team-a", "team-a", "https://apihub.agnes-ai.com/v1"))

	repository := NewSQLRouteRepository(db)
	_, err = repository.Upsert(context.Background(), RouteConfig{
		Platform: "agnes", Slug: "team-a", BaseURL: "https://apihub.agnes-ai.com/v1",
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeRouteConfigRejectsInvalidSlugAndInsecureURL(t *testing.T) {
	_, err := NormalizeRouteConfig(RouteConfig{
		Platform: "agnes",
		Slug:     "bad/path",
		BaseURL:  "http://example.test",
	})
	if err == nil {
		t.Fatal("NormalizeRouteConfig() error = nil, want validation error")
	}
}
