package lint

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// testYAML is a YAML document with known line numbers for testing
const testYAML = `# This is a test YAML file
package:
  name: test-package
  version: 1.2.3
  epoch: 0
  copyright:
    - license: MIT
  description: A test package

environment:
  contents:
    packages:
      - busybox

pipeline:
  - name: fetch
    runs: |
      wget -O ${{targets.subdir}}/source.tar.gz ${{vars.url}}
    # Missing expected-sha256 or expected-sha512

  - name: build
    runs: |
      echo "Building..."

update:
  enabled: true
  schedule:
    interval: weekly
`

func setupTestNode(t *testing.T) *yaml.Node {
	var node yaml.Node
	err := yaml.Unmarshal([]byte(testYAML), &node)
	if err != nil {
		t.Fatalf("Failed to parse test YAML: %v", err)
	}
	return &node
}

func TestFindNodeByPath(t *testing.T) {
	node := setupTestNode(t)

	tests := []struct {
		name     string
		path     string
		wantKind yaml.Kind
		wantNil  bool
	}{
		{
			name:     "root node",
			path:     "",
			wantKind: yaml.MappingNode,
			wantNil:  false,
		},
		{
			name:     "package node",
			path:     "package",
			wantKind: yaml.MappingNode,
			wantNil:  false,
		},
		{
			name:     "package.version node",
			path:     "package.version",
			wantKind: yaml.ScalarNode,
			wantNil:  false,
		},
		{
			name:     "package.copyright node",
			path:     "package.copyright",
			wantKind: yaml.SequenceNode,
			wantNil:  false,
		},
		{
			name:    "non-existent node",
			path:    "package.nonexistent",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindNodeByPath(node, tt.path)
			if (got == nil) != tt.wantNil {
				t.Errorf("FindNodeByPath() got = %v, wantNil = %v", got, tt.wantNil)
				return
			}
			if !tt.wantNil && got.Kind != tt.wantKind {
				t.Errorf("FindNodeByPath() got kind = %v, want kind = %v", got.Kind, tt.wantKind)
			}
		})
	}
}

func TestGetLineNumberForPath(t *testing.T) {
	node := setupTestNode(t)

	tests := []struct {
		name     string
		path     string
		wantLine int
	}{
		{
			name:     "package.name",
			path:     "package.name",
			wantLine: 3, // Line number from the test YAML
		},
		{
			name:     "package.version",
			path:     "package.version",
			wantLine: 4, // Line number from the test YAML
		},
		{
			name:     "non-existent path",
			path:     "package.nonexistent",
			wantLine: 0, // Should return 0 for non-existent paths
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLineNumberForPath(node, tt.path)
			if got != tt.wantLine {
				t.Errorf("GetLineNumberForPath() got = %v, want = %v", got, tt.wantLine)
			}
		})
	}
}

func TestGetLineNumberForMissingField(t *testing.T) {
	node := setupTestNode(t)

	tests := []struct {
		name     string
		path     string
		wantLine int
	}{
		{
			name:     "missing field in package",
			path:     "package.nonexistent",
			wantLine: 2, // Should return the line of the package node
		},
		{
			name:     "missing field in non-existent parent",
			path:     "nonexistent.field",
			wantLine: 0, // Should return 0 if parent doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLineNumberForMissingField(node, tt.path)
			if got != tt.wantLine {
				t.Errorf("GetLineNumberForMissingField() got = %v, want = %v", got, tt.wantLine)
			}
		})
	}
}

func TestGetPackageBlockLineNumber(t *testing.T) {
	node := setupTestNode(t)

	got := GetPackageBlockLineNumber(node)
	want := 2 // Line number of the package block in testYAML

	if got != want {
		t.Errorf("GetPackageBlockLineNumber() got = %v, want = %v", got, want)
	}

	// Test with nil node
	got = GetPackageBlockLineNumber(nil)
	want = 0 // Should return 0 for nil node

	if got != want {
		t.Errorf("GetPackageBlockLineNumber() with nil node got = %v, want = %v", got, want)
	}
}

func TestGetPipelineBlockLineNumber(t *testing.T) {
	node := setupTestNode(t)

	tests := []struct {
		name         string
		pipelineName string
		wantLine     int
	}{
		{
			name:         "fetch pipeline",
			pipelineName: "fetch",
			wantLine:     14, // Line number of the fetch pipeline in testYAML
		},
		{
			name:         "build pipeline",
			pipelineName: "build",
			wantLine:     19, // Line number of the build pipeline in testYAML
		},
		{
			name:         "non-existent pipeline",
			pipelineName: "nonexistent",
			wantLine:     13, // Should return the line of the pipeline section
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPipelineBlockLineNumber(node, tt.pipelineName)
			if got != tt.wantLine {
				t.Errorf("GetPipelineBlockLineNumber() got = %v, want = %v", got, tt.wantLine)
			}
		})
	}
}

func TestGetLineNumberForRule(t *testing.T) {
	node := setupTestNode(t)

	tests := []struct {
		name     string
		ruleName string
		wantLine int
	}{
		{
			name:     "contains-epoch rule",
			ruleName: "contains-epoch",
			wantLine: 5, // Line number of the epoch field
		},
		{
			name:     "bad-version rule",
			ruleName: "bad-version",
			wantLine: 4, // Line number of the version field
		},
		{
			name:     "valid-pipeline-fetch-digest rule",
			ruleName: "valid-pipeline-fetch-digest",
			wantLine: 14, // Line number of the fetch pipeline
		},
		{
			name:     "unknown rule",
			ruleName: "unknown-rule",
			wantLine: 2, // Should default to package block
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLineNumberForRule(node, tt.ruleName)
			if got != tt.wantLine {
				t.Errorf("GetLineNumberForRule() got = %v, want = %v", got, tt.wantLine)
			}
		})
	}
}
