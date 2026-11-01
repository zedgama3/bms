package settings

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// change is one validated key of a Set.
type change struct {
	e      entry
	reset  bool   // back to the default
	value  string // the text form to store, when not reset and not a secret
	secret string // the plaintext of a Secret, when not reset
}

// Set changes the settings named in c, all or nothing. Needs settings.manage. Every key
// must be declared; each value is trimmed (a Secret is not), parsed by its kind and passed
// through the declaration's Check; then, inside the Write transaction, each touched area's
// Check runs over its effective values read from the stored rows, never from the cache. A
// nil value, or one equal to the default, resets the key. One audit entry per key that
// really changed is recorded inside the transaction, the revision is bumped, and the
// snapshot is replaced after commit; a batch that changes nothing writes, records and bumps
// nothing. A Secret is never compared with what is stored: saving one again counts as a
// change (a fresh nonce makes new ciphertext, the revision bumps, the change is audited),
// and neither a Check nor an area Check can tell whether a secret is already set (F22a).
// It returns the revision after the write. Fails errs.Unauthenticated, errs.Permission,
// errs.Invalid (field = the setting key; codes in the spec), errs.Unsupported (a Secret
// value without a sealing key), errs.Unavailable wrapping db.ErrReadOnly, errs.Internal (no
// revision row) and store kinds.
func (s *Service) Set(ctx context.Context, c Changes) (_ Revision, err error) {
	const op = "settings.Service.Set"
	ctx, span := s.start(ctx, "set")
	defer endSpan(span, &err)
	if err := s.require(ctx, op, PermissionManage); err != nil {
		return 0, err
	}
	span.SetAttrs(telemetry.Int("settings.keys", len(c.Values)))
	plan, err := s.plan(op, c)
	if err != nil {
		return 0, err
	}
	span.SetAttrs(telemetry.Token("settings.area", planArea(plan)))
	// The area Check runs once, in the Write transaction over the stored rows (setTx): a
	// check over the cached snapshot could refuse a batch that is valid against what is
	// stored, or pass one that is not (decision 15, F23a).
	sealed, err := s.sealAll(op, plan)
	if err != nil {
		return 0, err
	}
	var rev Revision
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, err := s.setTx(ctx, op, plan, sealed)
		rev = r
		return err
	})
	if err != nil {
		return 0, wrap(op, err)
	}
	return rev, nil
}

// planArea names the one area a batch touches, or "multiple".
func planArea(plan []change) string {
	area := ""
	for _, ch := range plan {
		switch {
		case area == "":
			area = ch.e.area
		case area != ch.e.area:
			return "multiple"
		}
	}
	return area
}

// plan validates c and returns its changes sorted by key, reporting every problem at once.
func (s *Service) plan(op string, c Changes) ([]change, error) {
	v := validate.Errors{Op: op}
	switch n := len(c.Values); {
	case n == 0:
		v.Addf("values", codeRequired, "set at least one setting")
	case n > MaxBatch:
		v.Add(errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "values", Code: codeTooMany, Params: map[string]string{"max": itoa(MaxBatch)},
			Reason: "at most " + itoa(MaxBatch) + " settings in one change",
		}))
	}
	if err := v.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	keys := make([]string, 0, len(c.Values))
	for k := range c.Values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	plan := make([]change, 0, len(keys))
	for _, key := range keys {
		e, ok := s.index[key]
		if !ok {
			v.Addf(safeField(key), codeUnknown, "no such setting")
			continue
		}
		ch, problems := s.changeOf(e, c.Values[key])
		for _, p := range problems {
			v.Add(p)
		}
		if len(problems) == 0 {
			plan = append(plan, ch)
		}
	}
	if err := v.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return plan, nil
}

