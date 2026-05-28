package skills

// Planned skills from the roadmap. They appear in `guard skills` as planned so
// the surface area is visible before the packs land.
func init() {
	Register(Skill{Name: "database", Description: "MySQL/PostgreSQL inspection and guarded queries", Status: "planned"})
	Register(Skill{Name: "cloud", Description: "AWS / Aliyun CLI assistance", Status: "planned"})
	Register(Skill{Name: "git", Description: "repository operations with force-push guarding", Status: "planned"})
}
