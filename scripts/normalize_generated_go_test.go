package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeMakesFunctionOrderAndTemporariesStable(t *testing.T) {
	first := `package sample
import ard "kb/internal/ard"
func B() { var _tmp_91 ard.Result[int, error]; _ = _tmp_91 }
func A() { var _tmp_92 ard.Result[int, error]; _ = _tmp_92 }
func Existing() { _kb_release_tmp_0 := 1; _ = _kb_release_tmp_0 }
`
	second := `package sample
import ard "kb/internal/ard"
func A() { var _tmp_3 ard.Result[int, error]; _ = _tmp_3 }
func Existing() { _kb_release_tmp_0 := 1; _ = _kb_release_tmp_0 }
func B() { var _tmp_4 ard.Result[int, error]; _ = _tmp_4 }
`
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first.go")
	secondPath := filepath.Join(directory, "second.go")
	if err := os.WriteFile(firstPath, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalize(firstPath); err != nil {
		t.Fatal(err)
	}
	if err := normalize(secondPath); err != nil {
		t.Fatal(err)
	}
	firstResult, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstResult) != string(secondResult) {
		t.Fatalf("normalized output differs:\n%s\n---\n%s", firstResult, secondResult)
	}
}

func TestNormalizeLeavesOwnedGoSourceAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ffi.go")
	source := []byte("package ffi\nfunc B() {}\nfunc A() {}\n")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalize(path); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != string(source) {
		t.Fatalf("owned source changed: %q", result)
	}
}
