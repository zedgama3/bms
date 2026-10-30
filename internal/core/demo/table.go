package demo

import (
	"fmt"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/authz"
)

// The seed's constant tables (decision 12). Everything here is invented: no row may be a
// real person, every address ends in "@demo.invalid" and every phone is "+1 555 01xx".

// account is one demo account: its key, its starter roles as the sign-in table shows them
// and the profile the seed fills in (empty for the newcomer).
type account struct {
	key     string
	roles   []string
	profile *profile
}

// profile is one invented person's profile.
type profile struct {
	playa, pronouns, legal, phone string
}

func strp(s string) *string { return &s }

// accountTable lists the ten accounts in creation and display order.
var accountTable = []account{
	{demoseed.ActorParticipant, []string{authz.RoleParticipant}, &profile{"Sprocket", "they/them", "Alex Example", "+1 555 0101"}},
	{demoseed.ActorNewcomer, []string{authz.RoleParticipant}, nil},
	{demoseed.ActorLead, leadRoles(), &profile{"Marbles", "she/her", "Sam Sample", "+1 555 0102"}},
	{demoseed.ActorCoordinator, []string{authz.RoleVolunteerCoordinator}, &profile{"Compass", "he/him", "Jordan Placeholder", "+1 555 0103"}},
	{demoseed.ActorSteward, []string{authz.RoleDepartmentSteward}, &profile{"Tumbleweed", "they/them", "", ""}},
	{demoseed.ActorEventAdmin, []string{authz.RoleEventAdmin}, &profile{"Lantern", "she/her", "", ""}},
	{demoseed.ActorSettingsAdmin, []string{authz.RoleSettingsAdmin}, &profile{"Dial Tone", "he/him", "", ""}},
	{demoseed.ActorAccountAdmin, []string{authz.RoleAccountAdmin}, &profile{"Ledger", "they/them", "Casey Fictional", "+1 555 0104"}},
	{demoseed.ActorAccessAdmin, []string{authz.RoleAccessAdmin}, &profile{"Gatekeeper", "she/her", "", ""}},
	{demoseed.ActorAuditReviewer, []string{authz.RoleAuditReviewer}, &profile{"Quill", "he/him", "", ""}},
}

// grantTable lists the grants the access-admin makes, in order. The lead's department role
// is made in the departments step, after the departments exist.
var grantTable = []struct{ key, role string }{
	{demoseed.ActorSteward, authz.RoleDepartmentSteward},
	{demoseed.ActorEventAdmin, authz.RoleEventAdmin},
	{demoseed.ActorSettingsAdmin, authz.RoleSettingsAdmin},
	{demoseed.ActorAccountAdmin, authz.RoleAccountAdmin},
	{demoseed.ActorAuditReviewer, authz.RoleAuditReviewer},
	{demoseed.ActorCoordinator, authz.RoleVolunteerCoordinator},
}

// departmentTable lists the six departments in creation order.
var departmentTable = []struct{ key, name, description string }{
	{demoseed.DeptRangers, "Rangers", "Keep the event safe and calm: patrols, perimeters, the radio"},
	{demoseed.DeptGate, "Gate", "Tickets, waivers and wristbands at the entrance"},
	{demoseed.DeptGreeters, "Greeters", "Welcome arrivals and point them to camp"},
	{demoseed.DeptMedical, "Medical", "First aid and the medical tent"},
	{demoseed.DeptDPW, "DPW", "Build and strike the city"},
	{demoseed.DeptSanctuary, "Sanctuary", "Peer support and a quiet place"},
}

// walkOnPlayaNames are the playa names of walkon-01..walkon-12.
var walkOnPlayaNames = [demoseed.WalkOns]string{
	"Dusty Gears", "Ember", "Static", "Kite", "Pebble", "Sundog",
	"Monsoon", "Tinsel", "Flint", "Bramble", "Echo", "Juniper",
}

var walkOnPronouns = [...]string{"they/them", "she/her", "he/him"}

// walkOn returns walk-on n (1..WalkOns): odd numbers carry a legal name and a 555-01xx phone.
func walkOn(n int) profile {
	p := profile{playa: walkOnPlayaNames[n-1], pronouns: walkOnPronouns[(n-1)%len(walkOnPronouns)]}
	if n%2 == 1 {
		p.legal = p.playa + " Walkon"
		p.phone = fmt.Sprintf("+1 555 01%02d", 10+n)
	}
	return p
}

// leadDepartments are the departments the lead leads, in grant order (amendment D1.1):
// the five that get volunteer data. Sanctuary has no lead on purpose.
var leadDepartments = []string{
	demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW,
}

// leadRoles is the lead's role list for the sign-in table: department-admin at each.
func leadRoles() []string {
	out := make([]string, len(leadDepartments))
	for i, d := range leadDepartments {
		out[i] = authz.RoleDepartmentAdmin + "@" + d
	}
	return out
}
