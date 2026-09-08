package main

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// Cache selection is deliberately separate from authority: the uncached reader
// still resolves independently configured approval sources, and direct clients
// remain subject to their service account's Kubernetes RBAC.
func controllerCacheOptions(namespace string) (cache.Options, error) {
	if namespace == "" {
		return cache.Options{}, nil
	}
	if errors := validation.IsDNS1123Label(namespace); len(errors) != 0 {
		return cache.Options{}, fmt.Errorf("watch namespace must be a single valid namespace name")
	}
	return cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}}, nil
}
