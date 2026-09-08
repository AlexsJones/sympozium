package cellnreview

import (
	"context"
	"path/filepath"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

func TestIssuerServingRouteIsFrozenBeforeProvisioningAndCannotChange(t *testing.T) {
	for _, mode := range []string{"same", "router", "backend", "remove", "add"} {
		t.Run(mode, func(t *testing.T) {
			f, m, _ := managedFixture(t)
			endpoint, _, tokenPath := serveTestIssuer(t, m)
			route := &DispatchRoute{RouterURL: "https://router.example:443", Backend: "http://host-a:8787"}
			o := IssuerClientOptions{URL: endpoint, TokenFile: tokenPath, CAFile: filepath.Join(filepath.Dir(tokenPath), "cert.pem"), Route: route}
			if mode == "add" {
				o.Route = nil
			}
			issuer, err := NewIssuerClient(o)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(issuer.CloseIdleConnections)
			// Mutating the constructor input must not retarget a live client.
			route.Backend = "http://mutated-input:8787"
			ctx := context.Background()
			key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
			seed := &IssuerRequest{APIVersion: "sympozium.ai/celln-issuer-request-v1", Frozen: *f.f, Approval: *f.a, Artifacts: f.artifacts}
			if _, err := issuer.IssueForRun(ctx, issuanceStatusFault{Client: f.c, phase: "Prepared", afterCommit: true}, f.c, key, f.l, seed); err == nil {
				t.Fatal("lost preparation acknowledgement should refuse")
			}
			var current api.AgentRun
			if err := f.c.Get(ctx, key, &current); err != nil {
				t.Fatal(err)
			}
			payload, _, err := decodeRunIssuance(current.Status.CellnIssuance, issuer.endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "add" && (payload.Route == nil || payload.Route.Backend != "http://host-a:8787") {
				t.Fatal("prepared route was absent or constructor input mutation changed it")
			}
			assertNoProfiles(t, f.o.PolicyRoot)
			o.Route = &DispatchRoute{RouterURL: "https://router.example:443", Backend: "http://host-a:8787"}
			switch mode {
			case "router":
				o.Route.RouterURL = "https://other-router.example:443"
			case "backend":
				o.Route.Backend = "http://host-b:8787"
			case "remove":
				o.Route = nil
			}
			restarted, err := NewIssuerClient(o)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.CloseIdleConnections)
			_, err = restarted.IssueForRun(ctx, f.c, f.c, key, f.l, nil)
			if mode == "same" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := restarted.FreezeIssuedDispatch(ctx, f.c, f.c, key, f.l); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("changed route resumed provisioning")
			}
			assertNoProfiles(t, f.o.PolicyRoot)
			// Commit the outcome with the original configuration, then ensure
			// changed routing cannot consume that already-issued outcome either.
			if _, err := issuer.IssueForRun(ctx, f.c, f.c, key, f.l, nil); err != nil {
				t.Fatal(err)
			}
			if bytes, err := restarted.FreezeIssuedDispatch(ctx, f.c, f.c, key, f.l); err == nil || len(bytes) != 0 {
				t.Fatal("changed route returned an issued request")
			}
		})
	}
}

func TestIssuerRouteRefusesUnsafeOrigins(t *testing.T) {
	for _, route := range []DispatchRoute{
		{RouterURL: "http://router.example", Backend: "http://host:8787"},
		{RouterURL: "https://router.example/", Backend: "http://host:8787"},
		{RouterURL: "https://user:secret@router.example", Backend: "http://host:8787"},
		{RouterURL: "https://router.example?", Backend: "http://host:8787"},
		{RouterURL: "https://router.example", Backend: "https://host:8787"},
		{RouterURL: "https://router.example", Backend: "http://host:8787/path"},
		{RouterURL: "https://router.example", Backend: "http://host:8787\r\nHeader: injected"},
		{RouterURL: "https://router.example", Backend: "http://host:"},
		{RouterURL: "https://router.example#", Backend: "http://host:8787"},
		{RouterURL: "https://router.example", Backend: "http://host:65536"},
		{RouterURL: "https://router.example", Backend: "http://host:0"},
	} {
		if _, err := NewIssuerClient(IssuerClientOptions{URL: "https://issuer.example", TokenFile: "/not-read", Route: &route}); err == nil {
			t.Fatalf("unsafe route accepted: %+v", route)
		}
	}
}
