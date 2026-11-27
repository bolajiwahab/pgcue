// Package knowledge embeds PostgreSQL topics, checks, and investigations.
package knowledge

import "embed"

// FS contains the topics and investigations collections.
// Embed the full tree so the loader can reject misplaced or misnamed YAML.
//
//go:embed *
var FS embed.FS
