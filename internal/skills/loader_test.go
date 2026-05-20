package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSkillDir creates a skill directory with a SKILL.md file.
func makeSkillDir(t *testing.T, parent, slug, content string) string {
	t.Helper()
	dir := filepath.Join(parent, slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("makeSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("makeSkillDir write: %v", err)
	}
	return dir
}

// --- ListSkills ---

func TestLoader_ListSkills_ZeroSkills(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, "", "")

	skills := l.ListSkills(context.Background())
	if len(skills) != 0 {
		t.Errorf("empty workspace should have 0 skills, got %d", len(skills))
	}
}

func TestLoader_ListSkills_SingleSkill(t *testing.T) {
	ws := t.TempDir()
	skillsDir := filepath.Join(ws, "skills")
	makeSkillDir(t, skillsDir, "my-tool", "---\nname: My Tool\ndescription: Does stuff\n---\n# My Tool\n")

	l := NewLoader(ws, "", "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Slug != "my-tool" {
		t.Errorf("slug: got %q", skills[0].Slug)
	}
	if skills[0].Name != "My Tool" {
		t.Errorf("name: got %q", skills[0].Name)
	}
	if skills[0].Description != "Does stuff" {
		t.Errorf("description: got %q", skills[0].Description)
	}
	if skills[0].Source != "workspace" {
		t.Errorf("source: got %q", skills[0].Source)
	}
}

func TestLoader_ListSkills_MultipleSkills(t *testing.T) {
	ws := t.TempDir()
	skillsDir := filepath.Join(ws, "skills")
	makeSkillDir(t, skillsDir, "skill-a", "---\nname: Skill A\n---\n")
	makeSkillDir(t, skillsDir, "skill-b", "---\nname: Skill B\n---\n")
	makeSkillDir(t, skillsDir, "skill-c", "---\nname: Skill C\n---\n")

	l := NewLoader(ws, "", "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(skills))
	}
}

func TestLoader_ListSkills_IgnoresNonDirs(t *testing.T) {
	ws := t.TempDir()
	skillsDir := filepath.Join(ws, "skills")
	os.MkdirAll(skillsDir, 0755)

	// Create a plain file (not a dir) — should be ignored
	os.WriteFile(filepath.Join(skillsDir, "not-a-dir.md"), []byte("content"), 0644)
	// Create a valid skill dir
	makeSkillDir(t, skillsDir, "valid-skill", "---\nname: Valid\n---\n")

	l := NewLoader(ws, "", "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Errorf("expected 1 skill (files ignored), got %d", len(skills))
	}
}

func TestLoader_ListSkills_IgnoresDirWithoutSKILLmd(t *testing.T) {
	ws := t.TempDir()
	skillsDir := filepath.Join(ws, "skills")
	os.MkdirAll(skillsDir, 0755)

	// A dir without SKILL.md should be ignored
	os.MkdirAll(filepath.Join(skillsDir, "no-skill-md"), 0755)
	makeSkillDir(t, skillsDir, "real-skill", "---\nname: Real\n---\n")

	l := NewLoader(ws, "", "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d: %+v", len(skills), skills)
	}
}

func TestLoader_ListSkills_PriorityWorkspaceOverGlobal(t *testing.T) {
	ws := t.TempDir()
	global := t.TempDir()

	// Same slug in both workspace and global
	makeSkillDir(t, filepath.Join(ws, "skills"), "shared-skill", "---\nname: From Workspace\n---\n")
	makeSkillDir(t, global, "shared-skill", "---\nname: From Global\n---\n")

	l := NewLoader(ws, global, "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Errorf("expected 1 skill (deduplication), got %d", len(skills))
	}
	if skills[0].Name != "From Workspace" {
		t.Errorf("workspace should take priority, got %q", skills[0].Name)
	}
}

