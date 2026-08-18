package diff

import (
	"testing"
)

func TestAnalyzeGoFile_AddedFunction(t *testing.T) {
	oldSrc := []byte(`package main
`)
	newSrc := []byte(`package main

func Hello() string { return "hi" }
`)
	changes := AnalyzeGoFile(oldSrc, newSrc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Kind != ChangeAdded || c.Symbol != "Hello" || c.Category != CategoryFunction {
		t.Errorf("unexpected change: %+v", c)
	}
}

func TestAnalyzeGoFile_RemovedType(t *testing.T) {
	oldSrc := []byte(`package main

type Config struct {
	Name string
}
`)
	newSrc := []byte(`package main
`)
	changes := AnalyzeGoFile(oldSrc, newSrc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Kind != ChangeRemoved || c.Symbol != "Config" || c.Category != CategoryType {
		t.Errorf("unexpected change: %+v", c)
	}
}

func TestAnalyzeGoFile_ModifiedMethodSignature(t *testing.T) {
	oldSrc := []byte(`package main

type S struct{}

func (s *S) Do(x int) {}
`)
	newSrc := []byte(`package main

type S struct{}

func (s *S) Do(x int, y string) error { return nil }
`)
	changes := AnalyzeGoFile(oldSrc, newSrc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Kind != ChangeModified || c.Symbol != "S.Do" || c.Category != CategoryMethod {
		t.Errorf("unexpected change: %+v", c)
	}
	if c.Detail == "" {
		t.Error("expected detail about signature change")
	}
}

func TestAnalyzeGoFile_ChangedImports(t *testing.T) {
	oldSrc := []byte(`package main

import "fmt"
`)
	newSrc := []byte(`package main

import "os"
`)
	changes := AnalyzeGoFile(oldSrc, newSrc)
	foundAdded := false
	foundRemoved := false
	for _, c := range changes {
		if c.Kind == ChangeAdded && c.Symbol == "os" && c.Category == CategoryImport {
			foundAdded = true
		}
		if c.Kind == ChangeRemoved && c.Symbol == "fmt" && c.Category == CategoryImport {
			foundRemoved = true
		}
	}
	if !foundAdded {
		t.Error("expected added import 'os'")
	}
	if !foundRemoved {
		t.Error("expected removed import 'fmt'")
	}
}

func TestAnalyzeGoFile_NilOldSrc(t *testing.T) {
	newSrc := []byte(`package main

func New() {}
`)
	changes := AnalyzeGoFile(nil, newSrc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Kind != ChangeAdded {
		t.Errorf("expected added, got %s", changes[0].Kind)
	}
}

func TestAnalyzeGoFile_NilNewSrc(t *testing.T) {
	oldSrc := []byte(`package main

func Old() {}
`)
	changes := AnalyzeGoFile(oldSrc, nil)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Kind != ChangeRemoved {
		t.Errorf("expected removed, got %s", changes[0].Kind)
	}
}

func TestAnalyzeGoFile_ConstAndVar(t *testing.T) {
	oldSrc := []byte(`package main

const Version = "1.0"
var Debug = false
`)
	newSrc := []byte(`package main

const Version = "2.0"
var Verbose = true
`)
	changes := AnalyzeGoFile(oldSrc, newSrc)
	foundRemovedDebug := false
	foundAddedVerbose := false
	for _, c := range changes {
		if c.Kind == ChangeRemoved && c.Symbol == "Debug" && c.Category == CategoryVar {
			foundRemovedDebug = true
		}
		if c.Kind == ChangeAdded && c.Symbol == "Verbose" && c.Category == CategoryVar {
			foundAddedVerbose = true
		}
	}
	if !foundRemovedDebug {
		t.Error("expected removed var Debug")
	}
	if !foundAddedVerbose {
		t.Error("expected added var Verbose")
	}
}
