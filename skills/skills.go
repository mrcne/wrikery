// Package skills carries the agent skill the repository ships under skills/wrikery, in the Agent Skills format,
// https://agentskills.io/specification.
// The directory is a package because embed reaches no file outside the package directory and follows no symlink,
// and the folder the skill installers discover in the repository and the one the binary writes have to be the same files.
package skills

import "embed"

// Name is the skill's directory, which the format requires to equal the name in its frontmatter.
// The install command and the installers write it under a skills directory.
const Name = "wrikery"

// FS holds the folder, SKILL.md and references/json.md, under Name.
//
//go:embed wrikery
var FS embed.FS
