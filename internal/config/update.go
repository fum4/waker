package config

// Update is how waker looks for, and optionally installs, new releases.
// Both keys are pointers so an omitted key keeps its default.
type Update struct {
	// Check looks for a newer release once a day (default true).
	Check *bool `yaml:"check"`
	// Auto installs it at serve's start, with rollback (default false).
	Auto *bool `yaml:"auto"`
}

// Checks reports whether waker looks for newer releases.
func (u Update) Checks() bool { return u.Check == nil || *u.Check }

// Autos reports whether serve installs newer releases itself.
func (u Update) Autos() bool { return u.Auto != nil && *u.Auto }

// Effective returns the block with every key resolved.
func (u Update) Effective() Update {
	check, auto := u.Checks(), u.Autos()
	u.Check, u.Auto = &check, &auto
	return u
}
