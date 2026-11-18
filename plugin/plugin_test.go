package django

import (
	"reflect"
	"sort"
	"testing"
)

func TestFindManageAndSettings(t *testing.T) {
	if got := findManagePy("testdata/dj1", nil); got != "backend/manage.py" {
		t.Fatalf("manage = %q", got)
	}
	m := analyzeModule("testdata/dj1", nil)
	if m.Settings != "config.settings.dev" {
		t.Fatalf("settings = %q", m.Settings)
	}
	v := append([]string{}, m.Variants...)
	sort.Strings(v)
	want := []string{"config.settings.base", "config.settings.dev", "config.settings.prod"}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("variants = %v", v)
	}
}

func TestManifest(t *testing.T) {
	m := loadManifest(t)
	for _, line := range []string{"Starting development server at http://127.0.0.1:8000/", "Starting WSGI development server at http://127.0.0.1:8000/", "Starting ASGI/Daphne version 4.1.0 development server at http://127.0.0.1:8001/"} {
		if got := matchURL(m, line); got == "" || got[len(got)-1] == '/' {
			t.Errorf("url(%q) = %q", line, got)
		}
	}
	if mm := matchRule(t, m, "django.unapplied-migrations", "You have 18 unapplied migration(s). Your project may not work properly until you apply the migrations for app(s): admin, auth, contenttypes, sessions."); mm != nil && mm[1] != "18" {
		t.Errorf("count = %q", mm[1])
	}
	matchRule(t, m, "django.port-in-use", "Error: That port is already in use.")
	matchRule(t, m, "django.no-such-table", "django.db.utils.OperationalError: no such table: auth_user")
	matchRule(t, m, "django.not-installed", "ImportError: Couldn't import Django. Are you sure it's installed and available on your PYTHONPATH environment variable?")
	matchRule(t, m, "django.mysqlclient", "django.core.exceptions.ImproperlyConfigured: Error loading MySQLdb module.")
	matchRule(t, m, "django.psycopg", "django.core.exceptions.ImproperlyConfigured: Error loading psycopg2 or psycopg module")
	matchRule(t, m, "django.secret-key", "django.core.exceptions.ImproperlyConfigured: The SECRET_KEY setting must not be empty.")
	matchRule(t, m, "django.db-connection", `django.db.utils.OperationalError: (2003, "Can't connect to MySQL server on '127.0.0.1' (61)")`)
}