func TestLoader_ListSkills_GlobalSkills(t *testing.T) {
	global := t.TempDir()
	makeSkillDir(t, global, "global-skill", "---\nname: Global\ndescription: global tool\n---\n")

	l := NewLoader("", global, "")
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Fatalf("expected 1 global skill, got %d", len(skills))
	}
	if skills[0].Source != "global" {
		t.Errorf("source: got %q, want global", skills[0].Source)
	}
}

func TestLoader_ListSkills_BuiltinSkills(t *testing.T) {
	builtin := t.TempDir()
	makeSkillDir(t, builtin, "builtin-skill", "---\nname: Builtin\n---\n")

	l := NewLoader("", "", builtin)
	skills := l.ListSkills(context.Background())

	if len(skills) != 1 {
		t.Fatalf("expected 1 builtin skill, got %d", len(skills))
	}
	if skills[0].Source != "builtin" {
		t.Errorf("source: got %q, want builtin", skills[0].Source)
	}
}

// --- LoadSkill ---

func TestLoader_LoadSkill_Found(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "my-skill", "---\nname: My Skill\n---\n# Content here\nDo something useful.")

	l := NewLoader(ws, "", "")
	content, ok := l.LoadSkill(context.Background(), "my-skill")

	if !ok {
		t.Fatal("expected skill to be found")
	}
	if !strings.Contains(content, "Content here") {
		t.Errorf("expected content body, got %q", content)
	}
	// Frontmatter should be stripped
	if strings.Contains(content, "---") {
		t.Errorf("frontmatter should be stripped, got %q", content)
	}
}

func TestLoader_LoadSkill_NotFound(t *testing.T) {
	l := NewLoader("", "", "")
	_, ok := l.LoadSkill(context.Background(), "nonexistent")
	if ok {
		t.Error("nonexistent skill should return false")
	}
}

func TestLoader_LoadSkill_BaseDirPlaceholder(t *testing.T) {
	ws := t.TempDir()
	skillDir := makeSkillDir(t, filepath.Join(ws, "skills"), "my-skill",
		"---\nname: My Skill\n---\nScript at: {baseDir}/run.sh")

	l := NewLoader(ws, "", "")
	content, ok := l.LoadSkill(context.Background(), "my-skill")

	if !ok {
		t.Fatal("expected skill to be found")
	}
	if strings.Contains(content, "{baseDir}") {
		t.Errorf("{baseDir} should be replaced, got %q", content)
	}
	if !strings.Contains(content, skillDir) {
		t.Errorf("expected skill dir %q in content, got %q", skillDir, content)
	}
}

// --- LoadForContext ---

func TestLoader_LoadForContext_Empty(t *testing.T) {
	l := NewLoader("", "", "")
	result := l.LoadForContext(context.Background(), nil)
	if result != "" {
		t.Errorf("empty loader should return empty string, got %q", result)
	}
}

func TestLoader_LoadForContext_AllSkills(t *testing.T) {
	ws := t.TempDir()
	// Use slug == name so LoadSkill lookup by name succeeds (LoadForContext passes s.Name to LoadSkill).
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-a", "---\nname: skill-a\ndescription: Tool A\n---\nContent A")
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-b", "---\nname: skill-b\ndescription: Tool B\n---\nContent B")

	l := NewLoader(ws, "", "")
	result := l.LoadForContext(context.Background(), nil)

	if !strings.Contains(result, "Available Skills") {
		t.Errorf("expected '## Available Skills' header, got %q", result)
	}
	if !strings.Contains(result, "skill-a") {
		t.Errorf("expected skill-a in output")
	}
	if !strings.Contains(result, "skill-b") {
		t.Errorf("expected skill-b in output")
	}
}

func TestLoader_LoadForContext_AllowList(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-a", "---\nname: skill-a\n---\nContent A")
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-b", "---\nname: skill-b\n---\nContent B")

	l := NewLoader(ws, "", "")
	// allowList uses skill names (same as slug when name==slug in frontmatter)
	result := l.LoadForContext(context.Background(), []string{"skill-a"})

	if !strings.Contains(result, "skill-a") {
		t.Error("expected skill-a in output")
	}
	if strings.Contains(result, "skill-b") {
		t.Error("skill-b should not appear when not in allowList")
	}
}