// changeOf validates one value of e. The problems carry the full key as their field.
func (s *Service) changeOf(e entry, p *string) (change, []error) {
	ch := change{e: e}
	if p == nil {
		ch.reset = true
		return ch, nil
	}
	v := *p
	if e.decl.Kind == Secret {
		if v == "" { // a clear: nothing to check, and a Check must not see it
			ch.reset = true
			return ch, nil
		}
		if pr := shapeOf(e.decl, v); pr.code != "" {
			return ch, []error{fieldError(e.full, pr.code, pr.params, pr.reason)}
		}
		if e.decl.Check != nil {
			if err := e.decl.Check(v); err != nil {
				return ch, checkError(e.full, err, true)
			}
		}
		ch.secret = v
		return ch, nil
	}
	v = strings.TrimSpace(v)
	if e.normalize != nil {
		v = e.normalize(v)
	}
	if pr := shapeOf(e.decl, v); pr.code != "" {
		return ch, []error{fieldError(e.full, pr.code, pr.params, pr.reason)}
	}
	if e.decl.Check != nil {
		if err := e.decl.Check(v); err != nil {
			return ch, checkError(e.full, err, false)
		}
	}
	if sameValue(e.decl, v, e.decl.Default) {
		ch.reset = true
		return ch, nil
	}
	ch.value = canonical(e.decl, v)
	return ch, nil
}

