package cellnreview

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRegisteredCompositionValidationAndCopy(t *testing.T) {
	f := provisionFixture(t)
	base := RegisteredComposition{Sources: f.f.Prepared.Composition.Sources, ImageBytes: 33554432, Artifacts: f.artifacts}
	for _, mode := range []string{"valid", "empty-sources", "bad-source", "bad-mote", "small", "unaligned", "ambiguous"} {
		r := base
		r.Sources = append([]string(nil), base.Sources...)
		switch mode {
		case "empty-sources":
			r.Sources = nil
		case "bad-source":
			r.Sources[0] = "bad"
		case "bad-mote":
			r.Artifacts.Mote.Hash = "bad"
		case "small":
			r.ImageBytes = 1
		case "unaligned":
			r.ImageBytes++
		}
		input := []RegisteredComposition{r}
		if mode == "ambiguous" {
			input = append(input, r)
		}
		out, err := copyRegisteredCompositions(input)
		if mode != "valid" {
			if err == nil {
				t.Fatalf("accepted %s", mode)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		input[0].Sources[0] = "mutated"
		if out[0].Sources[0] == "mutated" {
			t.Fatal("configuration alias retained")
		}
	}
}

func TestRegisteredAutomaticIssuanceAndPreparedRecovery(t *testing.T) {
	for _, mode := range []string{"issued", "missing", "unmatched", "gate", "prepared-ack", "result-ack", "result-before"} {
		t.Run(mode, func(t *testing.T) {
			f, m, _ := managedFixture(t)
			ctx := context.Background()
			key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
			var run api.AgentRun
			if err := f.c.Get(ctx, key, &run); err != nil {
				t.Fatal(err)
			}
			run.Spec.CellnSelection = &api.CellnCatalogueSelection{ToolRefs: []api.CellnCatalogueToolRef{}}
			if err := f.c.Update(ctx, &run); err != nil {
				t.Fatal(err)
			}
			endpoint, _, token := serveTestIssuer(t, m)
			issuer, err := NewIssuerClient(IssuerClientOptions{URL: endpoint, TokenFile: token, CAFile: filepath.Join(filepath.Dir(token), "cert.pem"), Route: &DispatchRoute{RouterURL: "https://router.example", Backend: "http://host-a:8787"}})
			if err != nil {
				t.Fatal(err)
			}
			defer issuer.CloseIdleConnections()
			router, err := NewRouterClient("https://router.example", "/never-read-auto-issuance-router-token", "")
			if err != nil {
				t.Fatal(err)
			}
			defer router.CloseIdleConnections()
			registrations := []RegisteredComposition{{Sources: append([]string(nil), f.f.Prepared.Composition.Sources...), ImageBytes: 33554432, Artifacts: f.artifacts}}
			if mode == "missing" {
				registrations = nil
			}
			if mode == "unmatched" {
				registrations[0].Sources[0] = "blake3:" + strings.Repeat("e", 64)
			}
			binding := RunDispatchBinding{Issuer: issuer, Router: router, Loader: f.l, Compositions: registrations}
			bindings := map[types.NamespacedName]RunDispatchBinding{{Namespace: f.f.Snapshot.Agent.Namespace, Name: f.f.Snapshot.Agent.Name}: binding}
			d, err := NewRunDispatcher(f.c, f.c, bindings)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "prepared-ack" {
				d.writer = issuanceStatusFault{Client: f.c, phase: "Prepared", afterCommit: true}
			}
			if mode == "result-ack" || mode == "result-before" {
				d.writer = issuanceStatusFault{Client: f.c, phase: "Issued", afterCommit: mode == "result-ack"}
			}
			admit := func(context.Context, *api.AgentRun) error {
				if mode == "gate" {
					return fmt.Errorf("gate refused")
				}
				return nil
			}
			err = d.EnsureIssued(ctx, key, admit)
			if mode == "missing" || mode == "unmatched" || mode == "gate" {
				if err == nil {
					t.Fatal("unregistered/denied selection issued")
				}
				if mode != "gate" && !errors.Is(err, ErrNoRegisteredComposition) {
					t.Fatal(err)
				}
				if err := f.c.Get(ctx, key, &run); err != nil {
					t.Fatal(err)
				}
				if run.Status.CellnIssuance != nil {
					t.Fatal("refusal prepared host work")
				}
				return
			}
			if mode != "issued" && err == nil {
				t.Fatal("lost status acknowledgement was ignored")
			}
			if mode == "issued" && err != nil {
				t.Fatal(err)
			}
			// Recreate the controller service with no registrations. Recovery
			// must use durable history, never require a new artifact choice.
			binding.Compositions = nil
			d, err = NewRunDispatcher(f.c, f.c, map[types.NamespacedName]RunDispatchBinding{{Namespace: f.f.Snapshot.Agent.Namespace, Name: f.f.Snapshot.Agent.Name}: binding})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.EnsureIssued(ctx, key, admit); err != nil {
				t.Fatal(err)
			}
			if err := f.c.Get(ctx, key, &run); err != nil {
				t.Fatal(err)
			}
			if run.Status.CellnIssuance == nil || run.Status.CellnIssuance.Phase != "Issued" || run.Status.CellnActionID != "" || run.Status.CellnRequest != "" || run.Status.StartedAt != nil {
				t.Fatal("issuance was not durable or crossed into dispatch")
			}
		})
	}
}
