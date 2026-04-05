package webhook

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sympoziumv1alpha1 "github.com/sympozium-ai/sympozium/api/v1alpha1"
)

func decoderFor(t *testing.T, scheme *runtime.Scheme) admission.Decoder {
	t.Helper()
	return admission.NewDecoder(scheme)
}

func admissionRequestFor(t *testing.T, run *sympoziumv1alpha1.AgentRun) admission.Request {
	t.Helper()
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal run: %v", err)
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// TestPolicyEnforcer_AllowsDeletingRun_WhenInstanceMissing is the regression
// guard: when an AgentRun has been marked for deletion (deletionTimestamp
// set) and its referenced SympoziumInstance has already been cascade-deleted
// (e.g. PersonaPack disabled), the controller still needs to remove its
// finalizer to let the object be GC'd. The webhook MUST allow that update
// rather than rejecting it with "instance not found" and leaving the run
// stuck in a terminating state forever.
func TestPolicyEnforcer_AllowsDeletingRun_WhenInstanceMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := sympoziumv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	now := metav1.NewTime(time.Now())
	run := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "stuck-run",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"sympozium.ai/agentrun-finalizer"},
		},
		Spec: sympoziumv1alpha1.AgentRunSpec{
			InstanceRef: "already-deleted-instance",
			Task:        "irrelevant",
		},
	}

	// Client has NO instance — the referenced one is gone.
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	pe := &PolicyEnforcer{Client: cl, Log: logr.Discard(), Decoder: decoderFor(t, scheme)}

	resp := pe.Handle(context.Background(), admissionRequestFor(t, run))
	if !resp.Allowed {
		t.Fatalf("expected webhook to ALLOW update on deleting run; got denied: %s",
			resp.Result.Message)
	}
}

// TestPolicyEnforcer_RejectsCreate_WhenInstanceMissing: the existing
// behaviour for NEW runs is preserved — creating a run that references a
// nonexistent instance must still be rejected.
func TestPolicyEnforcer_RejectsCreate_WhenInstanceMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := sympoziumv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	run := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-run",
			Namespace: "default",
		},
		Spec: sympoziumv1alpha1.AgentRunSpec{
			InstanceRef: "nonexistent-instance",
			Task:        "x",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	pe := &PolicyEnforcer{Client: cl, Log: logr.Discard(), Decoder: decoderFor(t, scheme)}

	resp := pe.Handle(context.Background(), admissionRequestFor(t, run))
	if resp.Allowed {
		t.Fatalf("expected webhook to REJECT create of run with missing instance; got allowed")
	}
}

// TestPolicyEnforcer_AllowsRun_WhenInstanceExistsAndNoPolicy: baseline —
// run with a valid instance and no policy is allowed.
func TestPolicyEnforcer_AllowsRun_WhenInstanceExistsAndNoPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := sympoziumv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	instance := &sympoziumv1alpha1.SympoziumInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst", Namespace: "default"},
	}
	run := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "default"},
		Spec:       sympoziumv1alpha1.AgentRunSpec{InstanceRef: "inst", Task: "x"},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	pe := &PolicyEnforcer{Client: cl, Log: logr.Discard(), Decoder: decoderFor(t, scheme)}

	resp := pe.Handle(context.Background(), admissionRequestFor(t, run))
	if !resp.Allowed {
		t.Fatalf("expected allow; got denied: %s", resp.Result.Message)
	}
}
