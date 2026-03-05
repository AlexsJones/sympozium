// Package controller contains the reconciliation logic for Sympozium CRDs.
package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	sympoziumv1alpha1 "github.com/alexsjones/sympozium/api/v1alpha1"
)

const sympoziumInstanceFinalizer = "sympozium.ai/finalizer"

// SympoziumInstanceReconciler reconciles a SympoziumInstance object.
type SympoziumInstanceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	ImageTag string // release tag for Sympozium images
}

// +kubebuilder:rbac:groups=sympozium.ai,resources=sympoziuminstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sympozium.ai,resources=sympoziuminstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sympozium.ai,resources=sympoziuminstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets;configmaps;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles SympoziumInstance reconciliation.
func (r *SympoziumInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("sympoziuminstance", req.NamespacedName)

	var instance sympoziumv1alpha1.SympoziumInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !instance.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&instance, sympoziumInstanceFinalizer) {
			log.Info("Cleaning up instance resources")
			if err := r.cleanupChannelDeployments(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanupWebEndpoint(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanupMemoryConfigMap(ctx, &instance); err != nil {
				log.Error(err, "failed to cleanup memory ConfigMap")
			}
			patch := client.MergeFrom(instance.DeepCopy())
			controllerutil.RemoveFinalizer(&instance, sympoziumInstanceFinalizer)
			if err := r.Patch(ctx, &instance, patch); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(&instance, sympoziumInstanceFinalizer) {
		patch := client.MergeFrom(instance.DeepCopy())
		controllerutil.AddFinalizer(&instance, sympoziumInstanceFinalizer)
		if err := r.Patch(ctx, &instance, patch); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch so subsequent operations use the latest resourceVersion.
		if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile channel deployments
	if err := r.reconcileChannels(ctx, &instance); err != nil {
		log.Error(err, "failed to reconcile channels")
		statusBase := instance.DeepCopy()
		instance.Status.Phase = "Error"
		_ = r.Status().Patch(ctx, &instance, client.MergeFrom(statusBase))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Reconcile memory ConfigMap
	if err := r.reconcileMemoryConfigMap(ctx, log, &instance); err != nil {
		log.Error(err, "failed to reconcile memory ConfigMap")
	}

	// Reconcile web endpoint
	if err := r.reconcileWebEndpoint(ctx, &instance); err != nil {
		log.Error(err, "failed to reconcile web endpoint")
	}

	// Count active agent pods
	activeCount, err := r.countActiveAgentPods(ctx, &instance)
	if err != nil {
		log.Error(err, "failed to count agent pods")
	}

	// Update status
	statusBase := instance.DeepCopy()
	instance.Status.Phase = "Running"
	instance.Status.ActiveAgentPods = activeCount
	if err := r.Status().Patch(ctx, &instance, client.MergeFrom(statusBase)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// reconcileChannels ensures a Deployment exists for each configured channel.
func (r *SympoziumInstanceReconciler) reconcileChannels(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	channelStatuses := make([]sympoziumv1alpha1.ChannelStatus, 0, len(instance.Spec.Channels))

	for _, ch := range instance.Spec.Channels {
		deployName := fmt.Sprintf("%s-channel-%s", instance.Name, ch.Type)

		// WhatsApp channels need a PVC for credential persistence (QR link survives restarts)
		if ch.Type == "whatsapp" {
			if err := r.ensureWhatsAppPVC(ctx, instance, deployName); err != nil {
				return err
			}
		}

		var deploy appsv1.Deployment
		err := r.Get(ctx, types.NamespacedName{
			Name:      deployName,
			Namespace: instance.Namespace,
		}, &deploy)

		if errors.IsNotFound(err) {
			// Create channel deployment
			deploy := r.buildChannelDeployment(instance, ch, deployName)
			if err := controllerutil.SetControllerReference(instance, deploy, r.Scheme); err != nil {
				return err
			}
			if err := r.Create(ctx, deploy); err != nil {
				return err
			}
			channelStatuses = append(channelStatuses, sympoziumv1alpha1.ChannelStatus{
				Type:   ch.Type,
				Status: "Pending",
			})
		} else if err != nil {
			return err
		} else {
			status := "Connected"
			if deploy.Status.ReadyReplicas == 0 {
				status = "Disconnected"
			}
			channelStatuses = append(channelStatuses, sympoziumv1alpha1.ChannelStatus{
				Type:   ch.Type,
				Status: status,
			})
		}
	}

	instance.Status.Channels = channelStatuses
	return nil
}

// buildChannelDeployment creates a Deployment spec for a channel pod.
func (r *SympoziumInstanceReconciler) buildChannelDeployment(
	instance *sympoziumv1alpha1.SympoziumInstance,
	ch sympoziumv1alpha1.ChannelSpec,
	name string,
) *appsv1.Deployment {
	replicas := int32(1)
	tag := r.ImageTag
	if tag == "" {
		tag = "latest"
	}
	registry := os.Getenv("SYMPOZIUM_IMAGE_REGISTRY")
	if registry == "" {
		registry = "ghcr.io/alexsjones/sympozium"
	}
	image := fmt.Sprintf("%s/channel-%s:%s", registry, ch.Type, tag)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "channel",
				"sympozium.ai/channel":   ch.Type,
				"sympozium.ai/instance":  instance.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"sympozium.ai/component": "channel",
					"sympozium.ai/channel":   ch.Type,
					"sympozium.ai/instance":  instance.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"sympozium.ai/component": "channel",
						"sympozium.ai/channel":   ch.Type,
						"sympozium.ai/instance":  instance.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "channel",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "INSTANCE_NAME", Value: instance.Name},
								{Name: "EVENT_BUS_URL", Value: "nats://nats.sympozium-system.svc:4222"},
								{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: resolveOTelEndpoint(instance)},
								{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"},
								{Name: "OTEL_SERVICE_NAME", Value: fmt.Sprintf("sympozium-channel-%s", ch.Type)},
							},
						},
					},
				},
			},
		},
	}

	// Inject channel credentials from secret (if referenced)
	if ch.ConfigRef.Secret != "" {
		deploy.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ch.ConfigRef.Secret,
					},
				},
			},
		}
	}

	// WhatsApp channels need a persistent volume for credential storage
	if ch.Type == "whatsapp" {
		pvcName := fmt.Sprintf("%s-data", name)
		deploy.Spec.Strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType, // prevent two pods mounting the same PVC
		}
		deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "whatsapp-data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			},
		}
		deploy.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "whatsapp-data",
				MountPath: "/data",
			},
		}
	}

	return deploy
}

