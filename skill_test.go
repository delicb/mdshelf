package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEmbeddedSkillMatchesSource(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("skills", "mdshelf", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mdshelfSkill, source) {
		t.Fatal("embedded skill does not match skills/mdshelf/SKILL.md")
	}
}

func TestSkillPrintWritesExactSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"skill", "print"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), mdshelfSkill) {
		t.Fatalf("skill print output differs from embedded source")
	}
	if stderr.Len() != 0 {
		t.Fatalf("skill print stderr = %q", stderr.String())
	}
}

func TestSkillInstallCreatesAndKeepsCurrentSkill(t *testing.T) {
	root := t.TempDir()
	var first bytes.Buffer
	if err := runSkillCommand([]string{"install", root}, &first, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "mdshelf", "SKILL.md")
	installed, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, mdshelfSkill) {
		t.Fatal("installed skill differs from embedded source")
	}
	if !strings.Contains(first.String(), "Installed MDShelf skill") {
		t.Fatalf("first install output = %q", first.String())
	}

	var second bytes.Buffer
	if err := runSkillCommand([]string{"install", root}, &second, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "is current") {
		t.Fatalf("second install output = %q", second.String())
	}

	if runtime.GOOS != "windows" {
		directoryInfo, err := os.Stat(filepath.Dir(destination))
		if err != nil {
			t.Fatal(err)
		}
		fileInfo, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if directoryInfo.Mode().Perm() != 0o755 {
			t.Errorf("skill directory mode = %o", directoryInfo.Mode().Perm())
		}
		if fileInfo.Mode().Perm() != 0o644 {
			t.Errorf("skill file mode = %o", fileInfo.Mode().Perm())
		}
	}
}

func TestSkillInstallRejectsSymbolicLinkDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if !trySymlink(t, outside, filepath.Join(root, "mdshelf")) {
		return
	}

	err := runSkillCommand([]string{"install", root}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink install error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("outside skill file error = %v", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("outside directory mode = %o", info.Mode().Perm())
	}
}

func TestSkillInstallRequiresForceForDifferentContent(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "mdshelf", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSkillCommand([]string{"install", root}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("install without force error = %v", err)
	}
	unchanged, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "different\n" {
		t.Fatalf("different skill changed to %q", unchanged)
	}

	if err := runSkillCommand([]string{"install", "--force", root}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	replaced, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replaced, mdshelfSkill) {
		t.Fatal("forced install did not replace skill")
	}
}

func TestSkillHelpForms(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"print", "--help"}, {"install", "--help"}} {
		if err := runSkillCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Errorf("runSkillCommand(%q) error = %v", args, err)
		}
	}
}
