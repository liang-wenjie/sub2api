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
		Platform:     "agnes",
		Slug:         "team-a",
		BaseURL:      "https://apihub.agnes-ai.com/v1",
		DefaultModel: "agnes-image-2.1-flash",
		Enabled:      true,
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
	if err := EnsureRouteSchema(context.Background(), db); err != nil {
		t.Fatalf("EnsureRouteSchema() error = %v", err)
	}
	mock.ExpectQuery("INSERT INTO plugin_ai_relay_routes").
		WithArgs("agnes", "team-a", "https://apihub.agnes-ai.com/v1", "agnes-image-2.1-flash", "{}", "{}", 4, true).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "slug", "base_url", "default_model", "model_map", "quality_map", "max_n", "enabled"}).
			AddRow("agnes", "team-a", "https://apihub.agnes-ai.com/v1", "agnes-image-2.1-flash", "{}", "{}", 4, true))

	repository := NewSQLRouteRepository(db)
	_, err = repository.Upsert(context.Background(), RouteConfig{
		Platform: "agnes", Slug: "team-a", BaseURL: "https://apihub.agnes-ai.com/v1", DefaultModel: "agnes-image-2.1-flash", Enabled: true,
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
