package config

// meta is one field's metadata as a Metadata() map carries it. The yaml names
// are the contract with yedit's spec.FieldMeta: a mistyped field does not
// build, and a name yedit does not know fails at startup.
type meta struct {
	Description string   `yaml:"description,omitempty"`
	Type        string   `yaml:"type,omitempty"`
	Required    bool     `yaml:"required,omitempty"`
	Default     string   `yaml:"default,omitempty"`
	Example     string   `yaml:"example,omitempty"`
	OneOf       []string `yaml:"oneof,omitempty"`
	Pattern     string   `yaml:"pattern,omitempty"` // RE2
	Formats     []string `yaml:"formats,omitempty"` // format names, OR semantics
	MinCount    int      `yaml:"mincount,omitempty"`
	Unique      bool     `yaml:"unique,omitempty"`
}
