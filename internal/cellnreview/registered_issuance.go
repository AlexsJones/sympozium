package cellnreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnauthority"
	"k8s.io/apimachinery/pkg/types"
)

var ErrNoRegisteredComposition = errors.New("no operator-registered composition matches this selection")

// RegisteredComposition locates already-materialized artifacts. Registration
// neither admits them nor grants execution: the host issuer independently
// verifies the exact signed composition, mote and current authority.
type RegisteredComposition struct {
	Sources    []string                          `json:"sources"`
	ImageBytes int64                             `json:"imageBytes"`
	Artifacts  cellnauthority.ExecutionArtifacts `json:"artifacts"`
}

func copyRegisteredCompositions(in []RegisteredComposition) ([]RegisteredComposition, error) {
	if len(in) > 128 {
		return nil, fmt.Errorf("at most 128 registered compositions per Agent")
	}
	out := make([]RegisteredComposition, len(in))
	seen := map[string]bool{}
	for i, r := range in {
		if len(r.Sources) < 1 || len(r.Sources) > 17 || r.ImageBytes < 33554432 || r.ImageBytes > 536870912 || r.ImageBytes%2097152 != 0 || !artifactHash(r.Artifacts.Mote.Hash) || !artifactHash(r.Artifacts.Closure.Hash) {
			return nil, fmt.Errorf("bounded exact registered artifacts and image size required")
		}
		for _, hash := range r.Sources {
			if !artifactHash(hash) {
				return nil, fmt.Errorf("registered source must be an exact closure hash")
			}
		}
		key, _ := json.Marshal(r.Sources)
		if seen[string(key)] {
			return nil, fmt.Errorf("ambiguous registered source sequence")
		}
		seen[string(key)] = true
		out[i] = r
		out[i].Sources = append([]string(nil), r.Sources...)
	}
	return out, nil
}

// EnsureIssued automates only the existing durable issuance path for named
// catalogue intent. It never composes, admits, distributes, prewarms or submits.
// Once Prepared exists, retries use that exact saved seed/route, not a newly
// selected registration. Removing a registration is not approval revocation.
func (d *RunDispatcher) EnsureIssued(ctx context.Context, key types.NamespacedName, admit func(context.Context, *api.AgentRun) error) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var run api.AgentRun
	if err := d.reader.Get(ctx, key, &run); err != nil {
		return err
	}
	if run.Spec.CellnSelection == nil || run.Spec.Backend != "celln" || run.Spec.Celln != nil {
		return fmt.Errorf("named catalogue intent required")
	}
	if err := provisionableRun(&run); err != nil {
		return err
	}
	b, ok := d.bindings[types.NamespacedName{Namespace: key.Namespace, Name: run.Spec.AgentRef}]
	if !ok {
		return ErrNoRegisteredComposition
	}
	if run.Status.CellnIssuance == nil && len(b.Compositions) == 0 {
		return ErrNoRegisteredComposition
	}
	if admit == nil {
		return fmt.Errorf("controller admission checks required before issuance")
	}
	if err := admit(ctx, &run); err != nil {
		return err
	}
	var seed *IssuerRequest
	if run.Status.CellnIssuance == nil {
		selected := make([]cellnauthority.Selection, 0, len(run.Spec.CellnSelection.ToolRefs))
		for _, ref := range run.Spec.CellnSelection.ToolRefs {
			selected = append(selected, cellnauthority.Selection{Name: ref.Name, Revision: ref.Revision})
		}
		frozen, err := b.Loader.Selection.FreezeRun(ctx, key, selected, 33554432)
		if err != nil {
			return err
		}
		var registration *RegisteredComposition
		for i := range b.Compositions {
			if slices.Equal(b.Compositions[i].Sources, frozen.Prepared.Composition.Sources) {
				registration = &b.Compositions[i]
				break
			}
		}
		if registration == nil {
			return ErrNoRegisteredComposition
		}
		if registration.ImageBytes != 33554432 {
			frozen, err = b.Loader.Selection.FreezeRun(ctx, key, selected, registration.ImageBytes)
			if err != nil {
				return err
			}
			if !slices.Equal(registration.Sources, frozen.Prepared.Composition.Sources) {
				return fmt.Errorf("selection changed during artifact resolution")
			}
		}
		approval, err := b.Loader.Resolve(ctx, *frozen)
		if err != nil {
			return err
		}
		seed = &IssuerRequest{APIVersion: "sympozium.ai/celln-issuer-request-v1", Frozen: *frozen, Approval: *approval, Artifacts: registration.Artifacts}
	}
	_, err := b.Issuer.IssueForRun(ctx, d.writer, d.reader, key, b.Loader, seed)
	return err
}
