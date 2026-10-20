package settings

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed terms/*.json
var termsFS embed.FS

// fallbackLanguage is the language whose defaults a language without a file uses.
const fallbackLanguage = "en"

// loadDefaultTerms reads terms/<lang>.json for every language that has a file and checks
// that the fallback language gives every core term all four forms. Fails errs.Internal: a
// bad embedded file is a bug.
func loadDefaultTerms() (map[string]map[string]Forms, error) {
	const op = "settings.loadDefaultTerms"
	out := map[string]map[string]Forms{}
	for _, lang := range Languages {
		b, err := termsFS.ReadFile("terms/" + lang + ".json")
		if err != nil {
			continue // a language without a file uses the fallback's
		}
		var byTerm map[string]Forms
		if err := json.Unmarshal(b, &byTerm); err != nil {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("terms/%s.json does not decode", lang))
		}
		for _, term := range CoreTerms {
			f, ok := byTerm[term]
			if !ok || f.Singular == "" || f.Plural == "" || f.SingularTitle == "" || f.PluralTitle == "" {
				return nil, errs.E(op, errs.Internal, fmt.Errorf("terms/%s.json lacks a form of %s", lang, term))
			}
		}
		out[lang] = byTerm
	}
	if _, ok := out[fallbackLanguage]; !ok {
		return nil, errs.E(op, errs.Internal, errors.New("terms/en.json is missing"))
	}
	return out, nil
}

// defaultsFor returns the embedded defaults of lang, or the fallback's.
func (s *Service) defaultsFor(lang string) map[string]Forms {
	if d, ok := s.defaults[lang]; ok {
		return d
	}
	return s.defaults[fallbackLanguage]
}

func validLanguage(lang string) bool { return slices.Contains(Languages, lang) }

func unknownLanguage(op string) error {
	return errs.E(op, errs.Invalid, fieldError("lang", codeOneOf,
		map[string]string{"options": strings.Join(Languages, ", ")}, "must be one of: "+strings.Join(Languages, ", ")))
}

// formPtr returns the address of the named form of f; nil for an unknown name.
func (f *Forms) formPtr(name string) *string {
	switch name {
	case formSingular:
		return &f.Singular
	case formPlural:
		return &f.Plural
	case formSingularTitle:
		return &f.SingularTitle
	case formPluralTitle:
		return &f.PluralTitle
	}
	return nil
}

// formPtr is the same for a FormChanges' pointers.
func (c *FormChanges) formPtr(name string) **string {
	switch name {
	case formSingular:
		return &c.Singular
	case formPlural:
		return &c.Plural
	case formSingularTitle:
		return &c.SingularTitle
	case formPluralTitle:
		return &c.PluralTitle
	}
	return nil
}

// formsIn returns the forms of one term in sn for lang: the embedded defaults with the
// overrides applied. An override of a form this build does not know is ignored.
func (s *Service) formsIn(sn *snapshot, lang, term string) Forms {
	f := s.defaultsFor(lang)[term]
	for form, v := range sn.terms[lang][term] {
		if p := f.formPtr(form); p != nil {
			*p = v
		}
	}
	return f
}

// TermsFor returns every core term in one language, overrides applied over the embedded
// defaults (a language without a defaults file uses "en"). Fails errs.Invalid (lang, one_of)
// for a language not in Languages, and the snapshot's kinds (Unavailable, Timeout,
// Canceled) before the first load.
func (s *Service) TermsFor(ctx context.Context, lang string) (map[string]Forms, error) {
	const op = "settings.Service.TermsFor"
	if !validLanguage(lang) {
		return nil, unknownLanguage(op)
	}
	sn, err := s.current(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	return s.termsIn(sn, lang), nil
}

func (s *Service) termsIn(sn *snapshot, lang string) map[string]Forms {
	out := make(map[string]Forms, len(CoreTerms))
	for _, term := range CoreTerms {
		out[term] = s.formsIn(sn, lang, term)
	}
	return out
}

// flatten renders terms as the shell's {term_*} arguments.
func flatten(terms map[string]Forms) map[string]string {
	out := make(map[string]string, 4*len(terms))
	for term, f := range terms {
		out["term_"+term] = f.Singular
		out["term_"+term+"_plural"] = f.Plural
		out["term_"+term+"_title"] = f.SingularTitle
		out["term_"+term+"_plural_title"] = f.PluralTitle
	}
	return out
}

// Terms returns every language and term with its default and its override. Needs
// settings.read. Fails errs.Unauthenticated, errs.Permission and the snapshot's kinds.
func (s *Service) Terms(ctx context.Context) (_ TermsDocument, err error) {
	const op = "settings.Service.Terms"
	ctx, span := s.start(ctx, "terms")
	defer endSpan(span, &err)
	if err := s.require(ctx, op, PermissionRead); err != nil {
		return TermsDocument{}, err
	}
	sn, err := s.current(ctx)
	if err != nil {
		return TermsDocument{}, wrap(op, err)
	}
	doc := TermsDocument{
		Languages: slices.Clone(Languages),
		Terms:     make(map[string]map[string]TermDoc, len(Languages)),
	}
	for _, lang := range Languages {
		doc.Terms[lang] = make(map[string]TermDoc, len(CoreTerms))
		for _, term := range CoreTerms {
			td := TermDoc{Default: s.defaultsFor(lang)[term]}
			for form, v := range sn.terms[lang][term] {
				if p := td.Override.formPtr(form); p != nil {
					*p = &v
				}
			}
			doc.Terms[lang][term] = td
		}
	}
	return doc, nil
}
