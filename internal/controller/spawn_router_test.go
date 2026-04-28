package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sympoziumv1alpha1 "github.com/sympozium-ai/sympozium/api/v1alpha1"
)

func newTestSpawnRouter(t *testing.T, objs ...client.Object) *SpawnRouter {
	t.Helper()

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	sympoziumv1alpha1.AddToScheme(scheme)

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&sympoziumv1alpha1.Ensemble{}).
		Build()

	return &SpawnRouter{
		Client: cl,
		Log:    logr.Discard(),
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	pack := &sympoziumv1alpha1.Ensemble{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pack", Namespace: "default"},
		Spec: sympoziumv1alpha1.EnsembleSpec{
			SharedMemory: &sympoziumv1alpha1.SharedMemorySpec{
				Enabled: true,
				Membrane: &sympoziumv1alpha1.MembraneSpec{
					CircuitBreaker: &sympoziumv1alpha1.CircuitBreakerSpec{
						ConsecutiveFailures: 2,
					},
				},
			},
		},
	}
	parentRun := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-run",
			Namespace: "default",
			Labels:    map[string]string{"sympozium.ai/ensemble": "my-pack"},
		},
	}

	sr := newTestSpawnRouter(t, pack, parentRun)
	ctx := context.Background()

	// First failure: should not trip.
	sr.incrementCircuitBreaker(ctx, "parent-run")
	var updated sympoziumv1alpha1.Ensemble
	sr.Client.Get(ctx, types.NamespacedName{Name: "my-pack", Namespace: "default"}, &updated)
	if updated.Status.CircuitBreakerOpen {
		t.Error("circuit breaker should not be open after 1 failure")
	}
	if updated.Status.ConsecutiveDelegateFailures != 1 {
		t.Errorf("failures = %d, want 1", updated.Status.ConsecutiveDelegateFailures)
	}

	// Second failure: should trip (threshold = 2).
	sr.incrementCircuitBreaker(ctx, "parent-run")
	sr.Client.Get(ctx, types.NamespacedName{Name: "my-pack", Namespace: "default"}, &updated)
	if !updated.Status.CircuitBreakerOpen {
		t.Error("circuit breaker should be open after 2 failures")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	pack := &sympoziumv1alpha1.Ensemble{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pack", Namespace: "default"},
		Spec: sympoziumv1alpha1.EnsembleSpec{
			SharedMemory: &sympoziumv1alpha1.SharedMemorySpec{
				Enabled: true,
				Membrane: &sympoziumv1alpha1.MembraneSpec{
					CircuitBreaker: &sympoziumv1alpha1.CircuitBreakerSpec{
						ConsecutiveFailures: 3,
					},
				},
			},
		},
		Status: sympoziumv1alpha1.EnsembleStatus{
			ConsecutiveDelegateFailures: 2,
		},
	}
	parentRun := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-run",
			Namespace: "default",
			Labels:    map[string]string{"sympozium.ai/ensemble": "my-pack"},
		},
	}

	sr := newTestSpawnRouter(t, pack, parentRun)
	ctx := context.Background()

	sr.resetCircuitBreaker(ctx, "parent-run")

	var updated sympoziumv1alpha1.Ensemble
	sr.Client.Get(ctx, types.NamespacedName{Name: "my-pack", Namespace: "default"}, &updated)
	if updated.Status.ConsecutiveDelegateFailures != 0 {
		t.Errorf("failures = %d, want 0 after reset", updated.Status.ConsecutiveDelegateFailures)
	}
	if updated.Status.CircuitBreakerOpen {
		t.Error("circuit breaker should be closed after reset")
	}
}

func TestCircuitBreaker_BlocksSpawn(t *testing.T) {
	pack := &sympoziumv1alpha1.Ensemble{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pack", Namespace: "default"},
		Spec: sympoziumv1alpha1.EnsembleSpec{
			SharedMemory: &sympoziumv1alpha1.SharedMemorySpec{
				Enabled: true,
				Membrane: &sympoziumv1alpha1.MembraneSpec{
					CircuitBreaker: &sympoziumv1alpha1.CircuitBreakerSpec{
						ConsecutiveFailures: 3,
					},
				},
			},
		},
		Status: sympoziumv1alpha1.EnsembleStatus{
			CircuitBreakerOpen:          true,
			ConsecutiveDelegateFailures: 3,
		},
	}
	parentRun := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-run",
			Namespace: "default",
			Labels:    map[string]string{"sympozium.ai/ensemble": "my-pack"},
		},
	}

	sr := newTestSpawnRouter(t, pack, parentRun)
	ctx := context.Background()

	err := sr.checkCircuitBreaker(ctx, "my-pack", "parent-run")
	if err == nil {
		t.Error("expected error when circuit breaker is open")
	}
}

func TestCircuitBreaker_NoConfig(t *testing.T) {
	pack := &sympoziumv1alpha1.Ensemble{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pack", Namespace: "default"},
		Spec:       sympoziumv1alpha1.EnsembleSpec{},
	}
	parentRun := &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-run",
			Namespace: "default",
			Labels:    map[string]string{"sympozium.ai/ensemble": "my-pack"},
		},
	}

	sr := newTestSpawnRouter(t, pack, parentRun)
	ctx := context.Background()

	// Should not error when no circuit breaker is configured.
	err := sr.checkCircuitBreaker(ctx, "my-pack", "parent-run")
	if err != nil {
		t.Errorf("expected no error without circuit breaker config, got: %v", err)
	}
}
