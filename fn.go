package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	"github.com/crossplane/function-sdk-go/response"
	input "github.com/disaster37/function-rancher2/input/v1beta1"
	rancherk8s "github.com/disaster37/provider-rancher2/apis/namespaced/k8s/v1"
	rancherProvider "github.com/disaster37/provider-rancher2/apis/namespaced/v1beta1"

	"github.com/rancher/terraform-provider-rancher2/rancher2"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

const (
	credKey = "credentials"
)

var (
	scheme = composed.Scheme
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(ctx context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())

	utilruntime.Must(rancherk8s.AddToScheme(scheme))
	utilruntime.Must(rancherProvider.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	rsp := response.To(req, response.DefaultTTL)

	in := &input.Input{}
	if err := request.GetInput(req, in); err != nil {

		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to get input").
			TargetCompositeAndClaim()

		response.Warning(rsp, errors.New("something went wrong to get input")).
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input from %T", req))
		return rsp, nil
	}

	// Template input resource if needed
	if err := f.TemplateResource(ctx, in, req); err != nil {
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to template input resource").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot template input resource from %T", req))
		return rsp, nil
	}

	// Get resource and fields
	xr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed composite resource from %T", req))
		return rsp, nil
	}
	name, err := xr.Resource.GetString("metadata.name")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read metadata.name field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}
	labels, err := xr.Resource.GetStringObject("metadata.labels")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read metadata.labels field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}
	currentNamespace, err := xr.Resource.GetString("metadata.namespace")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read metadata.namespace field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}

	desired, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired resources from %T", req))
		return rsp, nil
	}

	observed, err := request.GetObservedComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed resources from %T", req))
		return rsp, nil
	}

	// Handle requirements
	if err := f.HandleResourceRequirements(ctx, currentNamespace, name, in, req, rsp); err != nil {
		if IsErrWaitRequiredResources(err) {
			f.log.Info("Waiting for required resources to be available", "requirements", rsp.GetRequirements())
			return rsp, nil
		}

		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to handle resource requirements").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot handle resource requirements from %T", req))
		return rsp, nil
	}
	// Pull extra resources to search rancher secret data
	requirements, err := request.GetRequiredResources(req)
	if err != nil {
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to get required resources").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get required resources from %T", req))
		return rsp, nil
	}

	// Get Rancher secret
	credentials, err := f.GetRancherCredentials(ctx, currentNamespace, name, in.RancherSecretRef, req, requirements, rsp)
	if err != nil {
		if IsErrWaitRequiredResources(err) {
			f.log.Info("Waiting for rancher secret to be available", "requirements", rsp.GetRequirements())
			return rsp, nil
		}
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to get Rancher credentials").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get rancher credentials from %T", req))
		return rsp, nil
	}

	// Get Rancher client
	rancherClient, err := f.GetRancherClient(ctx, currentNamespace, credentials, rsp)
	if err != nil {
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong to get Rancher client").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get rancher client from %T", req))
		return rsp, nil
	}

	// Compute projects
	if len(in.ImportProjects) > 0 {

		if err := f.ImportProjects(ctx, name, currentNamespace, labels, rancherClient, in.ImportProjects, desired, rsp); err != nil {
			response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
				WithMessage("Something went wrong to import projects").
				TargetCompositeAndClaim()

			response.Fatal(rsp, errors.Wrapf(err, "cannot import projects from %T", req))
			return rsp, nil
		}
	}

	if len(in.GenerateRancherProviderCredentials) > 0 {

		if err := f.GenerateRancherProviders(ctx, name, currentNamespace, labels, rancherClient, in.GenerateRancherProviderCredentials, desired, observed, requirements, rsp); err != nil {
			response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
				WithMessage("Something went wrong to generate rancher provider credentials").
				TargetCompositeAndClaim()

			response.Fatal(rsp, errors.Wrapf(err, "cannot generate rancher provider credentials from %T", req))
			return rsp, nil
		}
	}

	if err := response.SetDesiredComposedResources(rsp, desired); err != nil {
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong import desired objects").
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot import desired objects %T", req))
		return rsp, nil
	}

	// You can set a custom status condition on the claim. This allows you to
	// communicate with the user. See the link below for status condition
	// guidance.
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").
		TargetCompositeAndClaim()

	return rsp, nil
}