// --- BuildSummary ---

func TestLoader_BuildSummary_Empty(t *testing.T) {
	l := NewLoader("", "", "")
	result := l.BuildSummary(context.Background(), nil)
	if result != "" {
		t.Errorf("empty loader BuildSummary should return empty, got %q", result)
	}
}

func TestLoader_BuildSummary_XMLFormat(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "web-search",
		"---\nname: Web Search\ndescription: Search the web\n---\n")

	l := NewLoader(ws, "", "")
	result := l.BuildSummary(context.Background(), nil)

	if !strings.Contains(result, "<available_skills>") {
		t.Error("expected <available_skills> root element")
	}
	if !strings.Contains(result, "<skill>") {
		t.Error("expected <skill> element")
	}
	if !strings.Contains(result, "Web Search") {
		t.Error("expected skill name in summary")
	}
	if !strings.Contains(result, "</available_skills>") {
		t.Error("expected closing tag")
	}
}

func TestLoader_BuildSummary_XMLEscaping(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "xml-skill",
		"---\nname: \"Tool <with> &special& chars\"\ndescription: \"A & B < C > D\"\n---\n")

	l := NewLoader(ws, "", "")
	result := l.BuildSummary(context.Background(), nil)

	// Raw < and & should be escaped in XML output
	if strings.Contains(result, "<with>") {
		t.Error("< should be escaped in XML output")
	}
	if strings.Contains(result, "&special&") {
		t.Error("& should be escaped in XML output")
	}
}

// --- InlineBody opt-in (GOCLAW_INLINE_BODY) ---

// TestLoader_ParseMetadata_InlineBodyFlag verifies that both YAML and JSON
// frontmatter forms parse the `inline_body` flag correctly.
func TestLoader_ParseMetadata_InlineBodyFlag(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "yaml_true",
			content: "---\nname: yaml-true\ndescription: x\ninline_body: true\n---\nbody",
			want:    true,
		},
		{
			name:    "yaml_false",
			content: "---\nname: yaml-false\ndescription: x\ninline_body: false\n---\nbody",
			want:    false,
		},
		{
			name:    "yaml_absent",
			content: "---\nname: yaml-absent\ndescription: x\n---\nbody",
			want:    false,
		},
		{
			name:    "yaml_garbage_defaults_false",
			content: "---\nname: yaml-garbage\ndescription: x\ninline_body: nottrue\n---\nbody",
			want:    false,
		},
		{
			name:    "json_true",
			content: "---\n{\"name\":\"json-true\",\"description\":\"x\",\"inline_body\":true}\n---\nbody",
			want:    true,
		},
		{
			name:    "json_false",
			content: "---\n{\"name\":\"json-false\",\"description\":\"x\",\"inline_body\":false}\n---\nbody",
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "SKILL.md")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			meta := parseMetadata(path)
			if meta == nil {
				t.Fatal("expected non-nil metadata")
			}
			if meta.InlineBody != tc.want {
				t.Errorf("InlineBody = %v, want %v", meta.InlineBody, tc.want)
			}
		})
	}
}