// ensureWhatsAppPVC creates a PVC for the WhatsApp credential store if it doesn't exist.
func (r *SympoziumInstanceReconciler) ensureWhatsAppPVC(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance, deployName string) error {
	pvcName := fmt.Sprintf("%s-data", deployName)
	var pvc corev1.PersistentVolumeClaim
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: instance.Namespace}, &pvc)
	if err == nil {
		return nil // already exists
	}
	if !errors.IsNotFound(err) {
		return err
	}

	pvc = corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "channel",
				"sympozium.ai/channel":   "whatsapp",
				"sympozium.ai/instance":  instance.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("256Mi"),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(instance, &pvc, r.Scheme); err != nil {
		return err
	}

	r.Log.Info("Creating WhatsApp credential PVC", "name", pvcName)
	return r.Create(ctx, &pvc)
}

// cleanupChannelDeployments removes channel deployments owned by the instance.
func (r *SympoziumInstanceReconciler) cleanupChannelDeployments(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	var deploys appsv1.DeploymentList
	if err := r.List(ctx, &deploys,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels{"sympozium.ai/instance": instance.Name, "sympozium.ai/component": "channel"},
	); err != nil {
		return err
	}

	for i := range deploys.Items {
		if err := r.Delete(ctx, &deploys.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// countActiveAgentPods counts running agent pods for this instance.
func (r *SympoziumInstanceReconciler) countActiveAgentPods(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) (int, error) {
	var runs sympoziumv1alpha1.AgentRunList
	if err := r.List(ctx, &runs,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels{"sympozium.ai/instance": instance.Name},
	); err != nil {
		return 0, err
	}

	count := 0
	for _, run := range runs.Items {
		if run.Status.Phase == sympoziumv1alpha1.AgentRunPhaseRunning {
			count++
		}
	}
	return count, nil
}

// reconcileMemoryConfigMap ensures the memory ConfigMap exists when memory is
// enabled for the instance. The ConfigMap is named "<instance>-memory" and
// contains a single key "MEMORY.md".
func (r *SympoziumInstanceReconciler) reconcileMemoryConfigMap(ctx context.Context, log logr.Logger, instance *sympoziumv1alpha1.SympoziumInstance) error {
	if instance.Spec.Memory == nil || !instance.Spec.Memory.Enabled {
		return nil
	}

	cmName := fmt.Sprintf("%s-memory", instance.Name)
	var cm corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: instance.Namespace}, &cm)
	if err == nil {
		return nil // Already exists.
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create the memory ConfigMap with initial content.
	initialContent := "# Agent Memory\n\nNo memories recorded yet.\n"
	cm = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/instance":  instance.Name,
				"sympozium.ai/component": "memory",
			},
		},
		Data: map[string]string{
			"MEMORY.md": initialContent,
		},
	}

	if err := controllerutil.SetControllerReference(instance, &cm, r.Scheme); err != nil {
		return err
	}

	log.Info("Creating memory ConfigMap", "name", cmName)
	return r.Create(ctx, &cm)
}