// checkAreas runs the Check of every area the plan touches over its effective values:
// every declared key, the plan's value for a key it changes, the stored value (or the
// default) for the rest, and "" for a Secret. A returned field gets the area as prefix.
func (s *Service) checkAreas(op string, plan []change, sn *snapshot) error {
	touched := map[int]struct{}{}
	byKey := make(map[string]change, len(plan))
	for _, ch := range plan {
		touched[s.areaIdx[ch.e.area]] = struct{}{}
		byKey[ch.e.full] = ch
	}
	order := make([]int, 0, len(touched))
	for i := range touched {
		order = append(order, i)
	}
	slices.Sort(order)
	v := validate.Errors{Op: op}
	for _, i := range order {
		a := s.areas[i]
		if a.Check == nil {
			continue
		}
		values := make(map[string]string, len(a.Decls))
		for _, d := range a.Decls {
			e := s.index[a.Name+"."+d.Key]
			switch ch, changed := byKey[e.full]; {
			case d.Kind == Secret:
				values[d.Key] = ""
			case changed && ch.reset:
				values[d.Key] = d.Default
			case changed:
				values[d.Key] = ch.value
			default:
				values[d.Key], _ = s.effective(sn, e)
			}
		}
		if err := a.Check(values); err != nil {
			for _, p := range areaCheckErrors(a.Name, err) {
				v.Add(p)
			}
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// areaCheckErrors converts an Area.Check's error into field errors named "<area>.<field>".
func areaCheckErrors(area string, err error) []error {
	fes := validate.Fields(err)
	if len(fes) == 0 {
		return []error{fieldError(area, codeRejected, nil, err.Error())}
	}
	out := make([]error, len(fes))
	for i, fe := range fes {
		field := area
		if fe.Field != "" {
			field += "." + fe.Field
		}
		out[i] = fieldError(field, fe.Code, fe.Params, fe.Reason)
	}
	return out
}

// sealAll seals the plan's secrets before the transaction opens. Fails errs.Unsupported
// when there is a secret to set and no sealing key.
func (s *Service) sealAll(op string, plan []change) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, ch := range plan {
		if ch.reset || ch.e.decl.Kind != Secret {
			continue
		}
		if s.sealer == nil {
			return nil, errs.E(op, errs.Unsupported, errors.New("a secret setting needs settings.sealing_key"))
		}
		blob, err := s.sealer.seal(ch.e.full, ch.secret)
		if err != nil {
			return nil, wrap(op, err)
		}
		out[ch.e.full] = blob
	}
	return out, nil
}

// setTx is the body of Set's write transaction.
func (s *Service) setTx(ctx context.Context, op string, plan []change, sealed map[string][]byte) (Revision, error) {
	rev0, err := s.store.LockRevision(ctx)
	if err != nil {
		return 0, revisionErr(op, err)
	}
	next, err := s.freshSnapshot(ctx, op, rev0)
	if err != nil {
		return 0, err
	}
	if err := s.checkAreas(op, plan, next); err != nil {
		return 0, err
	}
	now := s.now()
	var entries []audit.Entry
	for _, ch := range plan {
		full := ch.e.full
		var changed bool
		switch {
		case ch.reset:
			changed, err = s.store.DeleteSetting(ctx, full)
		case ch.e.decl.Kind == Secret:
			changed, err = s.store.UpsertSetting(ctx, full, nil, sealed[full], now)
		default:
			v := ch.value
			changed, err = s.store.UpsertSetting(ctx, full, &v, nil, now)
		}
		if err != nil {
			return 0, constraintErr(op, full, err)
		}
		if !changed {
			continue
		}
		action := actionSettingChanged
		delete(next.values, full)
		delete(next.sealed, full)
		switch {
		case ch.reset:
			action = actionSettingReset
		case ch.e.decl.Kind == Secret:
			next.sealed[full] = sealed[full]
		default:
			next.values[full] = ch.value
		}
		entries = append(entries, audit.Entry{
			Action: action, Outcome: audit.Succeeded,
			Record: audit.Ref{Kind: kindSetting, ID: IDOf(full)},
			Fields: []string{strings.TrimPrefix(full, ch.e.area+".")}, Level: model.SensitivityInternal,
			Reason: "key:" + full,
		})
	}
	return s.finish(ctx, op, rev0, next, entries)
}

// freshSnapshot reads every stored row inside the write transaction, under the revision
// lock, so the checks and the next snapshot are built from what cannot change underneath.
func (s *Service) freshSnapshot(ctx context.Context, op string, rev int64) (*snapshot, error) {
	ss, err := s.store.LoadSettings(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	ts, err := s.store.LoadTerminology(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	return snapshotOf(rev, ss, ts), nil
}

// finish records the entries, bumps the revision and arranges for next to replace the
// snapshot after commit. With no entries nothing changed: no record, no bump.
func (s *Service) finish(ctx context.Context, op string, rev0 int64, next *snapshot, entries []audit.Entry) (Revision, error) {
	if len(entries) == 0 {
		return Revision(rev0), nil
	}
	if err := s.audit.Record(ctx, entries...); err != nil {
		return 0, wrap(op, err)
	}
	return s.bump(ctx, op, rev0, next)
}

// bump adds one to the revision and arranges for next to replace the snapshot after commit.
func (s *Service) bump(ctx context.Context, op string, rev0 int64, next *snapshot) (Revision, error) {
	bumped, err := s.store.BumpRevision(ctx)
	if err != nil {
		return 0, revisionErr(op, err)
	}
	next.revision = bumped
	// Under the revision lock every commit so far is at rev0 or below, so a snapshot
	// newer than rev0 can only come from before a restore to an earlier backup.
	regressed := false
	if cur := s.snap.Load(); cur != nil && cur.revision > rev0 {
		regressed = true
	}
	if err := db.AfterCommit(ctx, func(ctx context.Context) {
		if regressed {
			s.replace(ctx, next)
			return
		}
		s.install(next)
	}); err != nil {
		return 0, wrap(op, err)
	}
	return Revision(bumped), nil
}

// Revise runs fn inside one Write transaction after taking the revision row lock, which
// serialises it with Set, SetTerms and every other Revise across instances. When fn reports
// changed, Revise bumps the revision and, after commit, installs the snapshot it built under
// the lock (as Set does); when fn reports no change, it commits nothing new and returns the
// current revision. fn's error rolls everything back and is returned with its kind. Revise
// records no audit entry (fn records under its own area) and checks no permission: it is a
// method of *settings.Service, which only app holds (modules see a Reader), and the caller has
// checked its own permission first. Call it outside any transaction. Consumer: regional
// (SetLogo, RemoveLogo). It fails errs.Unavailable wrapping db.ErrReadOnly on a read-only
// copy (before fn runs), errs.Internal when core.setting_revision has no row, and the store's
// kinds.
func (s *Service) Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (_ Revision, err error) {
	const op = "settings.Service.Revise"
	ctx, span := s.start(ctx, "revise")
	defer endSpan(span, &err)
	if fn == nil {
		return 0, errs.E(op, errs.Invalid, errors.New("nil function"))
	}
	var rev Revision
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		rev0, err := s.store.LockRevision(ctx)
		if err != nil {
			return revisionErr(op, err)
		}
		changed, err := fn(ctx)
		if err != nil {
			return err //nolint:wrapcheck // fn's own error, kept with its kind
		}
		if !changed {
			rev = Revision(rev0)
			return nil
		}
		next, err := s.freshSnapshot(ctx, op, rev0)
		if err != nil {
			return err
		}
		rev, err = s.bump(ctx, op, rev0, next)
		return err
	})
	if err != nil {
		return 0, wrap(op, err)
	}
	return rev, nil
}

// Constraint names of the CHECKs a value can break (migration 0001).
const (
	conSettingLength = "setting_value_length_chk"
	conSettingChars  = "setting_value_chars_chk"
	conTermLength    = "terminology_value_length_chk"
	conTermChars     = "terminology_value_chars_chk"
)

// constraintErr turns a violated value CHECK into errs.Invalid on field; Go validates
// first, so this is a safety net. Any other error keeps its kind.
func constraintErr(op, field string, err error) error {
	var pe *db.Error
	if errors.As(err, &pe) {
		switch pe.Constraint {
		case conSettingLength:
			p := tooLong(MaxTextBytes)
			return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: p.code, Params: p.params, Reason: p.reason})
		case conTermLength:
			p := tooLong(MaxTermBytes)
			return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: p.code, Params: p.params, Reason: p.reason})
		case conSettingChars, conTermChars:
			return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: codeControl, Reason: "must not contain control characters"})
		}
	}
	return wrap(op, err)
}