// HandleResourceRequirements checks the resource requirements and if the rancher secret is not yet available, it will return an error to ask crossplane to wait for the required resources to be available. This function is used in the RunFunction to handle the case when the rancher secret is not yet pulled by crossplane and we need to wait for it before we can get the credentials and create the rancher client.
func (f *Function) HandleResourceRequirements(ctx context.Context, currentNamespace string, name string, input *input.Input, req *fnv1.RunFunctionRequest, rsp *fnv1.RunFunctionResponse) (err error) {

	extraResources := make(map[string]*fnv1.ResourceSelector, len(input.GenerateRancherProviderCredentials))

	// Handler rancher secret as required resource
	if input.RancherSecretRef == nil {
		return errors.New("RancherSecretRef is nil")
	}
	if input.RancherSecretRef.Name == "" {
		return errors.New("RancherSecretRef name is empty")
	}
	if input.RancherSecretRef.Key == "" {
		return errors.New("RancherSecretRef key is empty")
	}

	extraResources[strings.ReplaceAll(fmt.Sprintf("%s_%s", name, input.RancherSecretRef.Name), "-", "_")] = &fnv1.ResourceSelector{
		ApiVersion: "v1",
		Kind:       "Secret",
		Match: &fnv1.ResourceSelector_MatchName{
			MatchName: input.RancherSecretRef.Name,
		},
		Namespace: &currentNamespace,
	}

	// Handle rancher provider credential as required resource if generate rancher provider credential is specified
	for _, providerCredentialRequest := range input.GenerateRancherProviderCredentials {
		if providerCredentialRequest.Name == "" {
			return errors.New("GenerateRancherProviderCredential name is empty")
		}

		extraResources[strings.ReplaceAll(fmt.Sprintf("%s_username", providerCredentialRequest.Name), "-", "_")] = &fnv1.ResourceSelector{
			ApiVersion: "v1",
			Kind:       "Secret",
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: providerCredentialRequest.UsernameSecretRef.Name,
			},
			Namespace: &currentNamespace,
		}

		extraResources[strings.ReplaceAll(fmt.Sprintf("%s_password", providerCredentialRequest.Name), "-", "_")] = &fnv1.ResourceSelector{
			ApiVersion: "v1",
			Kind:       "Secret",
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: providerCredentialRequest.PasswordSecretRef.Name,
			},
			Namespace: &currentNamespace,
		}
	}

	// Add them on the response requirements so that crossplane can pull them before the next call of the function when we can get the credentials and create the rancher client.
	rsp.Requirements = &fnv1.Requirements{Resources: extraResources}

	// If requirements is nil, it means the function is being called for the first time and the resources haven't been pulled yet, we just return here and wait for the next call when the resources are available. If requirements is not nil but the rancher secret is not found, it means the resource is still being pulled, we also return here and wait for the next call.
	if req.RequiredResources == nil {
		f.log.Debug("Rancher secret not yet exiting", "requirements", rsp.GetRequirements())
		return ErrWaitRequiredResources
	}

	return nil
}

// GetRancherSecret adds the rancher secret to the function response requirements so that it can be pulled and used to create the rancher client.
func (f *Function) GetRancherCredentials(ctx context.Context, currentNamespace string, name string, rancherSecretRef *corev1.SecretKeySelector, req *fnv1.RunFunctionRequest, requirements map[string][]resource.Required, rsp *fnv1.RunFunctionResponse) (credentials map[string]string, err error) {

	var credentialsBytes []byte
	if v, ok := requirements[strings.ReplaceAll(fmt.Sprintf("%s_%s", name, rancherSecretRef.Name), "-", "_")]; ok {
		credentialsB64, err := fieldpath.Pave(v[0].Resource.Object).GetString("data." + rancherSecretRef.Key)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return nil, errors.Errorf("rancher secret %q does not contain key %q", rancherSecretRef.Name, rancherSecretRef.Key)
			}

			return nil, errors.Wrap(err, "fetching rancher secret")
		}

		credentialsBytes, err = base64.StdEncoding.DecodeString(credentialsB64)
		if err != nil {
			return nil, errors.Wrap(err, "decoding rancher secret credentials")
		}
	} else {
		return nil, errors.New("Rancher secret credential not found in required resources")
	}

	// Unmarshal credentials
	credentials = map[string]string{}
	if err := json.Unmarshal(credentialsBytes, &credentials); err != nil {
		return nil, errors.Wrap(err, "cannot unmarshal rancher credentials")
	}

	return credentials, nil
}

// GetRancherClient returns a rancher client with the given rancher credential reference.
func (f *Function) GetRancherClient(ctx context.Context, currentNamespace string, rancherCredential map[string]string, rsp *fnv1.RunFunctionResponse) (client *rancher2.Config, err error) {

	if v, ok := rancherCredential["api_url"]; !ok || v == "" {
		return nil, errors.New("Rancher credential api_url is missing or empty")
	}

	if v, ok := rancherCredential["token_key"]; !ok || v == "" {
		return nil, errors.New("Rancher credential token_key is missing or empty")
	}

	options := &rancher2.Config{
		URL:      rancherCredential["api_url"],
		TokenKey: rancherCredential["token_key"],
	}

	if v, ok := rancherCredential["ca_cert"]; ok && v != "" {
		options.CACerts = rancherCredential["ca_cert"]
	}

	if v, ok := rancherCredential["insecure"]; ok && v != "" {
		insecure, err := strconv.ParseBool(rancherCredential["insecure"])
		if err != nil {
			return nil, errors.Wrap(err, "parsing rancher credential insecure field")
		}
		options.Insecure = insecure
	}

	if v, ok := rancherCredential["timeout"]; ok && v != "" {
		timeout, err := time.ParseDuration(rancherCredential["timeout"])
		if err != nil {
			return nil, errors.Wrap(err, "parsing rancher credential timeout field")
		}
		options.Timeout = timeout
	}

	return options, nil
}