// cleanupMemoryConfigMap deletes the memory ConfigMap for an instance.
func (r *SympoziumInstanceReconciler) cleanupMemoryConfigMap(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	cmName := fmt.Sprintf("%s-memory", instance.Name)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: instance.Namespace,
		},
	}
	if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// reconcileWebEndpoint ensures the web-proxy Deployment, Service, Secret, and
// HTTPRoute exist when the web endpoint is enabled, and tears them down when disabled.
func (r *SympoziumInstanceReconciler) reconcileWebEndpoint(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	if instance.Spec.WebEndpoint == nil || !instance.Spec.WebEndpoint.Enabled {
		return r.cleanupWebEndpoint(ctx, instance)
	}

	log := r.Log.WithValues("instance", instance.Name, "component", "web-endpoint")

	// 1. Ensure API key Secret
	secretName, err := r.ensureWebProxySecret(ctx, instance)
	if err != nil {
		return fmt.Errorf("ensure web proxy secret: %w", err)
	}

	// 2. Reconcile Deployment
	if err := r.reconcileWebProxyDeployment(ctx, instance, secretName); err != nil {
		return fmt.Errorf("reconcile web proxy deployment: %w", err)
	}

	// 3. Reconcile Service
	if err := r.reconcileWebProxyService(ctx, instance); err != nil {
		return fmt.Errorf("reconcile web proxy service: %w", err)
	}

	// 4. Reconcile HTTPRoute (only if hostname is set)
	hostname := instance.Spec.WebEndpoint.Hostname
	if hostname == "" {
		baseDomain := r.getGatewayBaseDomain(ctx, instance.Namespace)
		if baseDomain != "" {
			hostname = instance.Name + "." + baseDomain
		}
	}
	if hostname != "" {
		if err := r.reconcileHTTPRoute(ctx, instance, hostname); err != nil {
			return fmt.Errorf("reconcile HTTPRoute: %w", err)
		}
	}

	// 5. Update status
	url := ""
	if hostname != "" {
		url = "https://" + hostname
	}
	statusBase := instance.DeepCopy()
	instance.Status.WebEndpoint = &sympoziumv1alpha1.WebEndpointStatus{
		Status:         "Ready",
		URL:            url,
		AuthSecretName: secretName,
	}
	if err := r.Status().Patch(ctx, instance, client.MergeFrom(statusBase)); err != nil {
		log.Error(err, "failed to update web endpoint status")
	}

	return nil
}

// ensureWebProxySecret creates or returns the API key Secret for the web proxy.
func (r *SympoziumInstanceReconciler) ensureWebProxySecret(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) (string, error) {
	// Use user-provided secret if specified
	if instance.Spec.WebEndpoint.AuthSecretRef != "" {
		return instance.Spec.WebEndpoint.AuthSecretRef, nil
	}

	secretName := instance.Name + "-web-proxy-key"
	var secret corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: instance.Namespace}, &secret)
	if err == nil {
		return secretName, nil // Already exists
	}
	if !errors.IsNotFound(err) {
		return "", err
	}

	// Generate random API key
	keyBytes := make([]byte, 24)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	apiKey := "sk-" + hex.EncodeToString(keyBytes)

	secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "web-proxy",
				"sympozium.ai/instance":  instance.Name,
			},
		},
		StringData: map[string]string{
			"api-key": apiKey,
		},
	}

	if err := controllerutil.SetControllerReference(instance, &secret, r.Scheme); err != nil {
		return "", err
	}

	r.Log.Info("Creating web proxy API key Secret", "name", secretName)
	return secretName, r.Create(ctx, &secret)
}

