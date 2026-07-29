package survey

// Every fixed piece of wording either renderer prints lives here, in one
// place, specifically so it can be changed without hunting through
// render_tree.go/render_plain.go's formatting logic.
const (
	textNoLabels            = "(no labels)"
	textNoServicesFound     = "(no services found)"
	textMissing             = "MISSING"
	textOverrideConflict    = "OVERRIDES BASE FILE KEY"
	textLabelFileConflict   = "CONFLICTS ACROSS LABEL FILES (not resolved)"
	textConflictSectionHead = "Conflicting keys across label files (not resolved here — see --on-conflict for add/remove):"
	textInlineTag           = "[inline]"
)
