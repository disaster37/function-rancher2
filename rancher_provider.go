package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/apis/common"
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/function-sdk-go/errors"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	input "github.com/disaster37/function-rancher2/input/v1beta1"
	rancherProvider "github.com/disaster37/provider-rancher2/apis/namespaced/v1beta1"
	managementClient "github.com/rancher/rancher/pkg/client/generated/management/v3"
	"github.com/rancher/terraform-provider-rancher2/rancher2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateRancherProviders generates rancher provider credentials and providers with the given rancher client and provider credential requests. The provider credential requests contain the information to generate the rancher provider credentials and providers. If the token already exist and is still valid, it will reuse the existing token instead of generating a new one.
func (f *Function) GenerateRancherProviders(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, providerCredentialRequests []*input.ProviderCredentialRequest, desired map[resource.Name]*resource.DesiredComposed, observed map[resource.Name]resource.ObservedComposed, requirement map[string][]resource.Required, rsp *fnv1.RunFunctionResponse) (err error) {
	for _, providerCredentialRequest := range providerCredentialRequests {
		if err := f.GenerateRancherProvider(ctx, name, currentNamespace, labels, rancherClient, providerCredentialRequest, desired, observed, requirement, rsp); err != nil {
			return err
		}
	}
	return nil
}

// GenerateRancherProvider generates a rancher provider credential and provider with the given rancher client and provider credential request. The provider credential request contains the information to generate the rancher provider credential and provider. If the token already exist and is still valid, it will reuse the existing token instead of generating a new one.
func (f *Function) GenerateRancherProvider(ctx context.Context, name string, currentNamespace string, labels map[string]string, rancherClient *rancher2.Config, providerCredentialRequest *input.ProviderCredentialRequest, desired map[resource.Name]*resource.DesiredComposed, observed map[resource.Name]resource.ObservedComposed, requirement map[string][]resource.Required, rsp *fnv1.RunFunctionResponse) (err error) {

	secretResourceName := fmt.Sprintf("%s_%s_credential", name, providerCredentialRequest.Name)
	secretName := fmt.Sprintf("%s-credential", name, providerCredentialRequest.Name)
	providerResourceName := fmt.Sprintf("%s_%s_provider", name, providerCredentialRequest.Name)

	// Check if token already exist and if is still valid
	token, renew, err := getExistingRancherToken(rancherClient, observed, secretResourceName, secretName)
	if err != nil {
		return errors.Wrap(err, "cannot get existing rancher token")
	}

	if renew {
		token, err = generateToken(name, providerCredentialRequest, requirement)
		if err != nil {
			return errors.Wrap(err, "cannot generate rancher token")
		}
	}

	if err = computeCompsedRancherProvider(secretName, secretResourceName, providerResourceName, currentNamespace, labels, providerCredentialRequest, token, desired); err != nil {
		return errors.Wrap(err, "cannot compute composed rancher provider")
	}

	return nil
}

