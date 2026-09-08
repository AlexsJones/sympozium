// Fixture setup for the isolated catalogue execution proof. This is not an
// admission service: it installs operator-selected public fixture metadata only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnauthority"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type catalogue struct {
	RuntimeSpec api.AgentRuntimeSpec `json:"runtimeSpec"`
	Tools       []struct {
		Name string            `json:"name"`
		Spec api.CellnToolSpec `json:"spec"`
	} `json:"tools"`
}

func main() {
	var kubeconfig, cataloguePath string
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Absolute isolated Kind kubeconfig")
	flag.StringVar(&cataloguePath, "catalogue", "", "Absolute operator-selected public catalogue fixture")
	flag.Parse()
	if err := setup(kubeconfig, cataloguePath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func setup(kubeconfig, cataloguePath string) error {
	if !filepath.IsAbs(kubeconfig) || !filepath.IsAbs(cataloguePath) {
		return fmt.Errorf("explicit absolute kubeconfig and catalogue paths required")
	}
	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return err
	}
	if config.CurrentContext != "kind-celln-deployed" {
		return fmt.Errorf("only kind-celln-deployed is permitted")
	}
	rest, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	c, err := client.New(rest, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var deployment appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Namespace: "sympozium-system", Name: "harness-proof-controller"}, &deployment); err != nil {
		return err
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 || deployment.Status.Replicas != 0 || deployment.Status.ObservedGeneration < deployment.Generation {
		return fmt.Errorf("isolated proof controller must be stopped before pending run creation")
	}
	raw, err := os.ReadFile(cataloguePath)
	if err != nil {
		return err
	}
	var source catalogue
	if err := json.Unmarshal(raw, &source); err != nil {
		return err
	}
	if source.RuntimeSpec.Celln == nil || len(source.Tools) != 2 {
		return fmt.Errorf("native runtime and exactly two fixture tools required")
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "celln-catalogue-proof-", Labels: map[string]string{"sympozium.ai/celln-catalogue-proof": "true"}}}
	if err := c.Create(ctx, ns); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			// A failed setup never hands the namespace to the controller. Delete
			// only the namespace whose API-assigned UID this process created.
			uid := ns.UID
			if err := c.Delete(cleanup, ns, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
				fmt.Fprintf(os.Stderr, "fixture namespace cleanup failed for %s: %v\n", ns.Name, err)
			}
		}
	}()
	report, err := populate(ctx, c, ns.Name, source)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return err
	}
	success = true // caller owns cleanup after receiving the complete report
	return nil
}

func populate(ctx context.Context, c client.Client, namespace string, source catalogue) (map[string]any, error) {
	rt := &api.AgentRuntime{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "runtime"}, Spec: source.RuntimeSpec}
	agent := &api.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "agent"}, Spec: api.AgentSpec{RuntimeRef: "runtime"}}
	for _, obj := range []client.Object{rt, agent} {
		if err := c.Create(ctx, obj); err != nil {
			return nil, err
		}
		// Read persisted/defaulted specs, never assume fixture UIDs or defaults.
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return nil, err
		}
	}
	aid, err := cellnauthority.IdentifySubject("Agent", agent.ObjectMeta, agent.Spec)
	if err != nil {
		return nil, err
	}
	rid, err := cellnauthority.IdentifySubject("AgentRuntime", rt.ObjectMeta, rt.Spec)
	if err != nil {
		return nil, err
	}
	var grants []cellnauthority.Grant
	var selected []cellnauthority.Selection
	for _, tool := range source.Tools {
		obj := &api.CellnTool{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: tool.Name}, Spec: tool.Spec}
		if err := c.Create(ctx, obj); err != nil {
			return nil, err
		}
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return nil, err
		}
		id, err := cellnauthority.Identify(*obj)
		if err != nil {
			return nil, err
		}
		grants = append(grants, cellnauthority.Grant{Tool: id, Limits: obj.Spec.Limits})
		selected = append(selected, cellnauthority.Selection{Name: obj.Name, Revision: obj.Spec.Revision})
	}
	createDocument := func(name, key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return c.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: map[string]string{key: string(raw)}})
	}
	for _, layer := range []string{"operator", "runtime", "agent"} {
		if err := createDocument(layer, "grants.json", cellnauthority.GrantDocument{APIVersion: "sympozium.ai/celln-grants-v1", Layer: layer, Agent: aid, Runtime: rid, Grants: grants}); err != nil {
			return nil, err
		}
	}
	model := cellnauthority.ModelPolicyDocument{APIVersion: "sympozium.ai/celln-model-policy-v1", Agent: aid, Runtime: rid, Provider: "deepseek", Model: "deepseek-chat", URL: "https://api.deepseek.com/chat/completions", CredentialProfile: "catalogue-proof", MaxRequests: 3, MaxOutputTokens: 512, MaxTotalOutputTokens: 1536}
	if err := createDocument("model", "model-policy.json", model); err != nil {
		return nil, err
	}
	var run api.AgentRun
	if err := json.Unmarshal([]byte(`{"spec":{"agentRef":"agent","agentId":"default","sessionKey":"catalogue-proof","backend":"celln","task":"Call uppercase with text celln, then call length with the returned text. Use both tools exactly once in that order. Finally answer exactly: CELLN has length 5","systemPrompt":"Use the explicitly lent tools. Do not calculate tool results yourself.","model":{"provider":"deepseek","model":"deepseek-chat"},"timeout":"180s"}}`), &run); err != nil {
		return nil, err
	}
	run.Namespace, run.Name = namespace, "run"
	run.Spec.CellnSelection = &api.CellnCatalogueSelection{ToolRefs: []api.CellnCatalogueToolRef{}}
	for _, ref := range selected {
		run.Spec.CellnSelection.ToolRefs = append(run.Spec.CellnSelection.ToolRefs, api.CellnCatalogueToolRef{Name: ref.Name, Revision: ref.Revision})
	}
	if err := c.Create(ctx, &run); err != nil {
		return nil, err
	}
	l := cellnauthority.Loader{Reader: c, OperatorSource: types.NamespacedName{Namespace: namespace, Name: "operator"}, RuntimeSource: types.NamespacedName{Namespace: namespace, Name: "runtime"}, AgentSource: types.NamespacedName{Namespace: namespace, Name: "agent"}}
	frozen, err := l.FreezeRun(ctx, client.ObjectKeyFromObject(&run), selected, 33554432)
	if err != nil {
		return nil, err
	}
	ml := cellnauthority.ModelLoader{Selection: l, Source: types.NamespacedName{Namespace: namespace, Name: "model"}}
	approval, err := ml.Resolve(ctx, *frozen)
	if err != nil {
		return nil, err
	}
	return map[string]any{"namespace": namespace, "run": run.Name, "frozen": frozen, "modelApproval": approval, "executionAuthorized": false}, nil
}
