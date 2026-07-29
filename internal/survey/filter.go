package survey

import "strings"

// KeyValueFilter is one --filter criterion: a service must have Key
// somewhere in its effective label set (see effectiveLabels), and if
// HasValue is true, at least one occurrence of Key must equal Value.
type KeyValueFilter struct {
	Key      string
	Value    string
	HasValue bool // false for a bare "KEY" filter (any value matches); true for "KEY=VALUE"
}

// ParseFilterArg parses a --filter argument, either "KEY" (match any
// value) or "KEY=VALUE" (match only that exact value).
func ParseFilterArg(arg string) KeyValueFilter {
	key, value, hasValue := strings.Cut(arg, "=")
	return KeyValueFilter{Key: key, Value: value, HasValue: hasValue}
}

// effectiveLabels returns every value seen for every key belonging to
// serviceName anywhere in rpt — inline labels in every file (base and
// override alike) plus every label_file reference's contents. This is
// deliberately an aggregate across sources: a user filtering for
// "does this service have diun.enable" doesn't care whether it came from
// an inline label or a label_file, and a key with conflicting values
// across sources (Decision 35 — survey reports conflicts, never resolves
// them) still counts as "has this key" for filtering purposes, with every
// value it was seen with available for a --filter KEY=VALUE match. This
// aggregate is used only for filter matching; the detailed report
// (ProjectReport/FileReport/ServiceReport) is unaffected and continues to
// show each source distinctly.
func effectiveLabels(rpt ProjectReport, serviceName string) map[string][]string {
	values := make(map[string][]string)
	for _, f := range rpt.Files {
		for _, s := range f.Services {
			if s.Name != serviceName {
				continue
			}
			for _, l := range s.InlineLabels {
				values[l.Key] = append(values[l.Key], l.Value)
			}
			for _, lfr := range s.LabelFileRefs {
				if lfr.ReadErr != nil || !lfr.Exists {
					continue
				}
				for _, l := range lfr.Labels {
					values[l.Key] = append(values[l.Key], l.Value)
				}
			}
		}
	}
	return values
}

// matchesFilters reports whether a service's effective label set
// (labels) satisfies every --filter criterion and lacks every --missing
// key. Both lists are AND-ed: all given filters must match, and the
// service must be missing all given --missing keys. An empty
// FilterOptions always matches (the no-filtering-requested default).
func matchesFilters(labels map[string][]string, filters []KeyValueFilter, missing []string) bool {
	for _, f := range filters {
		vals, ok := labels[f.Key]
		if !ok {
			return false
		}
		if f.HasValue {
			found := false
			for _, v := range vals {
				if v == f.Value {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, key := range missing {
		if _, ok := labels[key]; ok {
			return false
		}
	}
	return true
}

// filterProject returns a copy of rpt containing only the services that
// match filters/missing (see matchesFilters), and whether at least one
// service matched. A service that matches keeps its entries in every
// file it originally appeared in (e.g. both base and override), so the
// detailed report for a matching service is unaffected by filtering — only
// which services appear at all is affected.
func filterProject(rpt ProjectReport, filters []KeyValueFilter, missing []string) (ProjectReport, bool) {
	if len(filters) == 0 && len(missing) == 0 {
		return rpt, true // nothing to do — avoid needless copying on the common path
	}

	matched := make(map[string]bool)
	anyMatch := false
	seen := make(map[string]bool)
	for _, f := range rpt.Files {
		for _, s := range f.Services {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			if matchesFilters(effectiveLabels(rpt, s.Name), filters, missing) {
				matched[s.Name] = true
				anyMatch = true
			}
		}
	}

	out := rpt
	out.Files = make([]FileReport, len(rpt.Files))
	for i, f := range rpt.Files {
		nf := f
		nf.Services = nil
		for _, s := range f.Services {
			if matched[s.Name] {
				nf.Services = append(nf.Services, s)
			}
		}
		out.Files[i] = nf
	}
	return out, anyMatch
}