// reconcileWebProxyDeployment creates or updates the web-proxy Deployment.
func (r *SympoziumInstanceReconciler) reconcileWebProxyDeployment(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance, secretName string) error {
	deployName := instance.Name + "-web-proxy"

	var deploy appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: instance.Namespace}, &deploy)
	if err == nil {
		return nil // Already exists
	}
	if !errors.IsNotFound(err) {
		return err
	}

	replicas := int32(1)
	tag := r.ImageTag
	if tag == "" {
		tag = "latest"
	}
	registry := os.Getenv("SYMPOZIUM_IMAGE_REGISTRY")
	if registry == "" {
		registry = "ghcr.io/alexsjones/sympozium"
	}
	image := fmt.Sprintf("%s/web-proxy:%s", registry, tag)

	// Build env vars
	env := []corev1.EnvVar{
		{Name: "INSTANCE_NAME", Value: instance.Name},
		{Name: "EVENT_BUS_URL", Value: "nats://nats.sympozium-system.svc:4222"},
		{
			Name: "WEB_PROXY_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "api-key",
				},
			},
		},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: resolveOTelEndpoint(instance)},
		{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"},
		{Name: "OTEL_SERVICE_NAME", Value: "sympozium-web-proxy"},
	}

	if instance.Spec.WebEndpoint.RateLimit != nil {
		if instance.Spec.WebEndpoint.RateLimit.RequestsPerMinute > 0 {
			env = append(env, corev1.EnvVar{
				Name:  "RATE_LIMIT_RPM",
				Value: fmt.Sprintf("%d", instance.Spec.WebEndpoint.RateLimit.RequestsPerMinute),
			})
		}
		if instance.Spec.WebEndpoint.RateLimit.BurstSize > 0 {
			env = append(env, corev1.EnvVar{
				Name:  "RATE_LIMIT_BURST",
				Value: fmt.Sprintf("%d", instance.Spec.WebEndpoint.RateLimit.BurstSize),
			})
		}
	}

	deploy = appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "web-proxy",
				"sympozium.ai/instance":  instance.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"sympozium.ai/component": "web-proxy",
					"sympozium.ai/instance":  instance.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"sympozium.ai/component": "web-proxy",
						"sympozium.ai/instance":  instance.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "sympozium-web-proxy",
					Containers: []corev1.Container{
						{
							Name:            "web-proxy",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 3,
								PeriodSeconds:       5,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             boolPtr(true),
								ReadOnlyRootFilesystem:   boolPtr(true),
								AllowPrivilegeEscalation: boolPtr(false),
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(instance, &deploy, r.Scheme); err != nil {
		return err
	}

	r.Log.Info("Creating web-proxy Deployment", "name", deployName)
	return r.Create(ctx, &deploy)
}

// reconcileWebProxyService creates the ClusterIP Service for the web proxy.
func (r *SympoziumInstanceReconciler) reconcileWebProxyService(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	svcName := instance.Name + "-web-proxy"

	var svc corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: instance.Namespace}, &svc)
	if err == nil {
		return nil // Already exists
	}
	if !errors.IsNotFound(err) {
		return err
	}

	svc = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "web-proxy",
				"sympozium.ai/instance":  instance.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"sympozium.ai/component": "web-proxy",
				"sympozium.ai/instance":  instance.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(instance, &svc, r.Scheme); err != nil {
		return err
	}

	r.Log.Info("Creating web-proxy Service", "name", svcName)
	return r.Create(ctx, &svc)
}

