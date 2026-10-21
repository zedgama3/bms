package settings

import (
	"context"
	"slices"
)

// regionalIn reads the regional's values from sn, defaults filled in.
func (s *Service) regionalIn(ctx context.Context, sn *snapshot) Regional {
	get := func(key string) string {
		e := s.index[regionalArea+"."+key]
		v, stale := s.effective(sn, e)
		if stale {
			s.noteStale(ctx, e.full)
		}
		return v
	}
	// The canonical form of the public URL; a stored value that the rule now refuses (an
	// upgrade) reads as unset, under decision 5.
	publicURL := get(keyPublicURL)
	if c, err := canonicalPublicURL(publicURL); err == nil {
		publicURL = c
	} else {
		publicURL = ""
		s.noteStale(ctx, regionalArea+"."+keyPublicURL)
	}
	return Regional{
		Name:         get(keyName),
		ContactEmail: get(keyContactEmail),
		PublicURL:    publicURL,
		Language:     get(keyLanguage),
		Timezone:     get(keyTimezone),
		Languages:    slices.Clone(Languages),
	}
}

// themeIn renders the stylesheet for sn.
func (s *Service) themeIn(ctx context.Context, sn *snapshot) string {
	get := func(key string) string {
		e := s.index[regionalArea+"."+key]
		v, stale := s.effective(sn, e)
		if stale {
			s.noteStale(ctx, e.full)
		}
		return v
	}
	return renderTheme(get(keyColorPrimary), get(keyColorPrimaryDk))
}

// Regional returns the regional's own values. Consumers: notify (name, public URL), the
// public and theme handlers, api (trusted origin, Later), authz (zone, Later). Fails the
// snapshot's kinds (Unavailable, Timeout, Canceled) before the first load.
func (s *Service) Regional(ctx context.Context) (Regional, error) {
	sn, err := s.current(ctx)
	if err != nil {
		return Regional{}, wrap("settings.Service.Regional", err)
	}
	return s.regionalIn(ctx, sn), nil
}

// Public builds GET /public. No permission: every field is public (decision 6). Fails the
// snapshot's kinds before the first load.
func (s *Service) Public(ctx context.Context) (Public, error) {
	p, _, err := s.publicAt(ctx)
	return p, wrap("settings.Service.Public", err)
}

// publicAt is Public with the revision it was built from, for the ETag.
func (s *Service) publicAt(ctx context.Context) (Public, int64, error) {
	sn, err := s.current(ctx)
	if err != nil {
		return Public{}, 0, err
	}
	p := Public{
		Regional: s.regionalIn(ctx, sn),
		Theme:    s.themeIn(ctx, sn) != "",
		Logo:     sn.logo,
		Terms:    make(map[string]map[string]string, len(Languages)),
	}
	for _, lang := range Languages {
		p.Terms[lang] = flatten(s.termsIn(sn, lang))
	}
	return p, sn.revision, nil
}

// ThemeCSS renders the regional's colours as a stylesheet ("" when both are default). Fails
// the snapshot's kinds before the first load.
func (s *Service) ThemeCSS(ctx context.Context) (string, error) {
	css, _, err := s.themeAt(ctx)
	return css, wrap("settings.Service.ThemeCSS", err)
}

func (s *Service) themeAt(ctx context.Context) (string, int64, error) {
	sn, err := s.current(ctx)
	if err != nil {
		return "", 0, err
	}
	return s.themeIn(ctx, sn), sn.revision, nil
}

// Document returns every declared setting with its value, for a settings admin. Needs
// settings.read. Fails errs.Unauthenticated, errs.Permission and the snapshot's kinds.
func (s *Service) Document(ctx context.Context) (_ Document, err error) {
	const op = "settings.Service.Document"
	ctx, span := s.start(ctx, "document")
	defer endSpan(span, &err)
	if err := s.require(ctx, op, PermissionRead); err != nil {
		return Document{}, err
	}
	sn, err := s.current(ctx)
	if err != nil {
		return Document{}, wrap(op, err)
	}
	doc := Document{Revision: Revision(sn.revision), Areas: make([]AreaDoc, 0, len(s.areas))}
	for _, a := range s.areas {
		ad := AreaDoc{Name: a.Name, Core: a.Core, Settings: make([]SettingDoc, 0, len(a.Decls))}
		for _, d := range a.Decls {
			e := s.index[a.Name+"."+d.Key]
			ad.Settings = append(ad.Settings, s.settingDoc(sn, e))
		}
		doc.Areas = append(doc.Areas, ad)
	}
	return doc, nil
}

// settingDoc describes one setting. A Secret's value is never in it.
func (s *Service) settingDoc(sn *snapshot, e entry) SettingDoc {
	sd := SettingDoc{
		Key: e.full, Kind: e.decl.Kind.String(), Default: e.decl.Default,
		Options: slices.Clone(e.decl.Options),
	}
	if e.decl.Kind == Secret {
		_, sd.SecretSet = sn.sealed[e.full]
		return sd
	}
	if stored, ok := sn.values[e.full]; ok {
		if _, stale := s.effective(sn, e); stale {
			sd.Stale = true
		} else {
			v := stored
			sd.Value = &v
		}
	}
	return sd
}
