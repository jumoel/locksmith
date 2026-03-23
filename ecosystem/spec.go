package ecosystem

// SpecParser reads an ecosystem-specific spec file into a generic ProjectSpec.
type SpecParser interface {
	Parse(data []byte) (*ProjectSpec, error)
}
