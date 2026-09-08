package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func proveTenantHostNetwork(t *testing.T, ctx context.Context, c client.Client, image, issuer, router, evidence string) {
	t.Helper()
	if !strings.HasPrefix(image, "localhost/sympozium-celln-network-probe:") {
		t.Fatal("explicit preloaded local network probe image required")
	}
	var endpoints []string
	for _, endpoint := range []string{issuer, router} {
		u, err := url.Parse(endpoint)
		must(t, err)
		if u.Scheme != "https" || u.Hostname() != "10.89.0.1" || u.Port() == "" {
			t.Fatal("network proof requires private Podman host TLS endpoints")
		}
		endpoints = append(endpoints, u.Host)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "celln-tenant-network-"}}
	must(t, c.Create(ctx, ns))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.Delete(cleanup, ns, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &ns.UID}}); err != nil {
			t.Errorf("tenant network namespace cleanup: %v", err)
		}
	})
	no, yes := false, true
	uid := int64(65532)
	var results []map[string]string
	probe := func(name, mode string) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name,
			// Copying control-plane labels must not escape namespace-owned policy.
			Labels: map[string]string{"app": "celln-controller-proof", "app.kubernetes.io/name": "sympozium-controller"}},
			Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &no,
				Containers: []corev1.Container{{Name: "probe", Image: image, ImagePullPolicy: corev1.PullNever,
					Command: []string{"/probe"}, Args: append([]string{mode}, endpoints...),
					SecurityContext: &corev1.SecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
				}},
			},
		}
		must(t, c.Create(ctx, pod))
		for end := time.Now().Add(35 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
			must(t, c.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			if pod.Status.Phase == corev1.PodSucceeded {
				results = append(results, map[string]string{"mode": mode, "pod": pod.Name, "uid": string(pod.UID), "node": pod.Spec.NodeName})
				return
			}
			if pod.Status.Phase == corev1.PodFailed {
				t.Fatalf("tenant %s probe failed: %v", mode, pod.Status.ContainerStatuses)
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("tenant network probe did not finish")
	}
	probe("before-policy", "allow")
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "deny-celln-host", Namespace: ns.Name}, Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: []string{"10.89.0.1/32"}}}}}},
	}}
	must(t, c.Create(ctx, policy))
	// New Pods are created after policy publication; no pre-existing TCP flow
	// is mistaken for enforcement or revocation of an established connection.
	probe("with-policy", "deny")
	must(t, c.Delete(ctx, policy, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &policy.UID}}))
	probe("after-policy-removal", "allow")
	writeJSON(t, filepath.Join(evidence, "tenant-host-network.json"), map[string]any{
		"status": "network-checks-passed", "namespace": ns.Name, "image": image, "probes": results,
		"endpoints": endpoints, "credentialsMounted": false, "controlPlaneLabelsSpoofed": true,
		"scope": fmt.Sprintf("isolated tenant namespace egress exclusion for %s; before/deny/after TCP probes to actual TLS issuer/router; not host firewall, hostile cluster admin, additive allow-policy bypass or established-flow revocation", "10.89.0.1/32"),
	})
}
