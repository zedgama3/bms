// Package module is the contract between the core and the feature modules compiled into
// the binary (D5, D19): what a module declares (Manifest), what it gives the core at
// runtime (Module), what the core gives it (Deps), the registry of modules, and the
// start-up check of the whole graph. It sits at the top of the core: it imports core
// contracts, and no core package imports it.
//
// A module package declares `var Manifest = module.Manifest{...}`, a Factory named New,
// and calls module.Register(Manifest, New) in init. app resolves the registry against the
// enabled set (Registry.Resolve), builds the modules in dependency order (Plan.Build) and
// starts and stops them (Set.Start, Set.Stop). The package performs no I/O of its own.
package module
