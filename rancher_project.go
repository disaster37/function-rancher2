package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/apis/common"
	v2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
	"github.com/crossplane/function-sdk-go/errors"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	input "github.com/disaster37/function-rancher2/input/v1beta1"
	rancherk8s "github.com/disaster37/provider-rancher2/apis/namespaced/k8s/v1"
	"github.com/rancher/terraform-provider-rancher2/rancher2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// ImportProjects imports projects by name with the given rancher client and project requests. The project requests contain the project name and cluster name to search project ID. If both projectName and projectID are specified, projectID will be used.
func (f *Function) ImportProjects(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, projectRequests []*input.ProjectRequest, desired map[resource.Name]*resource.DesiredComposed, rsp *fnv1.RunFunctionResponse) (err error) {
	for _, projectRequest := range projectRequests {
		if err := f.ImportProject(ctx, name, currentNamespace, labels, rancherClient, projectRequest, desired, rsp); err != nil {
			return err
		}
	}
	return nil
}

func (f *Function) ImportProject(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, projectRequest *input.ProjectRequest, desired map[resource.Name]*resource.DesiredComposed, rsp *fnv1.RunFunctionResponse) (err error) {

	clusterId, err := rancherClient.GetClusterIDByName(projectRequest.ClusterName)
	if err != nil {
		return errors.Wrapf(err, "cannot get cluster ID by name %s", projectRequest.ClusterName)
	}

	projectId, err := rancherClient.GetProjectIDByName(projectRequest.Name, clusterId)
	if err != nil {
		return errors.Wrapf(err, "cannot get project ID by name %s", projectRequest.Name)
	}

	// Generate the project import
	project := &rancherk8s.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(fmt.Sprintf("%s-%s", projectRequest.ClusterName, projectRequest.Name)),
			Namespace: currentNamespace,
			Labels:    labels,
			Annotations: map[string]string{
				"crossplane.io/external-name": projectId,
			},
		},
		Spec: rancherk8s.ProjectSpec{
			ManagedResourceSpec: v2.ManagedResourceSpec{
				ManagementPolicies: common.ManagementPolicies{
					common.ManagementActionObserve,
				},
				ProviderConfigReference: projectRequest.ProviderConfigReference,
			},
			ForProvider: rancherk8s.ProjectParameters{
				Name: ptr.To(projectRequest.Name),
			},
		},
	}

	cd, err := composed.From(project)
	if err != nil {
		return errors.Wrap(err, "error when convert project to unstructured")
	}

	desired[resource.Name(fmt.Sprintf("%s_%s", projectRequest.ClusterName, projectRequest.Name))] = &resource.DesiredComposed{Resource: cd}

	return nil
}
