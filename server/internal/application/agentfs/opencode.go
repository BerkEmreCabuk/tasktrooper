package agentfs

import "fmt"

const FlavorOpencode Flavor = "opencode"

func renderOpencode(b Bundle) []file {
	skills := usableSkills(b.Skills)
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	slugs := uniqueNames(names)

	files := make([]file, 0, len(skills))
	for i, s := range skills {
		files = append(files, file{
			rel:  fmt.Sprintf("%s/%s/%s", claudeSkillsDir, slugs[i], claudeSkillFile),
			body: claudeSkill(slugs[i], s, b.stackName(s)),
		})
	}
	return files
}