// getExistingRancherToken retrieves an existing rancher token from the observed resources. If the token is not found or is expired, it indicates that a new token needs to be generated.
func getExistingRancherToken(rancherClient *rancher2.Config, observed map[resource.Name]resource.ObservedComposed, secretResourceName string, secretName string) (token string, renew bool, err error) {

	var credentialsBytes []byte
	if v, ok := observed[resource.Name(secretResourceName)]; ok {
		credentialsB64, err := fieldpath.Pave(v.Resource.Object).GetString("data." + credKey)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", false, errors.Errorf("secret %q does not contain key %q", secretName, credKey)
			}

			return "", false, errors.Wrap(err, "fetching observed rancher provider secret")
		}

		credentialsBytes, err = base64.StdEncoding.DecodeString(credentialsB64)
		if err != nil {
			return "", false, errors.Wrap(err, "decoding observed rancher provider secret credentials")
		}

		// Unmarshal credentials
		credentials := map[string]string{}
		if err := json.Unmarshal(credentialsBytes, &credentials); err != nil {
			return "", false, errors.Wrap(err, "cannot unmarshal rancher provider credentials")
		}

		if v, ok := credentials["token_key"]; ok {
			options := rancherClient.CreateClientOpts()
			options.URL = options.URL + "/v3"
			options.TokenKey = credentials["token_key"]

			tmpRancherClient, err := managementClient.NewClient(options)
			if err != nil {
				return "", false, errors.Wrap(err, "cannot create rancher client with token")
			}

			tokenId := strings.Split(v, ":")[0]
			tokenTmp, err := tmpRancherClient.Token.ByID(tokenId)
			if err != nil && !IsNotFound(err) && !IsForbidden(err) {
				return "", false, errors.Wrapf(err, "cannot get token by ID %s", tokenId)
			} else {
				if !*tokenTmp.Enabled || tokenTmp.Expired {
					renew = true
				} else {
					token = v
				}
			}
		}
	} else {
		return "", false, errors.New("Rancher provider secret credential not found in observed resources")
	}

	return token, renew, nil
}

// computeCompsedRancherProvider computes the composed rancher provider credential and provider with the given information and put them in the desired composed resources. If the token already exist and is still valid, it will reuse the existing token instead of generating a new one.
func computeCompsedRancherProvider(secretName string, secretResourceName string, providerResourceName string, currentNamespace string, labels map[string]string, providerCredentialRequest *input.ProviderCredentialRequest, token string, desired map[resource.Name]*resource.DesiredComposed) (err error) {

	// Create provider credential secret
	credentials := map[string]string{
		"api_url":   providerCredentialRequest.Url,
		"token_key": token,
		"insecure":  "true",
	}
	dataB, err := json.Marshal(credentials)
	if err != nil {
		return errors.Wrap(err, "cannot marshal rancher provider credentials")
	}
	sRancherCredentials := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: currentNamespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			credKey: dataB,
		},
	}
	cd, err := composed.From(sRancherCredentials)
	if err != nil {
		return errors.Wrap(err, "error when convert secret to unstructured")
	}
	desired[resource.Name(secretResourceName)] = &resource.DesiredComposed{Resource: cd}

	// Create provider
	if providerCredentialRequest.Type == "ProviderConfig" {
		rancherProvider := &rancherProvider.ProviderConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerCredentialRequest.Name,
				Namespace: currentNamespace,
				Labels:    labels,
			},
			Spec: rancherProvider.ProviderConfigSpec{
				Credentials: rancherProvider.ProviderCredentials{
					Source: xpv1.CredentialsSourceSecret,
					CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
						SecretRef: &common.SecretKeySelector{
							SecretReference: common.SecretReference{
								Name:      secretName,
								Namespace: currentNamespace,
							},
							Key: credKey,
						},
					},
				},
			},
		}
		cd, err := composed.From(rancherProvider)
		if err != nil {
			return errors.Wrap(err, "error when convert secret to unstructured")
		}
		desired[resource.Name(providerResourceName)] = &resource.DesiredComposed{Resource: cd}
	} else if providerCredentialRequest.Type == "ClusterProviderConfig" {
		rancherProvider := &rancherProvider.ClusterProviderConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerCredentialRequest.Name,
				Namespace: currentNamespace,
				Labels:    labels,
			},
			Spec: rancherProvider.ProviderConfigSpec{
				Credentials: rancherProvider.ProviderCredentials{
					Source: xpv1.CredentialsSourceSecret,
					CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
						SecretRef: &common.SecretKeySelector{
							SecretReference: common.SecretReference{
								Name:      secretName,
								Namespace: currentNamespace,
							},
							Key: credKey,
						},
					},
				},
			},
		}
		cd, err := composed.From(rancherProvider)
		if err != nil {
			return errors.Wrap(err, "error when convert secret to unstructured")
		}
		desired[resource.Name(providerResourceName)] = &resource.DesiredComposed{Resource: cd}
	} else {
		return errors.Errorf("unsupported provider credential request type %q", providerCredentialRequest.Type)
	}

	return nil
}

