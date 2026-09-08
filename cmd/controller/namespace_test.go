package main

import (
	"strings"
	"testing"
)

func TestControllerNamespaceCacheIsExplicitAndDefaultCompatible(t *testing.T) {
	defaults, err := controllerCacheOptions("")
	if err != nil || defaults.DefaultNamespaces != nil || defaults.ByObject != nil {
		t.Fatal("default cluster-wide cache changed")
	}
	options, err := controllerCacheOptions("celln-catalogue-proof-test")
	if err != nil || len(options.DefaultNamespaces) != 1 {
		t.Fatal("one namespace was not selected")
	}
	if _, ok := options.DefaultNamespaces["celln-catalogue-proof-test"]; !ok {
		t.Fatal("wrong namespace selected")
	}
	if _, ok := options.DefaultNamespaces[""]; ok {
		t.Fatal("all-namespace fallback added")
	}
	for _, invalid := range []string{"*", "a,b", " a", "a ", " ", "A", "a.b", "a/b", strings.Repeat("a", 64)} {
		if _, err := controllerCacheOptions(invalid); err == nil {
			t.Fatalf("invalid namespace accepted: %q", invalid)
		}
	}
}
