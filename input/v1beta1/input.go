// Package v1beta1 contains the input type for this Function
// +kubebuilder:object:generate=true
// +groupName=rancher2.fn.crossplane.io
// +versionName=v1beta1
package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/apis/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This isn't a custom resource, in the sense that we never install its CRD.
// It is a KRM-like object, so we generate a CRD to describe its schema.

// TODO: Add your input type here! It doesn't need to be called 'Input', you can
// rename it to anything you like.

// Input can be used to provide input to this Function.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=crossplane
type Input struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The rancher secret key that store the credentials
	// +required
	RancherSecretRef *corev1.SecretKeySelector `json:"rancherSecretRef"`

	// To import project by name, you can use this field to specify the project name. If both projectName and projectID are specified, projectID will be used.
	// +optional
	ImportProjects []*ProjectRequest `json:"importProjects,omitempty"`

	// To generate a provider credential with rancher provider, you can use this field to specify the provider credential to generate. If both RancherCredentialRef and GenerateRancherProviderCredential are specified, GenerateRancherProviderCredential will be used.
	// +optional
	GenerateRancherProviderCredentials []*ProviderCredentialRequest `json:"generateRancherProviderCredentials,omitempty"`
}

// Project is used to specify the project name and cluster name to search project ID. If both projectName and projectID are specified, projectID will be used.
type ProjectRequest struct {
	// The project name
	// +required
	Name string `json:"name"`

	// The cluster name where the project is located
	// +required
	ClusterName string `json:"clusterName"`

	// The provider config reference to observe rancher project
	// +optional
	ProviderConfigReference *common.ProviderConfigReference `json:"providerConfigRef,omitempty"`
}

// ProviderCredentialRequest is used to specify the provider credential to generate with rancher provider.
type ProviderCredentialRequest struct {

	// The provider type to generate.
	// +optional
	// +kubebuilder:default:="ProviderConfig"
	// +kubebuilder:validation:Enum=ProviderConfig;ClusterProviderConfig
	Type string `json:"type,omitempty"`

	// The provider credential name to create
	// +required
	Name string `json:"name"`

	// The rancher URL to generate the provider credential for.
	// +required
	Url string `json:"url"`

	// The auth provider to use to generate the provider credential.
	// +required
	// +kubebuilder:default:="local"
	AuthProvider string `json:"authProvider"`

	// UsernameSecretRef is the reference to the secret key that contains the username to authenticate to rancher.
	// +required
	UsernameSecretRef corev1.SecretKeySelector `json:"usernameSecretRef"`

	// PasswordSecretRef is the reference to the secret key that contains the password to authenticate to rancher.
	// +required
	PasswordSecretRef corev1.SecretKeySelector `json:"passwordSecretRef"`

	// The TTL when generate token for provider
	// +required
	// +kubebuilder:default:=0
	TTL int64 `json:"ttl"`
}
