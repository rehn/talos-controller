package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginAndSession(t *testing.T) {
	a, err := New("admin", "pw", "", []byte("secret"), time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	a.LoginHandler(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"username":"admin","password":"pw"}`)))
	if rec.Code != 200 {
		t.Fatalf("login failed: %d", rec.Code)
	}

	cookie := rec.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	if a.User(req) != "admin" {
		t.Fatal("valid session rejected")
	}

	// Tampered cookie.
	bad := *cookie
	bad.Value = strings.Replace(bad.Value, ".", "x.", 1)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&bad)
	if a.User(req) != "" {
		t.Fatal("tampered session accepted")
	}

	// Other secret.
	b, _ := New("admin", "pw", "", []byte("other"), time.Hour, false)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	if b.User(req) != "" {
		t.Fatal("session signed with other secret accepted")
	}
}

func TestBcrypt(t *testing.T) {
	h, _ := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	a, _ := New("admin", "", string(h), []byte("s"), time.Hour, false)
	if !a.checkCredentials("admin", "hunter2") || a.checkCredentials("admin", "nope") {
		t.Fatal("bcrypt check wrong")
	}
}

func TestLockout(t *testing.T) {
	a, _ := New("admin", "pw", "", []byte("s"), time.Hour, false)
	for i := 0; i < 5; i++ {
		a.recordFailure("1.2.3.4")
	}
	if !a.tooManyFailures("1.2.3.4") || a.tooManyFailures("5.6.7.8") {
		t.Fatal("lockout logic wrong")
	}
}
