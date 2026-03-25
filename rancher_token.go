package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/function-sdk-go/errors"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	"github.com/crossplane/function-sdk-go/response"
	input "github.com/disaster37/function-rancher2/input/v1beta1"
	managementClient "github.com/rancher/rancher/pkg/client/generated/management/v3"
	"github.com/rancher/terraform-provider-rancher2/rancher2"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateRancherProviders generates rancher provider credentials and providers with the given rancher client and provider credential requests. The provider credential requests contain the information to generate the rancher provider credentials and providers. If the token already exist and is still valid, it will reuse the existing token instead of generating a new one.
func (f *Function) GenerateTokens(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, tokenRequests []*input.GenerateTokenRequest, desired map[resource.Name]*resource.DesiredComposed, observed map[resource.Name]resource.ObservedComposed, requirement map[string][]resource.Required, rsp *fnv1.RunFunctionResponse) (err error) {
	for _, tokenRequest := range tokenRequests {
		if err := f.GenerateToken(ctx, name, currentNamespace, labels, rancherClient, tokenRequest, desired, observed, requirement, rsp); err != nil {
			return err
		}
	}
	return nil
}

// GenerateToken generates a token and inject it on context
func (f *Function) GenerateToken(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, tokenRequest *input.GenerateTokenRequest, desired map[resource.Name]*resource.DesiredComposed, observed map[resource.Name]resource.ObservedComposed, requirement map[string][]resource.Required, rsp *fnv1.RunFunctionResponse) (err error) {

	// Check if token already exist and if is still valid
	secretResourceName := strings.ReplaceAll(fmt.Sprintf("%s_token", tokenRequest.Name), "-", "_")
	secretName := strings.ReplaceAll(fmt.Sprintf("%s-token", tokenRequest.Name), "_", "-")

	// Check if token already exist and if is still valid
	token, renew, err := getExistingRancherToken2(rancherClient, observed, secretResourceName, secretName)
	if err != nil {
		return errors.Wrap(err, "cannot get existing rancher token")
	}

	if renew {
		token, err = generateToken2(tokenRequest, requirement)
		if err != nil {
			return errors.Wrap(err, "cannot generate rancher token")
		}
	}

	if err = computeSecretToken(secretName, secretResourceName, currentNamespace, labels, token, desired); err != nil {
		return errors.Wrap(err, "cannot compute composed rancher provider")
	}

	response.SetContextKey(rsp, fmt.Sprintf("token.%s/%s", Key, tokenRequest.Name), structpb.NewStringValue(token))

	return nil
}

// getExistingRancherToken retrieves an existing rancher token from the observed resources. If the token is not found or is expired, it indicates that a new token needs to be generated.
func getExistingRancherToken2(rancherClient *rancher2.Config, observed map[resource.Name]resource.ObservedComposed, secretResourceName string, secretName string) (token string, renew bool, err error) {

	if v, ok := observed[resource.Name(secretResourceName)]; ok {
		tokenB64, err := fieldpath.Pave(v.Resource.Object).GetString("data.token")
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", false, errors.Errorf("secret %q does not contain key token", secretName)
			}

			return "", false, errors.Wrap(err, "fetching observed rancher provider secret")
		}

		tokenBytes, err := base64.StdEncoding.DecodeString(tokenB64)
		if err != nil {
			return "", false, errors.Wrap(err, "decoding observed rancher provider secret token")
		}
		token = string(tokenBytes)

		options := rancherClient.CreateClientOpts()
		options.URL = options.URL + "/v3"
		options.TokenKey = token

		tmpRancherClient, err := managementClient.NewClient(options)
		if err != nil {
			if !IsNotFound(err) && !IsForbidden(err) && !IsUnauthorized(err) {
				return "", false, errors.Wrap(err, "cannot create rancher client with token")
			}

			return "", true, nil
		}

		tokenId := strings.Split(token, ":")[0]
		tokenTmp, err := tmpRancherClient.Token.ByID(tokenId)
		if err != nil {
			if !IsNotFound(err) && !IsForbidden(err) && !IsUnauthorized(err) {
				return "", false, errors.Wrapf(err, "cannot get token by ID %s", tokenId)
			}

			return "", true, nil

		} else {
			if !*tokenTmp.Enabled || tokenTmp.Expired {
				renew = true
			}
		}

	} else {
		return "", true, nil
	}

	return token, renew, nil
}

