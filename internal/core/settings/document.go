package settings

import (
	"context"
	"slices"
)

// Revision returns the revision of the current snapshot: the number every write bumps, the
// same one Document reports. regional derives the ETag of its public document from it
// (regional.RevisionSource). Fails the snapshot's kinds (Unavailable, Timeout, Canceled)
// before the first load.
func (s *Service) Revision(ctx context.Context) (Revision, error) {
	sn, err := s.current(ctx)
	if err != nil {
		return 0, wrap("settings.Service.Revision", err)
	}
	return Revision(sn.revision), nil
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
