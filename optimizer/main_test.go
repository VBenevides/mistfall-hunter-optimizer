package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResultPathRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../save", `folder\save`} {
		if _, err := resultPath(name); err == nil {
			t.Fatalf("accepted result name %q", name)
		}
	}
	path, err := resultPath("build 1")
	if err != nil || filepath.Base(path) != "build 1.json" || filepath.Base(filepath.Dir(path)) != "results" {
		t.Fatalf("result path = %q, %v", path, err)
	}
}

func TestSavedResultLifecycle(t *testing.T) {
	service := &GUIService{}
	name := "test-saved-result"
	service.DeleteResult(name)
	defer service.DeleteResult(name)
	if err := service.SaveResult(name, GUISession{}); err == nil {
		t.Fatal("unsuccessful result was saved")
	}
	session := GUISession{HasResult: true, Result: GUIResult{Possible: true}}
	if err := service.SaveResult(name, session); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveResult(name, session); err != nil {
		t.Fatal(err)
	}
	names, err := service.ListResults()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, saved := range names {
		found = found || saved.Name == name && saved.CreatedAt != ""
	}
	if !found {
		t.Fatalf("saved result missing from %#v", names)
	}
	loaded, err := service.LoadResult(name)
	if err != nil || !loaded.Result.Possible {
		t.Fatalf("loaded result = %#v, %v", loaded, err)
	}
	if err := service.DeleteResult(name); err != nil {
		t.Fatal(err)
	}
}

func TestLoadResultFallsBackToRequest(t *testing.T) {
	name := "test-broken-result"
	path, err := resultPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data := []byte(`{"request":{"characterClass":"Mercenary","affixes":[{"name":"Aegis","level":3}]},"result":{"sets":"broken"},"hasResult":true}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := (&GUIService{}).LoadResult(name)
	if err != nil || loaded.Request.CharacterClass != "Mercenary" || len(loaded.Request.Affixes) != 1 || loaded.HasResult {
		t.Fatalf("fallback session = %#v, %v", loaded, err)
	}
}
