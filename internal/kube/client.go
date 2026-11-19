package kube

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// authParams carries the resolved credentials for API requests.
type authParams struct {
	token      string
	username   string
	password   string
	clientCert *tls.Certificate
}

// Client is a minimal Kubernetes API client.
type Client struct {
	server string
	http   *http.Client
	authP  *authParams
}

// NewClient builds a Client from the given kubeconfig path.
func NewClient(kubeconfigPath string) (*Client, error) {
	path, err := resolvePath(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	kc, err := load(path)
	if err != nil {
		return nil, err
	}
	clusterName, userName, err := kc.selectClusterAndUser()
	if err != nil {
		return nil, err
	}

	tlsCfg, err := kc.buildTLSConfig(clusterName)
	if err != nil {
		return nil, err
	}

	c := &Client{
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		},
	}

	// Find the server URL for the selected cluster.
	for _, cl := range kc.Clusters {
		if cl.Name == clusterName {
			c.server = strings.TrimSuffix(cl.Cluster.Server, "/")
			break
		}
	}
	if c.server == "" {
		return nil, fmt.Errorf("no server URL found for cluster %q", clusterName)
	}

	// Resolve credentials once. Supports token, client certificate and
	// basic auth - the variants commonly found in kubeconfigs. A missing
	// user entry falls back to anonymous access (some k3s setups rely on it).
	c.authP = &authParams{}
	for _, u := range kc.Users {
		if u.Name != userName {
			continue
		}
		c.authP.token = u.User.Token
		c.authP.username = u.User.Username
		c.authP.password = u.User.Password

		certPEM, err := base64.StdEncoding.DecodeString(u.User.ClientCertificateData)
		if err != nil {
			return nil, fmt.Errorf("decoding client-certificate-data: %w", err)
		}
		keyPEM, err := base64.StdEncoding.DecodeString(u.User.ClientKeyData)
		if err != nil {
			return nil, fmt.Errorf("decoding client-key-data: %w", err)
		}
		if len(certPEM) > 0 && len(keyPEM) > 0 {
			cert, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return nil, fmt.Errorf("invalid client key pair: %w", err)
			}
			c.authP.clientCert = &cert
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
		break
	}

	return c, nil
}

// GetJSON performs a GET against an API path and decodes the JSON body
// into out. Non-200 responses produce a descriptive error.
func (c *Client) GetJSON(ctx context.Context, apiPath string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.server+apiPath, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	switch {
	case c.authP.token != "":
		req.Header.Set("Authorization", "Bearer "+c.authP.token)
	case c.authP.username != "":
		req.SetBasicAuth(c.authP.username, c.authP.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", apiPath, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d: %s", apiPath, resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding %s: %w", apiPath, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---- Typed responses for the endpoints used by checks ----

// VersionResponse matches the /version endpoint.
type VersionResponse struct {
	Major      string `json:"major"`
	Minor      string `json:"minor"`
	GitVersion string `json:"gitVersion"`
}

// ObjectMeta is the metadata subset shared by all list responses.
type ObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// NodeList matches a core/v1 Node list.
type NodeList struct {
	Items []struct {
		Meta   ObjectMeta `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	} `json:"items"`
}

// ContainerStatus carries per-container runtime info of a pod.
type ContainerStatus struct {
	Name         string `json:"name"`
	RestartCount int    `json:"restartCount"`
}

// Pod is the pod subset used by the checks.
type Pod struct {
	Meta   ObjectMeta `json:"metadata"`
	Status struct {
		Phase             string            `json:"phase"`
		ContainerStatuses []ContainerStatus `json:"containerStatuses"`
	} `json:"status"`
}

// PodList matches a core/v1 Pod list.
type PodList struct {
	Items []Pod `json:"items"`
}

// Namespace is the namespace subset used by the checks (labels matter for PSA).
type Namespace struct {
	Meta ObjectMeta `json:"metadata"`
}

// NamespaceList matches a core/v1 Namespace list.
type NamespaceList struct {
	Items []Namespace `json:"items"`
}

// PersistentVolumeList matches a core/v1 PersistentVolume list.
type PersistentVolumeList struct {
	Items []struct {
		Meta   ObjectMeta `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
		Spec struct {
			ClaimRef *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"claimRef"`
		} `json:"spec"`
	} `json:"items"`
}

// PersistentVolumeClaimList matches a core/v1 PVC list (all namespaces).
type PersistentVolumeClaimList struct {
	Items []struct {
		Meta   ObjectMeta `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

// StorageClassList matches storage.k8s.io/v1 StorageClasses.
type StorageClassList struct {
	Items []struct {
		Meta ObjectMeta `json:"metadata"`
	} `json:"items"`
}

// NetworkPolicyList matches networking.k8s.io/v1 NetworkPolicies (all namespaces).
type NetworkPolicyList struct {
	Items []struct {
		Meta ObjectMeta `json:"metadata"`
	} `json:"items"`
}

// ---- Convenience wrappers used by checks ----

func (c *Client) Version(ctx context.Context) (*VersionResponse, error) {
	var v VersionResponse
	if err := c.GetJSON(ctx, "/version", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) Nodes(ctx context.Context) (*NodeList, error) {
	var n NodeList
	if err := c.GetJSON(ctx, "/api/v1/nodes", &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (c *Client) PodsAll(ctx context.Context) (*PodList, error) {
	var p PodList
	if err := c.GetJSON(ctx, "/api/v1/pods", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) Namespaces(ctx context.Context) (*NamespaceList, error) {
	var ns NamespaceList
	if err := c.GetJSON(ctx, "/api/v1/namespaces", &ns); err != nil {
		return nil, err
	}
	return &ns, nil
}

func (c *Client) PersistentVolumes(ctx context.Context) (*PersistentVolumeList, error) {
	var pv PersistentVolumeList
	if err := c.GetJSON(ctx, "/api/v1/persistentvolumes", &pv); err != nil {
		return nil, err
	}
	return &pv, nil
}

func (c *Client) PersistentVolumeClaimsAll(ctx context.Context) (*PersistentVolumeClaimList, error) {
	var pvc PersistentVolumeClaimList
	if err := c.GetJSON(ctx, "/api/v1/persistentvolumeclaims", &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil
}

func (c *Client) StorageClasses(ctx context.Context) (*StorageClassList, error) {
	var sc StorageClassList
	if err := c.GetJSON(ctx, "/apis/storage.k8s.io/v1/storageclasses", &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

func (c *Client) NetworkPoliciesAll(ctx context.Context) (*NetworkPolicyList, error) {
	var np NetworkPolicyList
	if err := c.GetJSON(ctx, "/apis/networking.k8s.io/v1/networkpolicies", &np); err != nil {
		return nil, err
	}
	return &np, nil
}
