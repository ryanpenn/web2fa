package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterServesRequestedPages(t *testing.T) {
	router := setupRouter()

	tests := []struct {
		name     string
		path     string
		contains []string
	}{
		{
			name:     "home page",
			path:     "/",
			contains: []string{`data-view="home"`, "在线 2FA"},
		},
		{
			name:     "list page",
			path:     "/list",
			contains: []string{`data-view="list"`, "Secret (Base32)", "备注"},
		},
		{
			name:     "single secret page",
			path:     "/JBSWY3DPEHPK3PXP",
			contains: []string{`data-view="single"`, `data-initial-secret="JBSWY3DPEHPK3PXP"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			body := recorder.Body.String()
			for _, want := range tt.contains {
				if !strings.Contains(body, want) {
					t.Fatalf("body does not contain %q", want)
				}
			}
		})
	}
}

func TestHomePageDoesNotExposeVerifyFeature(t *testing.T) {
	router := setupRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	router.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, forbidden := range []string{"/verify", "验证 TOTP", "verifyBtn"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("home page still contains verify feature marker %q", forbidden)
		}
	}
}

func TestVerifyRouteIsRemoved(t *testing.T) {
	router := setupRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/verify?secret=JBSWY3DPEHPK3PXP&code=123456", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGenerateStillProducesTOTPJSON(t *testing.T) {
	router := setupRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/generate?secret=JBSWY3DPEHPK3PXP", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response struct {
		Code           string `json:"code"`
		SecondsElapsed int    `json:"secondsElapsed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Code) != 6 {
		t.Fatalf("code length = %d, want 6", len(response.Code))
	}
	if response.SecondsElapsed < 0 || response.SecondsElapsed >= int(period) {
		t.Fatalf("secondsElapsed = %d, want [0,%d)", response.SecondsElapsed, period)
	}
}

func TestRenderedPagePreservesSingleSecretAcrossNavigation(t *testing.T) {
	body := renderBody(t, "/")

	for _, want := range []string{
		`const SINGLE_SECRET_KEY = "web2fa.singleSecret";`,
		`localStorage.getItem(SINGLE_SECRET_KEY)`,
		`localStorage.setItem(SINGLE_SECRET_KEY,`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain single-secret persistence marker %q", want)
		}
	}
}

func TestHomeSecretInputHasClearButton(t *testing.T) {
	body := renderBody(t, "/")

	for _, want := range []string{
		`<div class="input-with-clear">`,
		`id="clearSingleSecret"`,
		`aria-label="清空 Secret"`,
		`function updateSingleSecretClearButton()`,
		`clearSingleSecret.hidden = !singleSecret.value;`,
		`localStorage.removeItem(SINGLE_SECRET_KEY);`,
		`singleSecret.focus();`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain single-secret clear marker %q", want)
		}
	}
}

func TestRenderedPagePreventsDuplicateListSecrets(t *testing.T) {
	body := renderBody(t, "/list")

	for _, want := range []string{
		"function normalizeSecret",
		"function findDuplicateSecret",
		"Secret 已存在",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain duplicate-secret guard marker %q", want)
		}
	}
}

func TestSingleCodeUsesListCodeAccentColor(t *testing.T) {
	body := renderBody(t, "/")

	codeButtonStyleStart := strings.Index(body, ".code-button {")
	if codeButtonStyleStart == -1 {
		t.Fatal("body does not contain .code-button style")
	}
	codeButtonStyleEnd := strings.Index(body[codeButtonStyleStart:], "}")
	if codeButtonStyleEnd == -1 {
		t.Fatal("body does not contain a complete .code-button style block")
	}
	codeButtonStyle := body[codeButtonStyleStart : codeButtonStyleStart+codeButtonStyleEnd]
	if !strings.Contains(codeButtonStyle, "color: var(--accent);") {
		t.Fatal("single code style does not use the accent color shared with list codes")
	}
}

func TestBrandLogoIsStatic(t *testing.T) {
	body := renderBody(t, "/list")

	if strings.Contains(body, `<a class="brand" href="/">`) {
		t.Fatal("brand logo should not be a home link")
	}
	if !strings.Contains(body, `<div class="brand">`) {
		t.Fatal("brand logo should render as a static brand block")
	}
}

func TestListSecretLinksToSecretPage(t *testing.T) {
	body := renderBody(t, "/list")

	for _, want := range []string{
		`secretLink.className = "secret-link";`,
		`secretLink.setAttribute("href", "/" + encodeURIComponent(record.secret));`,
		`secretLink.textContent = record.secret;`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain secret link marker %q", want)
		}
	}
}

func renderBody(t *testing.T, path string) string {
	t.Helper()

	router := setupRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	return recorder.Body.String()
}
