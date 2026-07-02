package database

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/miclle/qiniu-playground/internal/entity"
)

func TestOpenRejectsMissingDSN(t *testing.T) {
	if _, err := Open(context.Background(), "postgres", ""); err == nil {
		t.Fatal("Open should reject an empty DSN")
	}
}

func TestOpenRejectsUnsupportedDriver(t *testing.T) {
	if _, err := Open(context.Background(), "sqlite", "file:test.db"); err == nil {
		t.Fatal("Open should reject unsupported driver")
	}
}

func TestMigrateRejectsNilDB(t *testing.T) {
	if err := Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate should reject nil db")
	}
}

func TestMigrateUsesExplicitTableNames(t *testing.T) {
	db := openTestDB(t)

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	expectedTables := []string{
		"accounts",
		"oauth_identities",
		"github_installations",
		"github_repositories",
		"workspaces",
		"qiniu_credentials",
		"sandbox_sessions",
	}
	for _, tableName := range expectedTables {
		if !db.Migrator().HasTable(tableName) {
			t.Fatalf("expected table %q to exist", tableName)
		}
	}

	unexpectedTables := []string{
		"o_auth_identities",
		"git_hub_installations",
		"git_hub_repositories",
	}
	for _, tableName := range unexpectedTables {
		if db.Migrator().HasTable(tableName) {
			t.Fatalf("unexpected table %q exists", tableName)
		}
	}
}

func TestMigrateUsesLeanWorkspaceSchema(t *testing.T) {
	db := openTestDB(t)

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	removedColumns := []string{
		"github_repository_id",
		"repo_html_url",
		"default_branch",
		"cpu_count",
		"memory_gb",
		"last_connected_at",
	}
	for _, column := range removedColumns {
		if db.Migrator().HasColumn(&entity.Workspace{}, column) {
			t.Fatalf("workspaces.%s should not exist", column)
		}
	}
}

func TestPersistentEntityFieldsDeclareColumnTags(t *testing.T) {
	entities := []any{
		entity.Account{},
		entity.OAuthIdentity{},
		entity.GitHubInstallation{},
		entity.GitHubRepository{},
		entity.Workspace{},
		entity.QiniuCredential{},
		entity.SandboxSession{},
	}

	for _, model := range entities {
		modelType := reflect.TypeOf(model)
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			if field.PkgPath != "" || field.Type == reflect.TypeOf(entity.Account{}) {
				continue
			}
			if !strings.Contains(field.Tag.Get("gorm"), "column:") {
				t.Fatalf("%s.%s is missing an explicit gorm column tag", modelType.Name(), field.Name)
			}
		}
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}