// reconcileHTTPRoute creates an HTTPRoute pointing the hostname to the web-proxy Service.
func (r *SympoziumInstanceReconciler) reconcileHTTPRoute(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance, hostname string) error {
	routeName := instance.Name + "-web"

	var route gatewayv1.HTTPRoute
	err := r.Get(ctx, types.NamespacedName{Name: routeName, Namespace: instance.Namespace}, &route)
	if err == nil {
		return nil // Already exists
	}
	if !errors.IsNotFound(err) {
		return err
	}

	gatewayName := r.getGatewayName(ctx, instance.Namespace)
	gatewayNS := gatewayv1.Namespace(instance.Namespace)

	svcName := instance.Name + "-web-proxy"
	port := gatewayv1.PortNumber(8080)

	route = gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"sympozium.ai/component": "web-proxy",
				"sympozium.ai/instance":  instance.Name,
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      gatewayv1.ObjectName(gatewayName),
						Namespace: &gatewayNS,
					},
				},
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(hostname)},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(svcName),
									Port: &port,
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(instance, &route, r.Scheme); err != nil {
		return err
	}

	r.Log.Info("Creating web-proxy HTTPRoute", "name", routeName, "hostname", hostname)
	return r.Create(ctx, &route)
}

// cleanupWebEndpoint removes web-proxy Deployment, Service, and HTTPRoute by label selector.
// The API key Secret is retained so re-enabling reuses the same key.
func (r *SympoziumInstanceReconciler) cleanupWebEndpoint(ctx context.Context, instance *sympoziumv1alpha1.SympoziumInstance) error {
	labels := client.MatchingLabels{
		"sympozium.ai/component": "web-proxy",
		"sympozium.ai/instance":  instance.Name,
	}
	ns := client.InNamespace(instance.Namespace)

	// Delete Deployments
	var deploys appsv1.DeploymentList
	if err := r.List(ctx, &deploys, ns, labels); err != nil {
		return err
	}
	for i := range deploys.Items {
		if err := r.Delete(ctx, &deploys.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete Services
	var svcs corev1.ServiceList
	if err := r.List(ctx, &svcs, ns, labels); err != nil {
		return err
	}
	for i := range svcs.Items {
		if err := r.Delete(ctx, &svcs.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete HTTPRoutes
	var routes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &routes, ns, labels); err != nil {
		// If Gateway API CRDs are not installed, ignore the error
		if !errors.IsNotFound(err) {
			r.Log.V(1).Info("Could not list HTTPRoutes (Gateway API CRDs may not be installed)", "error", err)
		}
	} else {
		for i := range routes.Items {
			if err := r.Delete(ctx, &routes.Items[i]); err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}

	// Clear status
	statusBase := instance.DeepCopy()
	if instance.Status.WebEndpoint != nil {
		instance.Status.WebEndpoint = nil
		if err := r.Status().Patch(ctx, instance, client.MergeFrom(statusBase)); err != nil {
			r.Log.Error(err, "failed to clear web endpoint status")
		}
	}

	return nil
}

// getGatewayBaseDomain reads the base domain from SympoziumConfig, falling back to env var.
func (r *SympoziumInstanceReconciler) getGatewayBaseDomain(ctx context.Context, namespace string) string {
	var config sympoziumv1alpha1.SympoziumConfig
	if err := r.Get(ctx, types.NamespacedName{Name: "default", Namespace: namespace}, &config); err == nil {
		if config.Spec.Gateway != nil && config.Spec.Gateway.BaseDomain != "" {
			return config.Spec.Gateway.BaseDomain
		}
	}
	return os.Getenv("SYMPOZIUM_GATEWAY_BASE_DOMAIN")
}

// getGatewayName reads the gateway name from SympoziumConfig, falling back to env var.
func (r *SympoziumInstanceReconciler) getGatewayName(ctx context.Context, namespace string) string {
	var config sympoziumv1alpha1.SympoziumConfig
	if err := r.Get(ctx, types.NamespacedName{Name: "default", Namespace: namespace}, &config); err == nil {
		if config.Spec.Gateway != nil && config.Spec.Gateway.Name != "" {
			return config.Spec.Gateway.Name
		}
	}
	name := os.Getenv("SYMPOZIUM_GATEWAY_NAME")
	if name == "" {
		name = "sympozium-gateway"
	}
	return name
}

// SetupWithManager sets up the controller with the Manager.
func (r *SympoziumInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sympoziumv1alpha1.SympoziumInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
