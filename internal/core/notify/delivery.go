package notify

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The delivery rules: how a message finds its recipient's address, language and name, and
// the site and terms a template renders with. They were app's (boundaries F1) and are notify's
// because they are a rule of delivering mail. The Service reads them through Accounts,
// Profiles, Regional and Terms, which are the identity and people Lookups and regional's
// and settings' reads. None of them logs or returns an address in an error.

// addressOfPerson answers the person's current address: the login address of the account,
// else the contact address of the profile. A person without an account (NotFound) or an
// account with no address is asked of Profiles. It fails errs.NotFound when neither holds one
// (the sender then ends the message as undeliverable) and with the lookups' kinds otherwise.
func (s *Service) addressOfPerson(ctx context.Context, person model.PersonID) (model.Email, error) {
	const op = "notify.Service.addressOfPerson"
	to, err := s.accounts.EmailOf(ctx, person)
	switch {
	case err == nil && to != "":
		return to, nil
	case err != nil && !errs.Is(err, errs.NotFound):
		return "", fmt.Errorf("%s: account: %w", op, err)
	}
	to, err = s.profiles.ContactEmailOf(ctx, person)
	if err != nil {
		return "", fmt.Errorf("%s: contact: %w", op, err)
	}
	return to, nil
}

// languageOf answers the person's saved language tag, "" for a person with no profile
// (NotFound), and the lookup's kinds otherwise.
func (s *Service) languageOf(ctx context.Context, person model.PersonID) (Language, error) {
	l, err := s.profiles.LanguageOf(ctx, person)
	switch {
	case errs.Is(err, errs.NotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("notify.Service.languageOf: %w", err)
	}
	return Language(l), nil
}

// nameOf answers the person's playa name, "" for a person with no profile (NotFound), and
// the lookup's kinds otherwise.
func (s *Service) nameOf(ctx context.Context, person model.PersonID) (string, error) {
	n, err := s.profiles.PlayaNameOf(ctx, person)
	switch {
	case errs.Is(err, errs.NotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("notify.Service.nameOf: %w", err)
	}
	return n, nil
}

// siteOf answers the regional's name, public URL, default language and the terms in lang: ""
// and a language the build does not ship (neither itself nor its primary subtag) use the
// regional's default language. Each term is under its singular key, and under key_plural,
// key_title and key_plural_title for its other forms. It fails with the kinds of Regional
// and Terms.
func (s *Service) siteOf(ctx context.Context, lang Language) (Site, error) {
	const op = "notify.Service.siteOf"
	reg, err := s.regional.Profile(ctx)
	if err != nil {
		return Site{}, fmt.Errorf("%s: %w", op, err)
	}
	use := string(lang)
	if !slices.Contains(settings.Languages, use) {
		use, _, _ = strings.Cut(use, "-")
	}
	if !slices.Contains(settings.Languages, use) {
		use = reg.Language
	}
	forms, err := s.terms.TermsFor(ctx, use)
	if err != nil {
		return Site{}, fmt.Errorf("%s: terms in %s: %w", op, use, err)
	}
	terms := make(map[string]string, 4*len(forms))
	for key, f := range forms {
		terms[key] = f.Singular
		terms[key+"_plural"] = f.Plural
		terms[key+"_title"] = f.SingularTitle
		terms[key+"_plural_title"] = f.PluralTitle
	}
	return Site{Name: reg.Name, URL: reg.PublicURL, DefaultLanguage: Language(reg.Language), Terms: terms}, nil
}
