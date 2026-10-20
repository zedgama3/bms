package people

import "github.com/zedgama3/bms/internal/core/settings"

// Settings declares the people.* keys for the settings core area "people" (people.md,
// "Settings"; D39): how the profile's legal name, phone and emergency contacts are treated.
// app passes them to settings.New and implements PolicySource over the area's scoped
// Reader. Titles and descriptions are the catalogue keys
// core.people.setting.<key>.title|description|option.<opt>.
//
// emergency_contacts lists hidden and optional only, so the declaration is the rule and
// agrees with Policy.Validate: emergency contacts are never required (Q-CORE7).
var Settings = []settings.Decl{
	{Key: "legal_name", Kind: settings.Enum, Default: "optional", Options: []string{"hidden", "optional", "required"}},
	{Key: "phone", Kind: settings.Enum, Default: "optional", Options: []string{"hidden", "optional", "required"}},
	{Key: "emergency_contacts", Kind: settings.Enum, Default: "optional", Options: []string{"hidden", "optional"}},
}