// TestLoader_BuildSummary_InlineBodyOptIn verifies the variadic includeBody
// arg gates the body injection, and only skills with `inline_body: true` are
// inlined when the flag is set.
func TestLoader_BuildSummary_InlineBodyOptIn(t *testing.T) {
	ws := t.TempDir()
	// Opted-in skill: inline_body: true. Body marker is "PINNED_BODY_MARKER".
	makeSkillDir(t, filepath.Join(ws, "skills"), "pinned-opt-in",
		"---\nname: pinned-opt-in\ndescription: opted in\ninline_body: true\n---\nPINNED_BODY_MARKER\n")
	// Opted-out skill: no flag. Body marker is "OPTED_OUT_BODY_MARKER".
	makeSkillDir(t, filepath.Join(ws, "skills"), "pinned-opt-out",
		"---\nname: pinned-opt-out\ndescription: not opted in\n---\nOPTED_OUT_BODY_MARKER\n")

	l := NewLoader(ws, "", "")
	names := []string{"pinned-opt-in", "pinned-opt-out"}

	// Without flag: never inline any body.
	noFlag := l.BuildSummary(context.Background(), names)
	if strings.Contains(noFlag, "<body>") {
		t.Errorf("BuildSummary(ctx,names) without flag must not include <body>; got:\n%s", noFlag)
	}
	if strings.Contains(noFlag, "PINNED_BODY_MARKER") || strings.Contains(noFlag, "OPTED_OUT_BODY_MARKER") {
		t.Errorf("BuildSummary(ctx,names) without flag must not include body content; got:\n%s", noFlag)
	}

	// With flag=true: inline only opted-in skill.
	withFlag := l.BuildSummary(context.Background(), names, true)
	if !strings.Contains(withFlag, "<body>") {
		t.Errorf("BuildSummary(ctx,names,true) must include <body> for opted-in skill; got:\n%s", withFlag)
	}
	if !strings.Contains(withFlag, "PINNED_BODY_MARKER") {
		t.Errorf("opted-in body content missing; got:\n%s", withFlag)
	}
	if strings.Contains(withFlag, "OPTED_OUT_BODY_MARKER") {
		t.Errorf("opted-out skill body must not be inlined; got:\n%s", withFlag)
	}

	// With flag=false: behaves like no flag.
	withFalse := l.BuildSummary(context.Background(), names, false)
	if strings.Contains(withFalse, "<body>") {
		t.Errorf("BuildSummary(ctx,names,false) must not include <body>; got:\n%s", withFalse)
	}
}

// TestLoader_BuildSummary_InlineBodyTruncation verifies bodies > inlineBodyMaxBytes
// are truncated with the marker, while smaller bodies are emitted whole.
func TestLoader_BuildSummary_InlineBodyTruncation(t *testing.T) {
	ws := t.TempDir()

	// Small body (< 8KB): emitted whole.
	smallBody := strings.Repeat("a", 100)
	makeSkillDir(t, filepath.Join(ws, "skills"), "small",
		"---\nname: small\ndescription: s\ninline_body: true\n---\n"+smallBody+"\n")

	// Large body (> 8KB): must be truncated with marker.
	largeBody := strings.Repeat("X", inlineBodyMaxBytes+500)
	makeSkillDir(t, filepath.Join(ws, "skills"), "large",
		"---\nname: large\ndescription: l\ninline_body: true\n---\n"+largeBody+"\n")

	l := NewLoader(ws, "", "")
	out := l.BuildSummary(context.Background(), []string{"small", "large"}, true)

	// Small body is emitted whole.
	if !strings.Contains(out, smallBody) {
		t.Errorf("small body must be emitted whole; got:\n%s", out)
	}
	// Truncation marker present for large body.
	if !strings.Contains(out, "[… body truncated …]") {
		t.Errorf("large body must include truncation marker; got tail:\n%s", out[max(0, len(out)-400):])
	}
	// Large body must not be emitted whole — we should not see the entire over-cap string.
	if strings.Contains(out, largeBody) {
		t.Errorf("large body should be truncated, not emitted whole")
	}
}

