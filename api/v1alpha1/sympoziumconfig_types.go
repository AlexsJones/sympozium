package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SympoziumConfigSpec defines the desired platform-wide configuration.
type SympoziumConfigSpec struct {
	// Gateway configures the shared Envoy Gateway infrastructure.
	// +optional
	Gateway *GatewaySpec `json:"gateway,omitempty"`
}

// GatewaySpec defines the desired state of the shared Gateway.
type GatewaySpec struct {
	// Enabled is the master switch for the Gateway.
	// When false, Gateway and GatewayClass resources are removed.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// GatewayClassName is the name of the GatewayClass to create.
	// +kubebuilder:default="sympozium"
	// +optional
	GatewayClassName string `json:"gatewayClassName,omitempty"`

	// Name is the Gateway resource name.
	// +kubebuilder:default="sympozium-gateway"
	// +optional
	Name string `json:"name,omitempty"`

	// BaseDomain is the wildcard base domain for instance hostnames.
	// Instances get <name>.<baseDomain> as their hostname.
	// +optional
	BaseDomain string `json:"baseDomain,omitempty"`

	// TLS configures HTTPS on the Gateway.
	// +optional
	TLS *GatewayTLSSpec `json:"tls,omitempty"`
}

// GatewayTLSSpec configures TLS for the Gateway.
type GatewayTLSSpec struct {
	// Enabled turns on the HTTPS listener.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// CertManagerClusterIssuer is the cert-manager ClusterIssuer name.
	// When set, the Gateway is annotated for automatic certificate provisioning.
	// +optional
	CertManagerClusterIssuer string `json:"certManagerClusterIssuer,omitempty"`

	// SecretName is the TLS certificate Secret name.
	// +kubebuilder:default="sympozium-wildcard-cert"
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// SympoziumConfigStatus defines the observed state of SympoziumConfig.
type SympoziumConfigStatus struct {
	// Phase is the current phase: Disabled, Pending, Ready, or Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Gateway reports the observed state of the Gateway.
	// +optional
	Gateway *GatewayStatusInfo `json:"gateway,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GatewayStatusInfo reports the observed Gateway state.
type GatewayStatusInfo struct {
	// Ready indicates whether the Gateway is accepting traffic.
	Ready bool `json:"ready"`

	// Address is the external IP or hostname of the Gateway.
	// +optional
	Address string `json:"address,omitempty"`

	// ListenerCount is the number of active listeners.
	// +optional
	ListenerCount int `json:"listenerCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Gateway",type="boolean",JSONPath=".spec.gateway.enabled"
// +kubebuilder:printcolumn:name="Domain",type="string",JSONPath=".spec.gateway.baseDomain"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SympoziumConfig is the Schema for the sympoziumconfigs API.
// It holds platform-wide settings such as gateway configuration.
type SympoziumConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SympoziumConfigSpec   `json:"spec,omitempty"`
	Status SympoziumConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SympoziumConfigList contains a list of SympoziumConfig.
type SympoziumConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SympoziumConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SympoziumConfig{}, &SympoziumConfigList{})
}