// ---- terminology -------------------------------------------------------------------------

// termChange is one validated term of a SetTerms: the forms to store and the forms to
// reset (a form set to its default is a reset).
type termChange struct {
	term   string
	set    map[string]string
	resets []string
}

// SetTerms changes the forms of the terms in c for lang, all or nothing, under the same
// transaction rules as Set (audit actions settings.term.changed and .reset). Needs
// settings.manage. A form is trimmed, 1..MaxTermBytes bytes, with no control characters;
// one set to the default is stored as a reset. Fails errs.Unauthenticated, errs.Permission,
// errs.Invalid (lang; terms, terms.<term>, terms.<term>.<form>, terms.<term>.reset),
// errs.Unavailable wrapping db.ErrReadOnly, errs.Internal (no revision row) and store kinds.
func (s *Service) SetTerms(ctx context.Context, lang string, c TermChanges) (_ Revision, err error) {
	const op = "settings.Service.SetTerms"
	ctx, span := s.start(ctx, "set_terms")
	defer endSpan(span, &err)
	if err := s.require(ctx, op, PermissionManage); err != nil {
		return 0, err
	}
	span.SetAttrs(telemetry.Int("settings.keys", len(c.Terms)))
	if !validLanguage(lang) {
		return 0, unknownLanguage(op)
	}
	plan, err := s.planTerms(op, lang, c)
	if err != nil {
		return 0, err
	}
	var rev Revision
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, err := s.setTermsTx(ctx, op, lang, plan)
		rev = r
		return err
	})
	if err != nil {
		return 0, wrap(op, err)
	}
	return rev, nil
}

