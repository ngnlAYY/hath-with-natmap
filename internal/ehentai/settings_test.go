package ehentai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSettingsForm(t *testing.T) {
	html := `<html><body><form>
<input type="text" name="f_port" value="1234">
<input type="text" name="name" value="client">
<input type="checkbox" name="enabled" checked="checked">
<input type="checkbox" name="ignored">
</form></body></html>`
	form, locked, err := ParseSettingsForm(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseSettingsForm() error = %v", err)
	}
	if locked {
		t.Fatal("locked = true, want false")
	}
	if got := form.Get("f_port"); got != "1234" {
		t.Fatalf("f_port = %q", got)
	}
	if got := form.Get("name"); got != "client" {
		t.Fatalf("name = %q", got)
	}
	if got := form.Get("enabled"); got != "on" {
		t.Fatalf("enabled = %q", got)
	}
	if form.Has("ignored") {
		t.Fatal("unchecked checkbox should not be included")
	}
}

func TestParseSettingsFormDetectsLockedPort(t *testing.T) {
	html := `<input name="f_port" value="1234" disabled="disabled">`
	_, locked, err := ParseSettingsForm(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseSettingsForm() error = %v", err)
	}
	if !locked {
		t.Fatal("locked = false, want true")
	}
}

func TestParseSettingsFormRequiresPort(t *testing.T) {
	_, _, err := ParseSettingsForm(strings.NewReader(`<input name="name" value="client">`))
	if err == nil || !strings.Contains(err.Error(), "缺少 f_port") {
		t.Fatalf("ParseSettingsForm() error = %v, want missing f_port", err)
	}
}

func TestUpdatePortPostsExistingFieldsWithNewPort(t *testing.T) {
	var postedPort string
	var postedName string
	var gotGetCookie bool
	var gotPostCookie bool
	var gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hentaiathome.php" {
			t.Fatalf("path = %q, want /hentaiathome.php", r.URL.Path)
		}
		if got := r.URL.Query().Get("cid"); got != "client id" {
			t.Fatalf("cid = %q, want client id", got)
		}
		if got := r.URL.Query().Get("act"); got != "settings" {
			t.Fatalf("act = %q, want settings", got)
		}

		_, memberErr := r.Cookie("ipb_member_id")
		_, passErr := r.Cookie("ipb_pass_hash")
		hasCookies := memberErr == nil && passErr == nil

		switch r.Method {
		case http.MethodGet:
			gotGetCookie = hasCookies
			fmt.Fprint(w, `<input name="f_port" value="1111"><input name="name" value="client">`)
		case http.MethodPost:
			gotPostCookie = hasCookies
			gotContentType = r.Header.Get("Content-Type")
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error = %v", err)
			}
			postedPort = r.Form.Get("f_port")
			postedName = r.Form.Get("name")
			fmt.Fprint(w, `<input name="f_port" value="45678"><input name="name" value="client">`)
		default:
			t.Fatalf("method = %s, want GET or POST", r.Method)
		}
	}))
	defer server.Close()

	client := Client{
		HTTPClient: server.Client(),
		BaseURL:    server.URL + "/",
		MemberID:   "123456",
		PassHash:   "pass",
		ClientID:   "client id",
	}
	if err := client.UpdatePort(context.Background(), 45678); err != nil {
		t.Fatalf("UpdatePort() error = %v", err)
	}
	if postedPort != "45678" {
		t.Fatalf("postedPort = %q, want 45678", postedPort)
	}
	if postedName != "client" {
		t.Fatalf("postedName = %q, want client", postedName)
	}
	if !gotGetCookie || !gotPostCookie {
		t.Fatalf("cookies get=%t post=%t, want both true", gotGetCookie, gotPostCookie)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q, want application/x-www-form-urlencoded", gotContentType)
	}
}

func TestUpdatePortReturnsErrorWhenPostedPortIsNotConfirmed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, `<input name="f_port" value="1111"><input name="name" value="client">`)
		case http.MethodPost:
			fmt.Fprint(w, `<input name="f_port" value="1111"><input name="name" value="client">`)
		default:
			t.Fatalf("method = %s, want GET or POST", r.Method)
		}
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), BaseURL: server.URL, MemberID: "123", PassHash: "pass", ClientID: "999"}
	err := client.UpdatePort(context.Background(), 45678)
	if err == nil || !strings.Contains(err.Error(), "确认 Hentai@Home 端口更新失败") {
		t.Fatalf("UpdatePort() error = %v, want confirmation failure", err)
	}
}

func TestUpdatePortReturnsLockedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<input name="f_port" value="1111" disabled="disabled">`)
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), BaseURL: server.URL, MemberID: "123", PassHash: "pass", ClientID: "999"}
	err := client.UpdatePort(context.Background(), 45678)
	if err == nil || !IsPortLocked(err) {
		t.Fatalf("UpdatePort() error = %v, want port locked", err)
	}
}

func TestUpdatePortReturnsGetStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), BaseURL: server.URL, MemberID: "123", PassHash: "pass", ClientID: "999"}
	err := client.UpdatePort(context.Background(), 45678)
	if err == nil || !strings.Contains(err.Error(), "获取 Hentai@Home 设置页失败") {
		t.Fatalf("UpdatePort() error = %v, want GET status context", err)
	}
}
