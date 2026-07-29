package survey

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neutralvibes/daplabel/internal/discovery"
)

type treeNode struct {
	marker     byte
	text       string
	annotation string
	children   []treeNode
}

func branch(isLast bool) (connector, childPrefix string) {
	if isLast {
		return "└──", "    "
	}
	return "├──", "│   "
}

func writeNode(w io.Writer, prefix string, isLast bool, marker byte, text, annotation string) {
	conn, _ := branch(isLast)

	fmt.Fprint(w, prefix, conn)
	if marker != 0 {
		fmt.Fprintf(w, "%c ", marker)
	} else {
		fmt.Fprint(w, " ")
	}
	fmt.Fprint(w, text)
	if annotation != "" {
		fmt.Fprint(w, " ", annotation)
	}
	fmt.Fprintln(w)
}

func renderNode(w io.Writer, n treeNode, prefix string, blankBetween bool) {
	for i, child := range n.children {
		isLast := i == len(n.children)-1

		writeNode(w, prefix, isLast, child.marker, child.text, child.annotation)

		childPrefix := prefix
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
		renderNode(w, child, childPrefix, false)

		if blankBetween && !isLast {
			fmt.Fprintln(w, prefix+"│")
		}
	}
}

func buildTree(rootLabel string, reports []ProjectReport, warnings []discovery.Warning) treeNode {
	root := treeNode{text: rootLabel, children: make([]treeNode, 0)}

	type entry struct {
		name    string
		isWarn  bool
		warning discovery.Warning
		report  ProjectReport
	}
	var entries []entry
	for _, w := range warnings {
		entries = append(entries, entry{
			name:    filepath.Base(w.Path),
			isWarn:  true,
			warning: w,
		})
	}
	for _, r := range reports {
		entries = append(entries, entry{
			name:   r.ProjectName,
			report: r,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	for _, e := range entries {
		if e.isWarn {
			root.children = append(root.children, buildWarningNode(e.warning))
		} else {
			root.children = append(root.children, buildProjectNode(e.report))
		}
	}
	return root
}

func buildWarningNode(w discovery.Warning) treeNode {
	n := treeNode{
		text:       "[" + filepath.Base(w.Path) + "]",
		annotation: "!! " + w.Message,
		children:   make([]treeNode, 0),
	}
	for _, f := range w.Files {
		n.children = append(n.children, treeNode{
			marker: ':',
			text:   f,
		})
	}
	return n
}

func buildProjectNode(rpt ProjectReport) treeNode {
	n := treeNode{text: "[" + rpt.ProjectName + "]", children: make([]treeNode, 0)}

	for _, f := range rpt.Files {
		n.children = append(n.children, buildFileNode(f))
	}
	return n
}

func buildFileNode(f FileReport) treeNode {
	n := treeNode{
		marker:   ':',
		text:     filepath.Base(f.Path),
		children: make([]treeNode, 0),
	}
	if f.Err != nil {
		n.annotation = "!! ERROR: " + f.Err.Error()
		return n
	}
	for _, s := range f.Services {
		n.children = append(n.children, buildServiceNode(s))
	}
	return n
}

func buildServiceNode(s ServiceReport) treeNode {
	n := treeNode{text: s.Name, children: make([]treeNode, 0)}
	if s.Err != nil {
		n.annotation = "!! ERROR: " + s.Err.Error()
		return n
	}

	overrideConflict := make(map[string]bool, len(s.OverrideConflictKeys))
	for _, k := range s.OverrideConflictKeys {
		overrideConflict[k] = true
	}
	lfConflict := make(map[string]bool, len(s.LabelFileConflicts))
	for _, c := range s.LabelFileConflicts {
		lfConflict[c.Key] = true
	}

	if len(s.InlineLabels) == 0 && len(s.LabelFileRefs) == 0 {
		n.children = append(n.children, treeNode{text: textNoLabels})
		return n
	}

	for _, l := range s.InlineLabels {
		child := treeNode{
			marker: '>',
			text:   l.Key + "=" + l.Value,
		}
		if overrideConflict[l.Key] {
			child.annotation = "!! " + textOverrideConflict
		}
		n.children = append(n.children, child)
	}

	for _, lfr := range s.LabelFileRefs {
		refNode := treeNode{
			marker: '|',
			text:   lfr.Ref,
		}
		switch {
		case lfr.ReadErr != nil:
			refNode.annotation = "!! ERROR: " + lfr.ReadErr.Error()
		case !lfr.Exists:
			refNode.annotation = "!! " + textMissing
		default:
			refNode.children = make([]treeNode, 0, len(lfr.Labels))
			for _, l := range lfr.Labels {
				child := treeNode{
					marker: '>',
					text:   l.Key + "=" + l.Value,
				}
				if lfConflict[l.Key] {
					child.annotation = "!! " + textLabelFileConflict
				}
				refNode.children = append(refNode.children, child)
			}
		}
		n.children = append(n.children, refNode)
	}

	return n
}

func renderTree(w io.Writer, rootLabel string, reports []ProjectReport, warnings []discovery.Warning) {
	root := buildTree(rootLabel, reports, warnings)
	if root.text != "" {
		fmt.Fprintln(w, root.text)
	}
	renderNode(w, root, "", true)
}

func summaryBlock(bd breakdown, noSummary bool) string {
	if noSummary {
		return ""
	}
	var parts []string
	if bd.projectsWithErrors > 0 {
		parts = append(parts, plural(bd.projectsWithErrors, "error"))
	}
	if bd.skipped > 0 {
		parts = append(parts, plural(bd.skipped, "skipped"))
	}
	projLine := plural(bd.projects, "project")
	if len(parts) > 0 {
		projLine += " [" + strings.Join(parts, ", ") + "]"
	}

	svcLine := plural(bd.services, "service")
	if bd.servicesNoLabels > 0 {
		svcLine += fmt.Sprintf(" [%d no labels]", bd.servicesNoLabels)
	}

	return fmt.Sprintf("Summary:\n  %s\n  %s", projLine, svcLine)
}
