package regional

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	"github.com/zedgama3/bms/internal/core/settings"
)

// Profile is the regional's own values (area "regional"), defaults filled in.
type Profile struct {
	Name             string `json:"name"`               // regional.name
	ContactEmail     string `json:"contact_email"`      // regional.contact_email ("" = none)
	PublicURL        string `json:"public_url"`         // regional.public_url ("" = unset; no trailing slash)
	Language         string `json:"language"`           // regional.language, one of settings.Languages
	Timezone         string `json:"timezone"`           // regional.timezone, an IANA name or "" (the browser's)
	ColorPrimary     string `json:"color_primary"`      // regional.color_primary, #rrggbb or ""
	ColorPrimaryDark string `json:"color_primary_dark"` // regional.color_primary_dark, #rrggbb or ""
}

// Public is what GET /public returns. Everything in it is the site's own appearance.
type Public struct {
	Name         string   `json:"name"`
	ContactEmail string   `json:"contact_email"`
	PublicURL    string   `json:"public_url"`
	Language     string   `json:"language"`
	Timezone     string   `json:"timezone"`
	Languages    []string `json:"languages"` // the build's languages (settings.Languages), offered to everyone
	Theme        bool     `json:"theme"`     // theme.css is not empty
	// Logo is the logo's file ID ("" when none). The shell requests /api/regional/logo?v=<Logo>
	// so a replaced logo is a new URL. Public, like every field here (settings.md decision 6).
	Logo string `json:"logo"`
	// Terms per language, flattened for the shell's {term_*} arguments:
	// {"en": {"term_department": "department", "term_department_plural": "departments",
	// "term_department_title": "Department", "term_department_plural_title": "Departments"}}
	Terms map[string]map[string]string `json:"terms"`
}

// Profile reads the seven regional.* keys through the Reader, defaults filled in. A stored
// public URL that its rule now refuses (an upgrade) reads as unset, and is warned about once.
// It fails with the Reader's kind (Unavailable, Timeout or Canceled before the first load of
// settings' snapshot).
func (s *Service) Profile(ctx context.Context) (Profile, error) {
	const op = "regional.Service.Profile"
	var p Profile
	for _, f := range []struct {
		key string
		dst *string
	}{
		{keyName, &p.Name}, {keyContactEmail, &p.ContactEmail}, {keyPublicURL, &p.PublicURL},
		{keyLanguage, &p.Language}, {keyTimezone, &p.Timezone},
		{keyColorPrimary, &p.ColorPrimary}, {keyColorPrimaryDk, &p.ColorPrimaryDark},
	} {
		v, err := s.settings.Text(ctx, f.key)
		if err != nil {
			return Profile{}, fmt.Errorf("%s: %s.%s: %w", op, Area, f.key, err)
		}
		*f.dst = v
	}
	if c, err := canonicalPublicURL(p.PublicURL); err == nil {
		p.PublicURL = c
	} else {
		p.PublicURL = ""
		s.warnOnce(ctx, Area+"."+keyPublicURL)
	}
	return p, nil
}

// warnOnce logs, once per process and key, that a stored value no longer passes its rule and
// the default applies.
func (s *Service) warnOnce(ctx context.Context, key string) {
	if _, seen := s.warned.LoadOrStore(key, struct{}{}); !seen {
		s.logger.WarnContext(ctx, "a stored setting no longer parses; using its default", slog.String("setting", key))
	}
}

// Public builds GET /public: the profile, whether theme.css has content, the logo's file ID
// and the terms in every language. No permission: every field is public. The logo's ID is
// read with one ByRecord per settings revision and process. It fails the Reader's, the
// revision source's, the files store's and the term source's kinds.
func (s *Service) Public(ctx context.Context) (Public, error) {
	p, _, err := s.publicAt(ctx)
	return p, wrap("regional.Service.Public", err)
}