// TestLoader_BuildPinnedSummary_AlwaysInlinesOptedIn proves BuildPinnedSummary
// always passes the includeBody flag to BuildSummary (i.e. opted-in skills
// always get their body inlined when pinned).
func TestLoader_BuildPinnedSummary_AlwaysInlinesOptedIn(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "pin-me",
		"---\nname: pin-me\ndescription: opted in\ninline_body: true\n---\nPIN_BODY_MARKER\n")
	makeSkillDir(t, filepath.Join(ws, "skills"), "no-inline",
		"---\nname: no-inline\ndescription: not opted in\n---\nNO_INLINE_MARKER\n")

	l := NewLoader(ws, "", "")
	out := l.BuildPinnedSummary(context.Background(), []string{"pin-me", "no-inline"})

	if !strings.Contains(out, "<body>") {
		t.Errorf("BuildPinnedSummary must inline opted-in bodies; got:\n%s", out)
	}
	if !strings.Contains(out, "PIN_BODY_MARKER") {
		t.Errorf("opted-in body content must appear; got:\n%s", out)
	}
	if strings.Contains(out, "NO_INLINE_MARKER") {
		t.Errorf("opted-out skill body must not be inlined; got:\n%s", out)
	}
}

// --- FilterSkills ---

func TestLoader_FilterSkills_NilAllowList(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "a", "---\nname: A\n---\n")
	makeSkillDir(t, filepath.Join(ws, "skills"), "b", "---\nname: B\n---\n")

	l := NewLoader(ws, "", "")
	result := l.FilterSkills(context.Background(), nil)
	if len(result) != 2 {
		t.Errorf("nil allowList should return all skills, got %d", len(result))
	}
}

func TestLoader_FilterSkills_EmptyAllowList(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "a", "---\nname: A\n---\n")

	l := NewLoader(ws, "", "")
	result := l.FilterSkills(context.Background(), []string{})
	if len(result) != 0 {
		t.Errorf("empty allowList should return 0 skills, got %d", len(result))
	}
}

func TestLoader_FilterSkills_SpecificSkill(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-a", "---\nname: A\n---\n")
	makeSkillDir(t, filepath.Join(ws, "skills"), "skill-b", "---\nname: B\n---\n")

	l := NewLoader(ws, "", "")
	result := l.FilterSkills(context.Background(), []string{"skill-a"})
	if len(result) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(result))
	}
	if result[0].Slug != "skill-a" {
		t.Errorf("expected skill-a, got %q", result[0].Slug)
	}
}

// --- GetSkill ---

func TestLoader_GetSkill_Found(t *testing.T) {
	ws := t.TempDir()
	makeSkillDir(t, filepath.Join(ws, "skills"), "my-skill", "---\nname: My Skill\n---\n")

	l := NewLoader(ws, "", "")
	info, ok := l.GetSkill(context.Background(), "my-skill")
	if !ok {
		t.Fatal("expected skill to be found")
	}
	if info.Slug != "my-skill" {
		t.Errorf("slug: got %q", info.Slug)
	}
}

func TestLoader_GetSkill_NotFound(t *testing.T) {
	l := NewLoader("", "", "")
	_, ok := l.GetSkill(context.Background(), "nonexistent")
	if ok {
		t.Error("nonexistent skill should return false")
	}
}

// --- Version / BumpVersion ---

func TestLoader_Version(t *testing.T) {
	l := NewLoader("", "", "")
	v1 := l.Version()

	l.BumpVersion()
	v2 := l.Version()

	if v2 <= v1 {
		t.Errorf("BumpVersion should increase version: %d -> %d", v1, v2)
	}
}

// --- Frontmatter parsing ---

func TestExtractFrontmatter_Valid(t *testing.T) {
	content := "---\nname: Test Tool\ndescription: Does something\n---\n# Body"
	fm := extractFrontmatter(content)
	if !strings.Contains(fm, "name: Test Tool") {
		t.Errorf("expected frontmatter content, got %q", fm)
	}
}

func TestExtractFrontmatter_Missing(t *testing.T) {
	content := "# Just a body\nNo frontmatter here."
	fm := extractFrontmatter(content)
	if fm != "" {
		t.Errorf("expected empty frontmatter, got %q", fm)
	}
}

func TestStripFrontmatter(t *testing.T) {
	content := "---\nname: Test\n---\n# Body\nContent here."
	result := stripFrontmatter(content)
	if strings.Contains(result, "---") {
		t.Errorf("frontmatter should be stripped, got %q", result)
	}
	if !strings.Contains(result, "Body") {
		t.Errorf("body should remain, got %q", result)
	}
}

