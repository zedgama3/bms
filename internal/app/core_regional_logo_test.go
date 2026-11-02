package app

// Logo wiring (settings.md amendment 2026-10-21, L9 and Proposed change 14), moved with the
// logo to regional (boundaries step 7). Regional is built after files and takes the scoped
// store directly, so there is no seam between them to test.

import (
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestRegionalRoutes_CarryBodyCapAndTimeout(t *testing.T) {
	in := []httpx.Route{
		{Method: http.MethodGet, Pattern: "/public", Public: true, Handler: http.NotFoundHandler()},
		{Method: http.MethodPut, Pattern: "/logo", Handler: http.NotFoundHandler(), MaxBodyBytes: 2 * regional.MaxLogoBytes, Timeout: files.UploadTimeout},
		{Method: http.MethodGet, Pattern: "/logo", Public: true, Handler: http.NotFoundHandler(), Timeout: files.ServeTimeout},
	}
	set, err := module.CoreRoutes("regional", in, nil)
	if err != nil {
		t.Fatalf("CoreRoutes: %v", err)
	}
	out := set.Routes
	if len(out) != len(in) {
		t.Fatalf("%d routes out, %d in", len(out), len(in))
	}
	if out[0].MaxBodyBytes != 0 || out[0].Timeout != 0 {
		t.Errorf("a route without limits got %d / %v", out[0].MaxBodyBytes, out[0].Timeout)
	}
	if out[1].MaxBodyBytes != 2*regional.MaxLogoBytes || out[1].Timeout != files.UploadTimeout || out[1].Access == module.AccessPublic {
		t.Errorf("PUT /logo = %+v", out[1])
	}
	if out[2].Timeout != files.ServeTimeout || out[2].Access != module.AccessPublic {
		t.Errorf("GET /logo = %+v", out[2])
	}
}

// The assembled "regional" route set holds the five routes with the spec's limits and access,
// and the "settings" route set holds none of the moved ones.
func TestBuiltRegionalRoutes_IncludeTheLogo(t *testing.T) {
	a := builtCore(t)
	var found, regionalRoutes int
	for _, rs := range a.routeSets {
		switch rs.Module {
		case "regional":
			regionalRoutes = len(rs.Routes)
			for _, r := range rs.Routes {
				switch r.Pattern {
				case "/public", "/theme.css":
					if r.Access != module.AccessPublic || r.Method != http.MethodGet {
						t.Errorf("%s %s: method/access = %s/%v, want a public GET", r.Method, r.Pattern, r.Method, r.Access)
					}
				case "/logo":
					found++
					switch r.Method {
					case http.MethodPut:
						if r.MaxBodyBytes != 2*regional.MaxLogoBytes || r.Timeout != files.UploadTimeout || r.Access == module.AccessPublic {
							t.Errorf("PUT /logo = limits %d / %v, access %v", r.MaxBodyBytes, r.Timeout, r.Access)
						}
					case http.MethodDelete:
						if r.Access == module.AccessPublic {
							t.Error("DELETE /logo is public")
						}
					case http.MethodGet:
						if r.Access != module.AccessPublic {
							t.Error("GET /logo is not public")
						}
					}
				default:
					t.Errorf("unexpected regional route %s %s", r.Method, r.Pattern)
				}
			}
		case "settings":
			for _, r := range rs.Routes {
				switch r.Pattern {
				case "/public", "/theme.css", "/logo":
					t.Errorf("settings still serves %s %s", r.Method, r.Pattern)
				}
			}
		}
	}
	if found != 3 {
		t.Errorf("%d /logo routes in the regional route set, want 3", found)
	}
	if regionalRoutes != 5 {
		t.Errorf("%d routes in the regional route set, want 5", regionalRoutes)
	}
}

// The logo's files owner stays "settings": deployed logos keep their bucket and
// core.file.owner (boundaries decision 12), though regional now holds the record.
func TestRegional_LogoOwnerStaysSettings(t *testing.T) {
	if filesOwner != "settings" {
		t.Errorf("filesOwner = %q, want settings", filesOwner)
	}
	if regional.LogoRef.Kind != files.LogoRecordKind || files.LogoRecordKind != "settings.logo" {
		t.Errorf("LogoRef.Kind = %q, want %q", regional.LogoRef.Kind, "settings.logo")
	}
}
