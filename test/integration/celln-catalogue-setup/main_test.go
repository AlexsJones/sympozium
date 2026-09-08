package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRejectsImplicitPathsAndOtherContexts(t *testing.T) {
	if err := setup("", "catalogue.json"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("implicit environment accepted: %v", err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncurrent-context: kubernetes-admin@kubernetes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := setup(path, "/not-read-catalogue.json"); err == nil || !strings.Contains(err.Error(), "only kind-celln-deployed") {
		t.Fatalf("non-proof cluster accepted: %v", err)
	}
}
