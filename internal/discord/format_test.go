package discord

import (
	"strings"
	"testing"
)

func TestFormatForDiscord(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		check    func(t *testing.T, got string) // optional custom check
	}{
		{
			name:     "plain text preserved",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "bold preserved",
			input:    "**bold**",
			expected: "**bold**",
		},
		{
			name:     "italic preserved",
			input:    "*italic*",
			expected: "*italic*",
		},
		{
			name:     "code block preserved",
			input:    "```go\nfmt.Println()\n```",
			expected: "```go\nfmt.Println()\n```",
		},
		{
			name:     "inline code preserved",
			input:    "`code`",
			expected: "`code`",
		},
		{
			name:  "table converted to list format",
			input: "| name | value |\n|------|-------|\n| CPU  | 45°C  |",
			check: func(t *testing.T, got string) {
				// Should NOT contain raw pipe-delimited table structure
				if strings.Contains(got, "| name | value |") {
					t.Errorf("output should not contain raw table row, got:\n%s", got)
				}
				// Should contain list-formatted entries
				if !strings.Contains(got, "- **name**: value") {
					t.Errorf("output should contain '- **name**: value', got:\n%s", got)
				}
				if !strings.Contains(got, "- **CPU**: 45°C") {
					t.Errorf("output should contain '- **CPU**: 45°C', got:\n%s", got)
				}
				// Should NOT contain the separator row
				if strings.Contains(got, "---|---") {
					t.Errorf("output should not contain table separator row, got:\n%s", got)
				}
			},
		},
		{
			name:     "header # converted to bold",
			input:    "# Heading",
			expected: "**Heading**",
		},
		{
			name:     "header ## converted to bold",
			input:    "## Subheading",
			expected: "**Subheading**",
		},
		{
			name:     "header ### converted to bold",
			input:    "### Sub",
			expected: "**Sub**",
		},
		{
			name:  "HTML tags stripped",
			input: "<b>text</b>",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "<") || strings.Contains(got, ">") {
					t.Errorf("output should not contain HTML angle brackets, got: %q", got)
				}
				if !strings.Contains(got, "text") {
					t.Errorf("output should contain 'text', got: %q", got)
				}
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "code block with pipes preserved as-is",
			input:    "```\n| a | b |\n|---|---|\n| 1 | 2 |\n```",
			expected: "```\n| a | b |\n|---|---|\n| 1 | 2 |\n```",
		},
		{
			name:  "mixed content with header, table, and code block",
			input: "# Results\n\n| Name | Value |\n|------|-------|\n| CPU  | 45°C  |\n\n```go\nfmt.Println(\"hi\")\n```",
			check: func(t *testing.T, got string) {
				// Header converted
				if !strings.Contains(got, "**Results**") {
					t.Errorf("output should contain '**Results**', got:\n%s", got)
				}
				// Table converted
				if !strings.Contains(got, "- **Name**: Value") {
					t.Errorf("output should contain '- **Name**: Value', got:\n%s", got)
				}
				if !strings.Contains(got, "- **CPU**: 45°C") {
					t.Errorf("output should contain '- **CPU**: 45°C', got:\n%s", got)
				}
				// Code block preserved
				if !strings.Contains(got, "```go") {
					t.Errorf("output should contain code block fence, got:\n%s", got)
				}
				if !strings.Contains(got, `fmt.Println("hi")`) {
					t.Errorf("output should contain code content, got:\n%s", got)
				}
			},
		},
		{
			name:  "nested list flattened",
			input: "- parent\n  - child",
			check: func(t *testing.T, got string) {
				// Should not have double-space indented list items
				if strings.Contains(got, "  -") {
					t.Errorf("output should not contain indented nested list items, got:\n%s", got)
				}
				// Both items should still be present
				if !strings.Contains(got, "- parent") {
					t.Errorf("output should contain '- parent', got:\n%s", got)
				}
				if !strings.Contains(got, "- child") {
					t.Errorf("output should contain '- child', got:\n%s", got)
				}
			},
		},
		{
			name:  "multiple tables both converted",
			input: "| A | B |\n|---|---|\n| 1 | 2 |\n\n| C | D |\n|---|---|\n| 3 | 4 |",
			check: func(t *testing.T, got string) {
				// First table converted
				if !strings.Contains(got, "- **A**: B") {
					t.Errorf("output should contain '- **A**: B', got:\n%s", got)
				}
				if !strings.Contains(got, "- **1**: 2") {
					t.Errorf("output should contain '- **1**: 2', got:\n%s", got)
				}
				// Second table converted
				if !strings.Contains(got, "- **C**: D") {
					t.Errorf("output should contain '- **C**: D', got:\n%s", got)
				}
				if !strings.Contains(got, "- **3**: 4") {
					t.Errorf("output should contain '- **3**: 4', got:\n%s", got)
				}
				// No raw table rows
				if strings.Contains(got, "| A | B |") {
					t.Errorf("output should not contain raw table row, got:\n%s", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatForDiscord(tt.input)
			if tt.check != nil {
				tt.check(t, got)
			} else if got != tt.expected {
				t.Errorf("formatForDiscord() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}