func generateToken2(tokenRequest *input.GenerateTokenRequest, requirement map[string][]resource.Required) (token string, err error) {
	var (
		username string
		password string
	)

	// Get username from requirement
	if v, ok := requirement[strings.ReplaceAll(fmt.Sprintf("%s_username", tokenRequest.Name), "-", "_")]; ok {
		usernameB64, err := fieldpath.Pave(v[0].Resource.Object).GetString("data." + tokenRequest.UsernameSecretRef.Key)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", errors.Errorf("rancher username secret %q does not contain key %q", tokenRequest.UsernameSecretRef.Name, tokenRequest.UsernameSecretRef.Key)
			}

			return "", errors.Wrapf(err, "fetching rancher username secret %q", tokenRequest.UsernameSecretRef.Name)
		}

		usernameBytes, err := base64.StdEncoding.DecodeString(usernameB64)
		if err != nil {
			return "", errors.Wrapf(err, "decoding rancher username secret %q", tokenRequest.UsernameSecretRef.Name)
		}

		username = string(usernameBytes)
	} else {
		return "", errors.Errorf("Rancher username secret %q not found in required resources", tokenRequest.UsernameSecretRef.Name)
	}

	// Get password from requirement
	if v, ok := requirement[strings.ReplaceAll(fmt.Sprintf("%s_password", tokenRequest.Name), "-", "_")]; ok {
		passwordB64, err := fieldpath.Pave(v[0].Resource.Object).GetString("data." + tokenRequest.PasswordSecretRef.Key)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", errors.Errorf("rancher password secret %q does not contain key %q", tokenRequest.PasswordSecretRef.Name, tokenRequest.PasswordSecretRef.Key)
			}

			return "", errors.Wrapf(err, "fetching rancher password secret %q", tokenRequest.PasswordSecretRef.Name)
		}

		passwordBytes, err := base64.StdEncoding.DecodeString(passwordB64)
		if err != nil {
			return "", errors.Wrapf(err, "decoding rancher password secret %q", tokenRequest.PasswordSecretRef.Name)
		}

		password = string(passwordBytes)
	} else {
		return "", errors.Errorf("Rancher password secret %q not found in required resources", tokenRequest.PasswordSecretRef.Name)
	}

	authType := map[string]string{
		"local":           "local",
		"activedirectory": "activeDirectory",
		"adfs":            "adfs",
		"azuread":         "azureAD",
		"freeipa":         "freeIpa",
		"generic_oidc":    "generic_oidc",
		"github":          "github",
		"keycloak":        "keyCloak",
		"okta":            "okta",
		"openldap":        "openLdap",
		"ping":            "ping",
	}

	payload, err := json.Marshal(map[string]any{
		"type":        fmt.Sprintf("%sProvider", authType[tokenRequest.AuthProvider]),
		"username":    username,
		"password":    password,
		"ttl":         tokenRequest.TTL,
		"description": fmt.Sprintf("Rancher provider %s", tokenRequest.Name),
	})
	if err != nil {
		return "", errors.Wrapf(err, "Marshalling login data: %v", tokenRequest.Name)
	}

	loginURL := fmt.Sprintf("%s/v1-public/login", tokenRequest.Url)

	loginHead := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}

	// Login with user and pass
	respBody, _, err := DoPost(loginURL, string(payload), "", true, loginHead)

	if err != nil {
		return "", errors.Wrapf(err, "error when get token %s", tokenRequest.Name)
	}

	if respBody["token"] != nil {
		token, _ = respBody["token"].(string)
	}

	if token == "" {
		return "", errors.Errorf("Token is empty for %s with response %v", tokenRequest.Name, respBody)
	}

	_, _, ok := strings.Cut(token, ":")
	if !ok {
		return "", errors.Errorf("Token format is invalid for %s with token %s", tokenRequest.Name, token)
	}

	return token, nil
}

// computeSecretToken computes the desired secret and provider resources for the rancher token. It creates a secret with the token and a provider that references the secret. The provider type is determined by the provider credential request type. If the provider credential request type is "ProviderConfig", it creates a ProviderConfig resource. If the provider credential request type is "ClusterProviderConfig", it creates a ClusterProviderConfig resource. The function also sets the token in the response context for later use.
func computeSecretToken(secretName string, secretResourceName string, currentNamespace string, labels map[string]string, token string, desired map[resource.Name]*resource.DesiredComposed) (err error) {

	sRancherToken := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: currentNamespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	cd, err := composed.From(sRancherToken)
	if err != nil {
		return errors.Wrap(err, "error when convert secret to unstructured")
	}
	desired[resource.Name(secretResourceName)] = &resource.DesiredComposed{Resource: cd}

	return nil
}
