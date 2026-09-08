package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sympozium-ai/sympozium/internal/cellnauthority"
	"github.com/zeebo/blake3"
)

// Test setup provides explicit pinned kernel/initrd bytes, not admission. The
// actual Celln CLI must perform preparation, hardware checking and admission.
func admitLiveCandidate(t *testing.T, ctx context.Context, binary, root, composed, pkg, evidence string) cellnauthority.ExecutionArtifacts {
	t.Helper()
	readPinned := func(path, expected string) []byte {
		if !filepath.IsAbs(path) || len(expected) != 64 {
			t.Fatal("explicit absolute artifact path and SHA256 pin required")
		}
		raw, err := os.ReadFile(path)
		must(t, err)
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != expected {
			t.Fatal("operator test artifact pin mismatch")
		}
		return raw
	}
	kernel := readPinned(os.Getenv("CELLN_LIVE_KERNEL"), os.Getenv("CELLN_LIVE_KERNEL_SHA256"))
	initrd := readPinned(filepath.Join(pkg, "initramfs.cpio"), os.Getenv("CELLN_LIVE_INITRD_SHA256"))
	put := func(raw []byte) string {
		h := blake3.Sum256(raw)
		digest := hex.EncodeToString(h[:])
		path := filepath.Join(root, "motes", "objects", digest[:2], digest)
		must(t, os.MkdirAll(filepath.Dir(path), 0700))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		must(t, err)
		_, err = f.Write(raw)
		must(t, err)
		must(t, f.Close())
		return "blake3:" + digest
	}
	var closure struct {
		Publisher string `json:"publisher"`
		Closure   struct {
			Entrypoint string `json:"entrypoint"`
			Members    map[string]struct {
				Hash string `json:"hash"`
			} `json:"members"`
		} `json:"closure"`
	}
	readJSON(t, filepath.Join(composed, "signed-closure.json"), &closure)
	if closure.Closure.Entrypoint == "" || closure.Closure.Members[closure.Closure.Entrypoint].Hash == "" {
		t.Fatal("missing signed runtime identity")
	}
	template := map[string]any{"apiVersion": "celln.dev/mote-template-v1", "kernel": put(kernel), "initrd": put(initrd), "runtimeExecutable": closure.Closure.Members[closure.Closure.Entrypoint].Hash, "runtimeEntryPoint": closure.Closure.Entrypoint, "composerPublisher": closure.Publisher}
	path := filepath.Join(evidence, "operator-template.json")
	writeJSON(t, path, template)
	raw, err := os.ReadFile(path)
	must(t, err)
	hash := blake3.Sum256(raw)
	templateHash := fmt.Sprintf("blake3:%x", hash)
	candidate := filepath.Join(filepath.Dir(composed), "operator-candidate")
	command(t, ctx, nil, binary, "--root", root, "closure", "prepare-mote", "--template", path, "--template-hash", templateHash, "--descriptor", filepath.Join(composed, "signed-closure.json"), "--toolfs", filepath.Join(composed, "toolfs.ext2"), "--mote-store", filepath.Join(root, "motes"), "--output-dir", candidate)
	var prepared struct {
		Mote struct {
			Hash string `json:"hash"`
		} `json:"mote"`
	}
	readJSON(t, filepath.Join(candidate, "prepared.json"), &prepared)
	if _, err := os.Stat(filepath.Join(root, "trusted-motes.json")); !os.IsNotExist(err) {
		t.Fatal("preparation unexpectedly admitted a mote")
	}
	result := command(t, ctx, nil, binary, "--root", root, "closure", "admit-prepared", "--candidate", candidate, "--template-hash", templateHash, "--approve-mote", prepared.Mote.Hash, "--mote-store", filepath.Join(root, "motes"), "--tool-store", filepath.Join(root, "tools"))
	var artifacts cellnauthority.ExecutionArtifacts
	must(t, json.Unmarshal(result, &artifacts))
	var report map[string]any
	must(t, json.Unmarshal(result, &report))
	if report["admitted"] != true || artifacts.Mote.Hash != prepared.Mote.Hash {
		t.Fatal("operator admission not acknowledged")
	}
	writeJSON(t, filepath.Join(evidence, "operator-admission.json"), report)
	return artifacts
}
