package main

import "testing"

func TestParseVDFManifest(t *testing.T) {
	input := `
"AppState"
{
	"appid"		"1245620"
	"name"		"ELDEN RING"
	"StateFlags"		"4"
	"installdir"		"ELDEN RING"
}
`
	doc, err := parseVDF(input)
	if err != nil {
		t.Fatalf("parseVDF: %v", err)
	}
	state := vdfObject(doc, "AppState")
	if state == nil {
		t.Fatal("missing AppState")
	}
	if got := vdfString(state, "name"); got != "ELDEN RING" {
		t.Fatalf("name = %q", got)
	}
	if got := vdfString(state, "installdir"); got != "ELDEN RING" {
		t.Fatalf("installdir = %q", got)
	}
	if got := vdfString(state, "stateflags"); got != "4" {
		t.Fatalf("StateFlags = %q", got)
	}
}

func TestParseLibraryFoldersV2(t *testing.T) {
	input := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
		"apps"
		{
			"1245620"		"123456"
		}
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"label"		""
	}
}
`
	doc, err := parseVDF(input)
	if err != nil {
		t.Fatalf("parseVDF: %v", err)
	}
	folders := vdfObject(doc, "libraryfolders")
	if folders == nil {
		t.Fatal("missing libraryfolders")
	}
	first := vdfObject(folders, "0")
	if got := vdfString(first, "path"); got != `C:\Program Files (x86)\Steam` {
		t.Fatalf("path = %q", got)
	}
	second := vdfObject(folders, "1")
	if got := vdfString(second, "path"); got != `D:\SteamLibrary` {
		t.Fatalf("path = %q", got)
	}
}

func TestParseVDFCommentAndBOM(t *testing.T) {
	input := "\ufeff// comment\n\"root\" { \"a\" \"b\" }"
	doc, err := parseVDF(input)
	if err != nil {
		t.Fatalf("parseVDF: %v", err)
	}
	if got := vdfString(vdfObject(doc, "root"), "a"); got != "b" {
		t.Fatalf("a = %q", got)
	}
}

func TestParseVDFInvalid(t *testing.T) {
	for _, input := range []string{
		`"root" { "a" "b"`,
		`"root" }`,
		`"root" { "a" }`,
		`"root" { { "a" "b" } }`,
	} {
		if _, err := parseVDF(input); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}
