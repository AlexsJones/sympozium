package cellnauthority

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCatalogueIntentCannotBeChangedByOperatorSelection(t *testing.T) {
	for _, mode := range []string{"match", "empty", "missing", "different-name", "different-revision", "mixed", "null", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			l, c, old := frozenFixture(t)
			ctx := context.Background()
			key := types.NamespacedName{Namespace: "tenant", Name: "run"}
			var run api.AgentRun
			if err := c.Get(ctx, key, &run); err != nil {
				t.Fatal(err)
			}
			id := old.Snapshot.Tools[0].Identity
			refs := []api.CellnCatalogueToolRef{{Name: id.Name, Revision: id.Revision}}
			selected := []Selection{{Name: id.Name, Revision: id.Revision}}
			switch mode {
			case "empty":
				refs = []api.CellnCatalogueToolRef{}
				selected = nil
			case "missing":
				selected = nil
			case "different-name":
				selected[0].Name = "other"
			case "different-revision":
				selected[0].Revision = "other"
			case "mixed":
				run.Spec.Celln = &api.CellnExecutionSpec{}
			case "null":
				refs = nil
				selected = nil
			case "duplicate":
				refs = append(refs, refs[0])
				selected = append(selected, selected[0])
			}
			run.Spec.CellnSelection = &api.CellnCatalogueSelection{ToolRefs: refs}
			if err := c.Update(ctx, &run); err != nil {
				t.Fatal(err)
			}
			frozen, err := l.FreezeRun(ctx, key, selected, 33554432)
			if mode != "match" && mode != "empty" {
				if err == nil {
					t.Fatal("changed or ambiguous intent accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := l.Revalidate(ctx, *frozen); err != nil {
				t.Fatal(err)
			}
			if len(frozen.Snapshot.Tools) != len(refs) {
				t.Fatal("empty selection expanded")
			}
			if err := l.Revalidate(ctx, *old); err == nil {
				t.Fatal("old frozen run survived intent change")
			}
		})
	}
}

func TestCatalogueRuntimeOverrideRequiresIndependentPairApproval(t *testing.T) {
	l, c, old := frozenFixture(t)
	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant", Name: "run"}
	var rt api.AgentRuntime
	if err := c.Get(ctx, types.NamespacedName{Namespace: "tenant", Name: "harness"}, &rt); err != nil {
		t.Fatal(err)
	}
	rt.Name, rt.UID, rt.ResourceVersion = "other-harness", "other-runtime-uid", ""
	if err := c.Create(ctx, &rt); err != nil {
		t.Fatal(err)
	}
	var run api.AgentRun
	if err := c.Get(ctx, key, &run); err != nil {
		t.Fatal(err)
	}
	id := old.Snapshot.Tools[0].Identity
	run.Spec.CellnSelection = &api.CellnCatalogueSelection{RuntimeRef: rt.Name, ToolRefs: []api.CellnCatalogueToolRef{{Name: id.Name, Revision: id.Revision}}}
	if err := c.Update(ctx, &run); err != nil {
		t.Fatal(err)
	}
	selection := []Selection{{Name: id.Name, Revision: id.Revision}}
	if _, err := l.FreezeRun(ctx, key, selection, 33554432); err == nil {
		t.Fatal("runtime override reused different runtime grants")
	}
	runtimeID, err := IdentifySubject("AgentRuntime", rt.ObjectMeta, rt.Spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []types.NamespacedName{l.OperatorSource, l.RuntimeSource, l.AgentSource} {
		var cm corev1.ConfigMap
		if err := c.Get(ctx, ref, &cm); err != nil {
			t.Fatal(err)
		}
		var doc GrantDocument
		if err := json.Unmarshal([]byte(cm.Data["grants.json"]), &doc); err != nil {
			t.Fatal(err)
		}
		doc.Runtime = runtimeID
		raw, _ := json.Marshal(doc)
		cm.Data["grants.json"] = string(raw)
		if err := c.Update(ctx, &cm); err != nil {
			t.Fatal(err)
		}
	}
	frozen, err := l.FreezeRun(ctx, key, selection, 33554432)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Snapshot.Runtime != runtimeID {
		t.Fatal("override not frozen")
	}
	if err := l.Revalidate(ctx, *frozen); err != nil {
		t.Fatal(err)
	}
	var agent api.Agent
	if err := c.Get(ctx, types.NamespacedName{Namespace: "tenant", Name: "agent"}, &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Spec.RuntimeRef != "harness" {
		t.Fatal("one-run override mutated Agent default")
	}
}
