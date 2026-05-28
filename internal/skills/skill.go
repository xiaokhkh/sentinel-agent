// Package skills is the registry for Sentinel's capability packs. A skill
// declares the kind of operations it understands and example invocations;
// packs register themselves from their own package init so the CLI can list
// what is available without importing each one explicitly.
package skills

// Skill describes a capability pack.
type Skill struct {
	Name        string
	Description string
	Examples    []string
	Status      string // stable | experimental | planned
}

var registry []Skill

// Register adds a skill to the global registry. Called from package init.
func Register(s Skill) { registry = append(registry, s) }

// All returns every registered skill.
func All() []Skill { return registry }