func TestStripFrontmatter_NonePresent(t *testing.T) {
	content := "# Just body"
	result := stripFrontmatter(content)
	if result != content {
		t.Errorf("no frontmatter: content should be unchanged, got %q", result)
	}
}

// --- parseSimpleYAML ---

func TestParseSimpleYAML_BasicKV(t *testing.T) {
	yaml := "name: My Tool\ndescription: Does something\n"
	kv := parseSimpleYAML(yaml)

	if kv["name"] != "My Tool" {
		t.Errorf("name: got %q", kv["name"])
	}
	if kv["description"] != "Does something" {
		t.Errorf("description: got %q", kv["description"])
	}
}

func TestParseSimpleYAML_QuotedValues(t *testing.T) {
	yaml := "name: \"Quoted Name\"\ndescription: 'Single quoted'\n"
	kv := parseSimpleYAML(yaml)

	if kv["name"] != "Quoted Name" {
		t.Errorf("name: got %q", kv["name"])
	}
	if kv["description"] != "Single quoted" {
		t.Errorf("description: got %q", kv["description"])
	}
}

func TestParseSimpleYAML_Empty(t *testing.T) {
	kv := parseSimpleYAML("")
	if len(kv) != 0 {
		t.Errorf("empty yaml should return empty map, got %v", kv)
	}
}

func TestParseSimpleYAML_CommentLines(t *testing.T) {
	yaml := "# This is a comment\nname: Tool\n"
	kv := parseSimpleYAML(yaml)
	if kv["name"] != "Tool" {
		t.Errorf("expected name=Tool after comment, got %q", kv["name"])
	}
	if _, ok := kv["# This is a comment"]; ok {
		t.Error("comment line should not be a key")
	}
}

// --- parseMetadata ---

func TestParseMetadata_ValidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte("---\nname: Test Skill\ndescription: A test skill\n---\n# Body"), 0644)

	meta := parseMetadata(path)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.Name != "Test Skill" {
		t.Errorf("name: got %q", meta.Name)
	}
	if meta.Description != "A test skill" {
		t.Errorf("description: got %q", meta.Description)
	}
}

func TestParseMetadata_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte("# Body without frontmatter"), 0644)

	meta := parseMetadata(path)
	if meta == nil {
		t.Fatal("expected non-nil metadata even without frontmatter")
	}
	// Name should fall back to directory name
	if meta.Name == "" {
		t.Error("name should fall back to directory name")
	}
}

func TestParseMetadata_FileNotFound(t *testing.T) {
	meta := parseMetadata("/nonexistent/path/SKILL.md")
	if meta != nil {
		t.Errorf("nonexistent file should return nil metadata, got %+v", meta)
	}
}

// --- normalizeLineEndings ---

func TestNormalizeLineEndings(t *testing.T) {
	crlf := "line1\r\nline2\r\nline3"
	got := normalizeLineEndings(crlf)
	if strings.Contains(got, "\r") {
		t.Errorf("normalizeLineEndings should remove \\r, got %q", got)
	}
	if got != "line1\nline2\nline3" {
		t.Errorf("expected unix line endings, got %q", got)
	}
}

// --- Managed skills versioning ---

