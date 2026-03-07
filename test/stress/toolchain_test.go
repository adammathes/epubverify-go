package stress_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolchainGenerateScript checks the generate-epubs.sh script exists and is executable.
func TestToolchainGenerateScript(t *testing.T) {
	script := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "generate-epubs.sh")
	info, err := os.Stat(script)
	if err != nil {
		t.Fatalf("generate-epubs.sh not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("generate-epubs.sh is not executable")
	}
}

// TestToolchainValidateScript checks the validate-epubs.sh script exists and is executable.
func TestToolchainValidateScript(t *testing.T) {
	script := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "validate-epubs.sh")
	info, err := os.Stat(script)
	if err != nil {
		t.Fatalf("validate-epubs.sh not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("validate-epubs.sh is not executable")
	}
}

// TestToolchainSourceContent checks that source content files exist for generation.
func TestToolchainSourceContent(t *testing.T) {
	srcDir := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "source-content")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("source-content directory not found: %v", err)
	}

	// Should have at least 5 source files
	if len(entries) < 5 {
		t.Errorf("expected at least 5 source content files, got %d", len(entries))
	}

	// Check for expected file types
	hasMarkdown := false
	hasHTML := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			hasMarkdown = true
		}
		if strings.HasSuffix(e.Name(), ".html") {
			hasHTML = true
		}
	}
	if !hasMarkdown {
		t.Error("no Markdown source files found")
	}
	if !hasHTML {
		t.Error("no HTML source files found")
	}
}

// TestToolchainSourceDiversity checks that source content covers multiple test dimensions.
func TestToolchainSourceDiversity(t *testing.T) {
	srcDir := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "source-content")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("source-content directory not found: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}

	expected := []string{
		"basic-prose.md",
		"multilingual.md",
		"math-content.md",
		"complex-structure.md",
		"minimal.md",
		"rich-html.html",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected source file %s not found", name)
		}
	}
}

// TestToolchainScriptFeatures checks that the generation script supports both pandoc and calibre.
func TestToolchainScriptFeatures(t *testing.T) {
	script := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "generate-epubs.sh")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("failed to read generate-epubs.sh: %v", err)
	}
	content := string(data)

	checks := map[string]string{
		"pandoc":        "pandoc",
		"calibre":       "ebook-convert",
		"--pandoc-only": "--pandoc-only",
		"--calibre-only": "--calibre-only",
		"--help":        "--help",
		"EPUB output":   "OUTPUT_DIR",
	}
	for desc, pattern := range checks {
		if !strings.Contains(content, pattern) {
			t.Errorf("generate-epubs.sh missing %s support (pattern: %s)", desc, pattern)
		}
	}
}

// TestToolchainValidateScriptFeatures checks the validation script has required features.
func TestToolchainValidateScriptFeatures(t *testing.T) {
	script := filepath.Join(repoRoot(), "stress-test", "toolchain-epubs", "validate-epubs.sh")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("failed to read validate-epubs.sh: %v", err)
	}
	content := string(data)

	checks := map[string]string{
		"epubverify":    "EPUBVERIFY",
		"epubcheck":     "EPUBCHECK_JAR",
		"JSON output":   "--json",
		"verdict match": "MATCH",
		"error analysis": "Error Code Analysis",
	}
	for desc, pattern := range checks {
		if !strings.Contains(content, pattern) {
			t.Errorf("validate-epubs.sh missing %s support (pattern: %s)", desc, pattern)
		}
	}
}
