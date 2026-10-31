package config

// Update is how waker looks for new releases.
type Update struct {
	// Check looks for a newer release at most once a day; a pointer so an
	// omitted key keeps its default (true).
	Check *bool `yaml:"check"`
}

// Checks reports whether waker looks for newer releases.
func (u Update) Checks() bool { return u.Check == nil || *u.Check }

// Effective returns the block with every key resolved.
func (u Update) Effective() Update {
	check := u.Checks()
	u.Check = &check
	return u
}
