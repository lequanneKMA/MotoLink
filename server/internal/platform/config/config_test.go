package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaultsNeedDatabaseURL(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("want DATABASE_URL error, got %v", err)
	}
}

func TestKafkaSinkRequiresBrokers(t *testing.T) {
	_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err == nil || !strings.Contains(err.Error(), "KAFKA_BROKERS") {
		t.Fatalf("want KAFKA_BROKERS error, got %v", err)
	}
}

func TestRenderProfileWithoutKafka(t *testing.T) { // ADR-021
	c, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "HISTORY_SINK": "direct_pg", "OUTBOX_SINK": "inproc",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Roles.API || !c.Roles.WS || !c.Roles.Worker {
		t.Fatalf("default roles should be all, got %+v", c.Roles)
	}
}

func TestRolesParsing(t *testing.T) {
	c, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "HISTORY_SINK": "direct_pg", "OUTBOX_SINK": "inproc", "ROLES": "api",
	}))
	if err != nil || !c.Roles.API || c.Roles.WS || c.Roles.Worker {
		t.Fatalf("roles=%+v err=%v", c.Roles, err)
	}
	if _, err := Load(env(map[string]string{"DATABASE_URL": "x", "ROLES": "api,bogus"})); err == nil {
		t.Fatal("unknown role must fail")
	}
}

func TestCoordLoggingForbiddenInProd(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "HISTORY_SINK": "direct_pg", "OUTBOX_SINK": "inproc",
		"APP_ENV": "prod", "DEBUG_LOG_COORDS": "true",
	}))
	if err == nil || !strings.Contains(err.Error(), "DEBUG_LOG_COORDS") {
		t.Fatalf("want DEBUG_LOG_COORDS error, got %v", err)
	}
}

func TestBadValuesReported(t *testing.T) {
	_, err := Load(env(map[string]string{"DATABASE_URL": "x", "WS_MAX_CONNS": "abc", "ACCESS_TOKEN_TTL": "zz"}))
	if err == nil || !strings.Contains(err.Error(), "WS_MAX_CONNS") || !strings.Contains(err.Error(), "ACCESS_TOKEN_TTL") {
		t.Fatalf("want both errors, got %v", err)
	}
}
