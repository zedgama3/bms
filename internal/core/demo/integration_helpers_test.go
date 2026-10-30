//go:build integration

package demo_test

import (
	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/demo"
)

// Helpers used only by the integration tests (the unit tests do not need them).

func (l *callLog) byMethod(seam, method string) []call {
	var out []call
	for _, c := range l.all() {
		if c.Seam == seam && c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// values flattens every Set into key to value.
func (f *fakeSettings) values() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for _, ch := range f.set {
		for k, v := range ch.Values {
			if v != nil {
				out[k] = *v
			}
		}
	}
	return out
}

func (r *rig) link(cs ...demo.Contributor) {
	r.t.Helper()
	wantNoErr(r.t, r.svc.Link(cs))
}

var deptKeys = []string{
	demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW, demoseed.DeptSanctuary,
}