func generateToken(name string, providerCredentialRequest *input.ProviderCredentialRequest, requirement map[string][]resource.Required) (token string, err error) {
	var (
		username string
		password string
	)

	// Get username from requirement
	if v, ok := requirement[fmt.Sprintf("%s_%s_username", name, providerCredentialRequest.Name)]; ok {
		usernameB64, err := fieldpath.Pave(v[0].Resource.Object).GetString("data." + providerCredentialRequest.UsernameSecretRef.Key)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", errors.Errorf("rancher username secret %q does not contain key %q", providerCredentialRequest.UsernameSecretRef.Name, providerCredentialRequest.UsernameSecretRef.Key)
			}

			return "", errors.Wrapf(err, "fetching rancher username secret %q", providerCredentialRequest.UsernameSecretRef.Name)
		}

		usernameBytes, err := base64.StdEncoding.DecodeString(usernameB64)
		if err != nil {
			return "", errors.Wrapf(err, "decoding rancher username secret %q", providerCredentialRequest.UsernameSecretRef.Name)
		}

		username = string(usernameBytes)
	} else {
		return "", errors.Errorf("Rancher username secret %q not found in required resources", providerCredentialRequest.UsernameSecretRef.Name)
	}

	// Get password from requirement
	if v, ok := requirement[fmt.Sprintf("%s_%s_password", name, providerCredentialRequest.Name)]; ok {
		passwordB64, err := fieldpath.Pave(v[0].Resource.Object).GetString("data." + providerCredentialRequest.PasswordSecretRef.Key)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				return "", errors.Errorf("rancher password secret %q does not contain key %q", providerCredentialRequest.PasswordSecretRef.Name, providerCredentialRequest.PasswordSecretRef.Key)
			}

			return "", errors.Wrapf(err, "fetching rancher password secret %q", providerCredentialRequest.PasswordSecretRef.Name)
		}

		passwordBytes, err := base64.StdEncoding.DecodeString(passwordB64)
		if err != nil {
			return "", errors.Wrapf(err, "decoding rancher password secret %q", providerCredentialRequest.PasswordSecretRef.Name)
		}

		password = string(passwordBytes)
	} else {
		return "", errors.Errorf("Rancher password secret %q not found in required resources", providerCredentialRequest.PasswordSecretRef.Name)
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
		"type":        fmt.Sprintf("%sProvider", authType[providerCredentialRequest.AuthProvider]),
		"username":    username,
		"password":    password,
		"ttl":         providerCredentialRequest.TTL,
		"description": fmt.Sprintf("Rancher provider %s", providerCredentialRequest.Name),
	})
	if err != nil {
		return "", errors.Wrapf(err, "Marshalling login data: %v", providerCredentialRequest.Name)
	}

	loginURL := fmt.Sprintf("%s/v3-public/%sProviders/%s?action=login", providerCredentialRequest.Url, authType[providerCredentialRequest.AuthProvider], providerCredentialRequest.AuthProvider)

	loginHead := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}

	// Login with user and pass
	respBody, _, err := DoPost(loginURL, string(payload), "", true, loginHead)

	if err != nil {
		return "", errors.Wrapf(err, "error when get token %s", providerCredentialRequest.Name)
	}

	if respBody["token"] != nil {
		token, _ = respBody["token"].(string)
	}

	if token == "" {
		return "", errors.Errorf("Token is empty for %s with response %v", providerCredentialRequest.Name, respBody)
	}

	_, _, ok := strings.Cut(token, ":")
	if !ok {
		return "", errors.Errorf("Token format is invalid for %s with token %s", providerCredentialRequest.Name, token)
	}

	return token, nil
}