func TestLoader_ManagedSkills_LatestVersion(t *testing.T) {
	managed := t.TempDir()

	// Create versioned structure: managed/my-skill/1/SKILL.md, managed/my-skill/2/SKILL.md
	os.MkdirAll(filepath.Join(managed, "my-skill", "1"), 0755)
	os.WriteFile(filepath.Join(managed, "my-skill", "1", "SKILL.md"),
		[]byte("---\nname: My Skill v1\n---\nVersion 1"), 0644)
	os.MkdirAll(filepath.Join(managed, "my-skill", "2"), 0755)
	os.WriteFile(filepath.Join(managed, "my-skill", "2", "SKILL.md"),
		[]byte("---\nname: My Skill v2\n---\nVersion 2"), 0644)

	l := NewLoader("", "", "")
	l.SetManagedDir(managed)

	skills := l.ListSkills(context.Background())
	if len(skills) != 1 {
		t.Fatalf("expected 1 managed skill, got %d", len(skills))
	}
	// Should pick v2 (highest)
	if skills[0].Name != "My Skill v2" {
		t.Errorf("expected latest version (v2), got %q", skills[0].Name)
	}
	if skills[0].Source != "managed" {
		t.Errorf("source should be managed, got %q", skills[0].Source)
	}
}

func TestLoader_ManagedSkills_WorkspaceTakesPriority(t *testing.T) {
	ws := t.TempDir()
	managed := t.TempDir()

	// Same slug in both workspace and managed
	makeSkillDir(t, filepath.Join(ws, "skills"), "shared-skill", "---\nname: Workspace Version\n---\n")
	os.MkdirAll(filepath.Join(managed, "shared-skill", "1"), 0755)
	os.WriteFile(filepath.Join(managed, "shared-skill", "1", "SKILL.md"),
		[]byte("---\nname: Managed Version\n---\n"), 0644)

	l := NewLoader(ws, "", "")
	l.SetManagedDir(managed)

	skills := l.ListSkills(context.Background())
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (dedup), got %d", len(skills))
	}
	if skills[0].Name != "Workspace Version" {
		t.Errorf("workspace should take priority over managed, got %q", skills[0].Name)
	}
}

// --- Dirs ---

func TestParseSimpleYAMLLists(t *testing.T) {
	cases := []struct {
		name    string
		content string
		key     string
		want    []string
	}{
		{
			name: "deps list",
			content: `name: test
deps:
  - pip:psycopg2-binary
  - system:ffmpeg
`,
			key:  "deps",
			want: []string{"pip:psycopg2-binary", "system:ffmpeg"},
		},
		{
			name: "quoted items",
			content: `deps:
  - "pip:requests"
  - 'npm:typescript'
`,
			key:  "deps",
			want: []string{"pip:requests", "npm:typescript"},
		},
		{
			name: "empty key",
			content: `name: test
description: plain
`,
			key:  "deps",
			want: nil,
		},
		{
			name: "crlf",
			content: "deps:\r\n  - pip:a\r\n  - pip:b\r\n",
			key:    "deps",
			want:   []string{"pip:a", "pip:b"},
		},
		{
			name: "scalar skipped",
			content: `deps: inline
other:
  - x
`,
			key:  "deps",
			want: nil,
		},
		{
			name: "multiple keys",
			content: `deps:
  - pip:a
exclude_deps:
  - pip:b
`,
			key:  "exclude_deps",
			want: []string{"pip:b"},
		},
		{
			// H2 regression: nested-map under tracked key must drop the key to
			// avoid silent prefix-loss ("pip:" stripped → miscategorized as system).
			name: "nested_map_dropped",
			content: `deps:
  pip:
    - requests
  system:
    - ffmpeg
`,
			key:  "deps",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSimpleYAMLLists(tc.content)
			gv := got[tc.key]
			if len(gv) != len(tc.want) {
				t.Fatalf("len = %d, want %d; got=%v", len(gv), len(tc.want), gv)
			}
			for i, v := range gv {
				if v != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

func TestLoader_Dirs(t *testing.T) {
	ws := t.TempDir()
	global := t.TempDir()
	builtin := t.TempDir()

	l := NewLoader(ws, global, builtin)
	dirs := l.Dirs()

	// Should include workspace skills dir, global, and builtin
	if len(dirs) == 0 {
		t.Error("expected non-empty dirs list")
	}
	// All returned dirs should be non-empty strings
	for _, d := range dirs {
		if d == "" {
			t.Error("dirs should not contain empty strings")
		}
	}
}