// publicAt is Public with the entity tag of what it built. The revision is read first, so
// the body is at least as new as the tag and a stale body is never served under a newer tag.
// Every logo change bumps settings' revision (SetLogo and RemoveLogo run in Revise), so the
// tag is the revision alone and the logo's ID is cached per revision.
func (s *Service) publicAt(ctx context.Context) (Public, string, error) {
	rev, err := s.revision.Revision(ctx)
	if err != nil {
		return Public{}, "", err //nolint:wrapcheck // callers add their op
	}
	prof, err := s.Profile(ctx)
	if err != nil {
		return Public{}, "", err
	}
	logo, err := s.logoAt(ctx, rev)
	if err != nil {
		return Public{}, "", err
	}
	p := Public{
		Name: prof.Name, ContactEmail: prof.ContactEmail, PublicURL: prof.PublicURL,
		Language: prof.Language, Timezone: prof.Timezone,
		Languages: slices.Clone(settings.Languages),
		Theme:     renderTheme(prof.ColorPrimary, prof.ColorPrimaryDark) != "",
		Logo:      logo,
		Terms:     make(map[string]map[string]string, len(settings.Languages)),
	}
	for _, lang := range settings.Languages {
		forms, err := s.terms.TermsFor(ctx, lang)
		if err != nil {
			return Public{}, "", fmt.Errorf("terms in %s: %w", lang, err)
		}
		p.Terms[lang] = flatten(forms)
	}
	return p, etagOf(rev), nil
}

// ThemeCSS renders the regional's colours as a stylesheet ("" when both are default). It
// fails the Reader's kinds.
func (s *Service) ThemeCSS(ctx context.Context) (string, error) {
	css, _, err := s.themeAt(ctx)
	return css, wrap("regional.Service.ThemeCSS", err)
}

// themeAt is ThemeCSS with the entity tag of what it rendered (the revision alone: the logo
// is not in the stylesheet).
func (s *Service) themeAt(ctx context.Context) (string, string, error) {
	rev, err := s.revision.Revision(ctx)
	if err != nil {
		return "", "", err //nolint:wrapcheck // callers add their op
	}
	light, err := s.settings.Text(ctx, keyColorPrimary)
	if err != nil {
		return "", "", fmt.Errorf("%s.%s: %w", Area, keyColorPrimary, err)
	}
	dark, err := s.settings.Text(ctx, keyColorPrimaryDk)
	if err != nil {
		return "", "", fmt.Errorf("%s.%s: %w", Area, keyColorPrimaryDk, err)
	}
	return renderTheme(light, dark), etagOf(rev), nil
}

// etagOf is the strong entity tag of the public documents at a settings revision.
func etagOf(rev settings.Revision) string {
	return `"regional-` + strconv.FormatInt(int64(rev), 10) + `"`
}

// logoAt returns the logo's file ID as of revision rev: the held one when it was read at the
// same revision, else one ByRecord, which replaces it. The cache is correct because every
// logo change bumps the revision; a replica's poll moves its revision, so its next call
// re-reads. It fails the files store's kinds.
func (s *Service) logoAt(ctx context.Context, rev settings.Revision) (string, error) {
	cur := s.logo.Load()
	if cur != nil && cur.revision == rev {
		return cur.id, nil
	}
	// A revision below the held one means the database went back (a restore): the held entry
	// is dropped for the lookup below. The lookup comes after the revision read, and an error
	// stores nothing.
	id, err := s.logoFileID(ctx)
	if err != nil {
		return "", err
	}
	// Replace only the entry seen above: if another request stored meanwhile, its entry is at
	// least as new as ours, and a slow request never moves the cache backwards.
	s.logo.CompareAndSwap(cur, &logoCache{revision: rev, id: id})
	return id, nil
}

// flatten renders terms as the shell's {term_*} arguments.
func flatten(terms map[string]settings.Forms) map[string]string {
	out := make(map[string]string, 4*len(terms))
	for term, f := range terms {
		out["term_"+term] = f.Singular
		out["term_"+term+"_plural"] = f.Plural
		out["term_"+term+"_title"] = f.SingularTitle
		out["term_"+term+"_plural_title"] = f.PluralTitle
	}
	return out
}