// planTerms validates c and returns its terms in declaration order: CoreTerms, then the
// areas' terms in build order.
func (s *Service) planTerms(op, lang string, c TermChanges) ([]termChange, error) {
	v := validate.Errors{Op: op}
	switch n := len(c.Terms); {
	case n == 0:
		v.Addf("terms", codeRequired, "set at least one term")
	case n > len(s.terms):
		v.Add(errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "terms", Code: codeTooMany, Params: map[string]string{"max": itoa(len(s.terms))},
			Reason: "at most " + itoa(len(s.terms)) + " terms",
		}))
	}
	var plan []termChange
	for name := range c.Terms {
		if _, ok := s.termIdx[name]; !ok {
			v.Addf("terms."+safeField(name), codeUnknown, "no such term")
		}
	}
	for _, te := range s.terms {
		term := te.key
		fc, ok := c.Terms[term]
		if !ok {
			continue
		}
		tc := termChange{term: term, set: map[string]string{}}
		for _, form := range formNames {
			p := *fc.formPtr(form)
			if p == nil {
				continue
			}
			field := "terms." + term + "." + form
			val := strings.TrimSpace(*p)
			if pr := termShape(val); pr.code != "" {
				v.Add(fieldError(field, pr.code, pr.params, pr.reason))
				continue
			}
			if val == s.defaultOf(lang, te).formValue(form) {
				tc.resets = append(tc.resets, form)
				continue
			}
			tc.set[form] = val
		}
		for _, form := range fc.Reset {
			switch {
			case !slices.Contains(formNames, form):
				v.Addf("terms."+term+".reset", codeUnknown, "no such form")
			case *fc.formPtr(form) != nil:
				v.Addf("terms."+term+".reset", codeConflict, "a form cannot be set and reset in one change")
			case !slices.Contains(tc.resets, form):
				tc.resets = append(tc.resets, form)
			}
		}
		slices.Sort(tc.resets)
		plan = append(plan, tc)
	}
	if err := v.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return plan, nil
}

func (f Forms) formValue(name string) string {
	if p := f.formPtr(name); p != nil {
		return *p
	}
	return ""
}

// termShape checks one trimmed form.
func termShape(v string) problem {
	switch {
	case v == "":
		return problem{code: codeRequired, reason: "must not be empty"}
	case !utf8.ValidString(v):
		return problem{code: codeMalformed, reason: "must be valid UTF-8"}
	case strings.IndexFunc(v, unicode.IsControl) >= 0:
		return problem{code: codeControl, reason: "must not contain control characters"}
	case len(v) > MaxTermBytes:
		return tooLong(MaxTermBytes)
	}
	return problem{}
}

// setTermsTx is the body of SetTerms' write transaction.
func (s *Service) setTermsTx(ctx context.Context, op, lang string, plan []termChange) (Revision, error) {
	rev0, err := s.store.LockRevision(ctx)
	if err != nil {
		return 0, revisionErr(op, err)
	}
	next, err := s.freshSnapshot(ctx, op, rev0)
	if err != nil {
		return 0, err
	}
	now := s.now()
	var entries []audit.Entry
	for _, tc := range plan {
		ref := audit.Ref{Kind: kindTerm, ID: IDOf(lang + ":" + tc.term)}
		reason := "term:" + lang + ":" + tc.term
		if len(tc.set) > 0 {
			forms := make([]string, 0, len(tc.set))
			for _, form := range formNames {
				if _, ok := tc.set[form]; ok {
					forms = append(forms, form)
				}
			}
			values := make([]string, len(forms))
			for i, form := range forms {
				values[i] = tc.set[form]
			}
			changed, err := s.store.UpsertTermForms(ctx, lang, tc.term, forms, values, now)
			if err != nil {
				return 0, constraintErr(op, "terms."+tc.term, err)
			}
			for _, form := range changed {
				next.setTerm(lang, tc.term, form, tc.set[form])
			}
			if len(changed) > 0 {
				entries = append(entries, audit.Entry{
					Action: actionTermChanged, Outcome: audit.Succeeded, Record: ref,
					Fields: changed, Level: model.SensitivityInternal, Reason: reason,
				})
			}
		}
		if len(tc.resets) > 0 {
			changed, err := s.store.DeleteTermForms(ctx, lang, tc.term, tc.resets)
			if err != nil {
				return 0, constraintErr(op, "terms."+tc.term, err)
			}
			for _, form := range changed {
				delete(next.terms[lang][tc.term], form)
			}
			if len(changed) > 0 {
				entries = append(entries, audit.Entry{
					Action: actionTermReset, Outcome: audit.Succeeded, Record: ref,
					Fields: changed, Level: model.SensitivityInternal, Reason: reason,
				})
			}
		}
	}
	return s.finish(ctx, op, rev0, next, entries)
}
