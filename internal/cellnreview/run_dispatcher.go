package cellnreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnauthority"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RunDispatchBinding struct {
	Issuer       *IssuerClient
	Router       *RouterClient
	Loader       cellnauthority.ModelLoader
	Compositions []RegisteredComposition
}

// RunDispatcher consumes durable catalogue issuance. Bindings are trusted
// deployment configuration, never fields copied from a tenant's run.
type RunDispatcher struct {
	writer   client.Client
	reader   client.Reader
	bindings map[types.NamespacedName]RunDispatchBinding
}

func NewRunDispatcher(writer client.Client, reader client.Reader, bindings map[types.NamespacedName]RunDispatchBinding) (*RunDispatcher, error) {
	if writer == nil || reader == nil || len(bindings) == 0 || len(bindings) > 1024 {
		return nil, fmt.Errorf("writer, uncached reader and bounded bindings required")
	}
	copy := make(map[types.NamespacedName]RunDispatchBinding, len(bindings))
	for key, b := range bindings {
		if key.Namespace == "" || key.Name == "" || b.Issuer == nil || b.Router == nil || b.Issuer.route == nil || b.Issuer.route.RouterURL != b.Router.origin || b.Issuer.tokenFile == b.Router.transport.tokenFile {
			return nil, fmt.Errorf("binding requires matching frozen route and separate issuer/router credentials")
		}
		b.Loader.Selection.Reader = reader
		var err error
		b.Compositions, err = copyRegisteredCompositions(b.Compositions)
		if err != nil {
			return nil, err
		}
		copy[key] = b
	}
	return &RunDispatcher{writer, reader, copy}, nil
}

func (d *RunDispatcher) history(ctx context.Context, key types.NamespacedName) (*api.AgentRun, RunDispatchBinding, *IssuedSelection, error) {
	var run api.AgentRun
	if err := d.reader.Get(ctx, key, &run); err != nil {
		return nil, RunDispatchBinding{}, nil, err
	}
	if run.Status.CellnIssuance == nil || len(run.Status.CellnIssuance.Payload) > 393216 {
		return nil, RunDispatchBinding{}, nil, fmt.Errorf("no catalogue issuance history")
	}
	var index runIssuancePayload
	if json.Unmarshal([]byte(run.Status.CellnIssuance.Payload), &index) != nil || index.Request.Frozen.Snapshot.Agent.Namespace != key.Namespace {
		return nil, RunDispatchBinding{}, nil, fmt.Errorf("invalid catalogue binding index")
	}
	b, ok := d.bindings[types.NamespacedName{Namespace: key.Namespace, Name: index.Request.Frozen.Snapshot.Agent.Name}]
	if !ok || run.Spec.Backend != "celln" {
		return nil, b, nil, fmt.Errorf("no configured catalogue issuance binding")
	}
	payload, issued, err := decodeRunIssuance(run.Status.CellnIssuance, b.Issuer.endpoint)
	if err != nil {
		return nil, b, nil, err
	}
	if issued == nil || !reflect.DeepEqual(payload.Route, b.Issuer.route) || payload.Request.Frozen.Run.Namespace != run.Namespace || payload.Request.Frozen.Run.Name != run.Name || payload.Request.Frozen.Run.UID != run.UID {
		return nil, b, nil, fmt.Errorf("issued history does not bind this run and serving route")
	}
	if run.Status.CellnRequest != "" || run.Status.CellnActionID != "" {
		var request struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(issued.Request, &request) != nil || !executionPathID(request.ID) || run.Status.CellnRequest != string(issued.Request) || run.Status.CellnActionID != request.ID {
			return nil, b, nil, fmt.Errorf("dispatch journal differs from issued history")
		}
	}
	return &run, b, issued, nil
}

func (d *RunDispatcher) ReconcilePending(ctx context.Context, key types.NamespacedName, admit func(context.Context, *api.AgentRun) error) (*RouterExecution, error) {
	run, b, _, err := d.history(ctx, key)
	if err != nil {
		return nil, err
	}
	if run.DeletionTimestamp != nil || (run.Status.Phase != "" && run.Status.Phase != api.AgentRunPhasePending) {
		return nil, fmt.Errorf("run is not pending dispatch")
	}
	if run.Status.CellnRequest != "" {
		// Observation is deliberately independent of fresh approval: work may
		// already exist and must remain recoverable after approval withdrawal.
		record, err := b.Router.Lookup(ctx, *b.Issuer.route, run.Status.CellnActionID)
		if err == nil {
			return record, nil
		}
		var status *RouterHTTPError
		if !errors.As(err, &status) || status.StatusCode != 404 {
			return nil, err
		}
	}
	if admit == nil {
		return nil, fmt.Errorf("controller admission checks required before submission")
	}
	if err := admit(ctx, run); err != nil {
		return nil, err
	}
	body, err := b.Issuer.FreezeIssuedDispatch(ctx, d.writer, d.reader, key, b.Loader)
	if err != nil {
		return nil, err
	}
	if _, err := b.Router.Prewarm(ctx, *b.Issuer.route, body); err != nil {
		return nil, err
	}
	var current api.AgentRun
	if err := d.reader.Get(ctx, key, &current); err != nil {
		return nil, err
	}
	if err := admit(ctx, &current); err != nil {
		return nil, err
	}
	// The prewarm observation is not authority. Revalidate the exact approved
	// outcome again before submission; never regenerate a candidate here.
	body, err = b.Issuer.FreezeIssuedDispatch(ctx, d.writer, d.reader, key, b.Loader)
	if err != nil {
		return nil, err
	}
	return b.Router.Submit(ctx, *b.Issuer.route, body)
}

func (d *RunDispatcher) Lookup(ctx context.Context, key types.NamespacedName) (*RouterExecution, error) {
	run, b, _, err := d.history(ctx, key)
	if err != nil {
		return nil, err
	}
	if run.Status.CellnActionID == "" {
		return nil, fmt.Errorf("catalogue execution has no saved dispatch identity")
	}
	return b.Router.Lookup(ctx, *b.Issuer.route, run.Status.CellnActionID)
}

func (d *RunDispatcher) Cancel(ctx context.Context, key types.NamespacedName) (*RouterExecution, error) {
	run, b, _, err := d.history(ctx, key)
	if err != nil {
		return nil, err
	}
	if run.Status.CellnActionID == "" {
		return nil, fmt.Errorf("catalogue execution has no saved dispatch identity")
	}
	return b.Router.Cancel(ctx, *b.Issuer.route, run.Status.CellnActionID)
}
