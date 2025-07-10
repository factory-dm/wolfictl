package lint

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// FindNodeByPath locates a YAML node by its path, where path is a dot-separated
// string of keys (e.g., "package.version").
// Returns nil if the path doesn't exist.
func FindNodeByPath(root *yaml.Node, path string) *yaml.Node {
	if root == nil {
		return nil
	}

	// Skip document node if present
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	// Empty path returns the root node
	if path == "" {
		return node
	}

	parts := strings.Split(path, ".")
	return findNodeByPathParts(node, parts)
}

// findNodeByPathParts is a recursive helper for FindNodeByPath
func findNodeByPathParts(node *yaml.Node, parts []string) *yaml.Node {
	if len(parts) == 0 || node == nil {
		return node
	}

	// We only care about mapping nodes (key-value pairs)
	if node.Kind != yaml.MappingNode {
		return nil
	}

	// Look for the key in the mapping
	currentKey := parts[0]
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) && node.Content[i].Value == currentKey {
			// If this is the last part of the path, return the value node
			if len(parts) == 1 {
				return node.Content[i+1]
			}
			// Otherwise, continue recursively
			return findNodeByPathParts(node.Content[i+1], parts[1:])
		}
	}

	return nil
}

// GetLineNumberForPath returns the line number for a specific path in the YAML document.
// Returns 0 if the path doesn't exist.
func GetLineNumberForPath(root *yaml.Node, path string) int {
	node := FindNodeByPath(root, path)
	if node == nil {
		return 0
	}
	return node.Line
}

// GetLineNumberForMissingField attempts to infer the line number where a missing field
// would be expected to appear. It looks at surrounding fields to make an educated guess.
func GetLineNumberForMissingField(root *yaml.Node, path string) int {
	// Split the path to get the parent path and the missing field name
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return 0
	}

	missingField := parts[len(parts)-1]
	parentPath := strings.Join(parts[:len(parts)-1], ".")

	// Find the parent node
	parentNode := FindNodeByPath(root, parentPath)
	if parentNode == nil || parentNode.Kind != yaml.MappingNode {
		// If we can't find the parent, try to get the line number of the root node
		if root != nil && len(root.Content) > 0 {
			return root.Line
		}
		return 0
	}

	// Look for adjacent fields to infer where the missing field would be
	// First, try to find the line right after the parent's opening
	if len(parentNode.Content) > 0 {
		return parentNode.Line
	}

	// If we can't infer a specific location, return the parent's line number
	return parentNode.Line
}

// GetPackageBlockLineNumber returns the line number of the package block in a YAML document.
// This is useful for errors that apply to the entire package rather than a specific field.
func GetPackageBlockLineNumber(root *yaml.Node) int {
	// Try to find the "package" node first
	packageNode := FindNodeByPath(root, "package")
	if packageNode != nil {
		return packageNode.Line
	}

	// If no package node is found, return the document start line
	if root != nil {
		if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
			return root.Content[0].Line
		}
		return root.Line
	}

	return 0
}

// GetLocationInfo returns location information for a specific path in the YAML document.
// If the path doesn't exist, it attempts to infer where it would be expected.
func GetLocationInfo(root *yaml.Node, path string) LocationInfo {
	line := GetLineNumberForPath(root, path)
	if line == 0 {
		// Try to infer the line number for a missing field
		line = GetLineNumberForMissingField(root, path)
	}

	return LocationInfo{
		Line: line,
		Path: path,
	}
}

// GetPipelineBlockLineNumber returns the line number for a specific pipeline in the YAML document.
// This is useful for errors related to pipeline configurations.
func GetPipelineBlockLineNumber(root *yaml.Node, pipelineName string) int {
	// Try to find the pipeline node
	pipelineNode := FindNodeByPath(root, "pipeline")
	if pipelineNode == nil || pipelineNode.Kind != yaml.SequenceNode {
		return GetPackageBlockLineNumber(root)
	}

	// Look for the specific pipeline by name
	for i := 0; i < len(pipelineNode.Content); i++ {
		node := pipelineNode.Content[i]
		if node.Kind != yaml.MappingNode {
			continue
		}

		// Look for the name field in this pipeline
		for j := 0; j < len(node.Content); j += 2 {
			if j+1 < len(node.Content) && 
			   node.Content[j].Value == "name" && 
			   node.Content[j+1].Value == pipelineName {
				return node.Line
			}
		}
	}

	// If we can't find the specific pipeline, return the pipeline section line
	return pipelineNode.Line
}

// rulePathLookup maps lint rule names to the YAML path that should be
// inspected when determining a line number.  The path should be expressed
// in dot-notation (e.g. "package.version").
var rulePathLookup = map[string]string{
	"contains-epoch":               "package.epoch",
	"bad-version":                  "package.version",
	"valid-version":                "package.version",
	"valid-license":                "package.copyright",
	"valid-copyright":              "package.copyright",
	"package-name-matches-filename":"package.name",
	"valid-update-schedule":        "update.schedule",
	"valid-automatic-update":       "update",
	// add more direct path mappings here as needed
}

// GetLineNumberForRule attempts to locate a sensible line number for a
// particular lint rule.  It first checks for any rule-specific logic that
// cannot be expressed as a simple path lookup, then falls back to a mapping
// lookup, and finally to the package block if nothing better is found.
func GetLineNumberForRule(root *yaml.Node, ruleName string) int {
	switch ruleName {
	case "valid-pipeline-fetch-digest":
		// The fetch pipeline rule generally applies to the `fetch` pipeline
		return GetPipelineBlockLineNumber(root, "fetch")
	}

	// Generic path lookup based on the mapping table
	if path, ok := rulePathLookup[ruleName]; ok {
		line := GetLineNumberForPath(root, path)
		if line == 0 {
			line = GetLineNumberForMissingField(root, path)
		}
		if line != 0 {
			return line
		}
	}

	// Fallback – use the top of the package block
	return GetPackageBlockLineNumber(root)
}
