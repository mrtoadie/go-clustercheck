package kube

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// --- kubeconfig types with full context resolution ---

type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Clusters []struct {
		Name    string      `yaml:"name"`
		Cluster kubeCluster `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string   `yaml:"name"`
		User kubeUser `yaml:"user"`
	} `yaml:"users"`
}

type kubeCluster struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
	CertificateAuthorityPath string `yaml:"certificate-authority"`
	InsecureSkipVerify       bool   `yaml:"insecure-skip-tls-verify"`
}

type kubeUser struct {
	Token                 string `yaml:"token"`
	Username              string `yaml:"username"`
	Password              string `yaml:"password"`
	ClientCertificateData string `yaml:"client-certificate-data"`
	ClientKeyData         string `yaml:"client-key-data"`
	ClientCertificatePath string `yaml:"client-certificate"`
	ClientKeyPath         string `yaml:"client-key"`
}

// resolvedConnection bundles everything NewClient needs to dial the API.
type resolvedConnection struct {
	server    string
	tlsCfg    *tls.Config
	token     string
	username  string
	password  string
	hasClient bool // client certificate present - takes precedence over token/basic
}

// Client is a minimal Kubernetes API client speaking plain HTTP(S) with
// full kubeconfig context resolution. Auth flavors supported, in order
// of precedence: client certificate (minikube, kind, kubeadm) >
// bearer token (k3s, service accounts) > basic auth.
type Client struct {
	server string
	http   *http.Client
	authP  resolvedConnection
}

// NewClient builds a Client from the given kubeconfig path.
//
// Resolution follows the kubeconfig spec: current-context selects a
// context entry, which names the cluster and user. Inline base64 blobs
// (certificate-authority-data, client-certificate-data) are preferred;
// file path variants (certificate-authority, client-certificate) are
// the fallback - minikube and kubeadm write those.
func NewClient(kubeconfigPath string) (*Client, error) {
	path, err := resolvePath(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading kubeconfig: %w", err)
	}
	var cfg kubeconfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}

	conn, err := resolveConnection(&cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		server: conn.server,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: conn.tlsCfg,
			},
		},
		authP: *conn,
	}, nil
}

// resolveConnection picks cluster and user via current-context and builds
// the TLS config and credentials from the resolved entries.
func resolveConnection(cfg *kubeconfig) (*resolvedConnection, error) {
	if cfg.CurrentContext == "" {
		return nil, fmt.Errorf("kubeconfig has no current-context")
	}

	// 1. Context entry: current-context -> (cluster, user)
	clusterName, userName := "", ""
	for _, c := range cfg.Contexts {
		if c.Name == cfg.CurrentContext {
			clusterName = c.Context.Cluster
			userName = c.Context.User
			break
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("current-context %q not found in kubeconfig", cfg.CurrentContext)
	}

	// 2. Cluster entry
	var cluster *kubeCluster
	for i := range cfg.Clusters {
		if cfg.Clusters[i].Name == clusterName {
			cluster = &cfg.Clusters[i].Cluster
			break
		}
	}
	if cluster == nil {
		return nil, fmt.Errorf("cluster %q not found", clusterName)
	}
	if cluster.Server == "" {
		return nil, fmt.Errorf("cluster %q has no server URL", clusterName)
	}

	// 3. User entry - may be absent entirely (anonymous access, some k3s setups)
	var user *kubeUser
	for i := range cfg.Users {
		if cfg.Users[i].Name == userName {
			user = &cfg.Users[i].User
			break
		}
	}

	conn := &resolvedConnection{
		server: strings.TrimSuffix(cluster.Server, "/"),
	}

	// 4. TLS: CA either inline base64 or via file path
	tlsCfg := &tls.Config{}
	if cluster.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	} else {
		caPEM, err := loadCA(cluster)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("cluster %q: no valid certificates in CA data", clusterName)
		}
		tlsCfg.RootCAs = pool
	}

	// 5. Client certificate auth: inline data preferred, file path fallback
	if user != nil {
		certPEM, keyPEM, err := loadKeyPair(user)
		if err != nil {
			return nil, err
		}
		if len(certPEM) > 0 && len(keyPEM) > 0 {
			cert, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return nil, fmt.Errorf("parsing client key pair: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
			conn.hasClient = true
		}
		conn.token = user.Token
		conn.username = user.Username
		conn.password = user.Password
	}

	conn.tlsCfg = tlsCfg
	return conn, nil
}

// loadCA returns the cluster CA PEM, inline base64 first, file path second.
func loadCA(cluster *kubeCluster) ([]byte, error) {
	if cluster.CertificateAuthorityData != "" {
		caPEM, err := base64.StdEncoding.DecodeString(cluster.CertificateAuthorityData)
		if err != nil {
			return nil, fmt.Errorf("decoding certificate-authority-data: %w", err)
		}
		if len(caPEM) > 0 {
			return caPEM, nil
		}
	}
	if cluster.CertificateAuthorityPath != "" {
		caPEM, err := os.ReadFile(cluster.CertificateAuthorityPath)
		if err != nil {
			return nil, fmt.Errorf("reading certificate-authority file: %w", err)
		}
		return caPEM, nil
	}
	return nil, fmt.Errorf("cluster has no certificate-authority (data or path)")
}

// loadKeyPair returns client cert and key PEMs (either may be empty when
// the user entry carries no certificate credentials).
func loadKeyPair(user *kubeUser) ([]byte, []byte, error) {
	certPEM, err := base64.StdEncoding.DecodeString(user.ClientCertificateData)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding client-certificate-data: %w", err)
	}
	keyPEM, err := base64.StdEncoding.DecodeString(user.ClientKeyData)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding client-key-data: %w", err)
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		// Fall back to path variants (minikube style)
		if user.ClientCertificatePath != "" && user.ClientKeyPath != "" {
			certPEM, err = os.ReadFile(user.ClientCertificatePath)
			if err != nil {
				return nil, nil, fmt.Errorf("reading client-certificate file: %w", err)
			}
			keyPEM, err = os.ReadFile(user.ClientKeyPath)
			if err != nil {
				return nil, nil, fmt.Errorf("reading client-key file: %w", err)
			}
		}
	}
	return certPEM, keyPEM, nil
}

// applyAuth sets credentials on a request. Client certificate auth works
// at the TLS layer (already configured), so the header only carries
// token/basic when no client cert is present.
func (c *Client) applyAuth(req *http.Request) {
	if c.authP.hasClient {
		return
	}
	switch {
	case c.authP.token != "":
		req.Header.Set("Authorization", "Bearer "+c.authP.token)
	case c.authP.username != "":
		req.SetBasicAuth(c.authP.username, c.authP.password)
	}
}

// GetJSON performs a GET against an API path and decodes the JSON body
// into out. Non-200 responses produce a descriptive error.
func (c *Client) GetJSON(ctx context.Context, apiPath string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.server+apiPath, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	c.applyAuth(req)

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

// RawText performs a GET and returns the body as plain text (for
// non-JSON endpoints like /readyz?verbose).
func (c *Client) RawText(ctx context.Context, apiPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.server+apiPath, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request %s: %w", apiPath, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %d: %s", apiPath, resp.StatusCode, truncate(string(body), 200))
	}
	return string(body), nil
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
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
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
// State.Waiting.Reason catches CrashLoopBackOff/ImagePullBackOff - the
// pod phase stays "Running" while the container is actually down.
// LastState.Terminated.FinishedAt tells us WHEN the last restart
// happened - the difference between an active crashloop and historic noise.
type ContainerStatus struct {
	Name         string `json:"name"`
	RestartCount int    `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
	} `json:"state"`
	LastState struct {
		Terminated *struct {
			FinishedAt string `json:"finishedAt"`
		} `json:"terminated"`
	} `json:"lastState"`
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

// Secret is the secret subset used by the checks.
type Secret struct {
	Meta ObjectMeta        `json:"metadata"`
	Type string            `json:"type"`
	Data map[string][]byte `json:"data"`
}

// SecretList matches a core/v1 Secret list.
type SecretList struct {
	Items []Secret `json:"items"`
}

// SecretsAll fetches all secrets across namespaces.
// CAUTION: secret data is base64-decoded automatically by encoding/json
// into []byte. Handle the content responsibly - the certificate check
// only parses public certificates and never logs private keys.
func (c *Client) SecretsAll(ctx context.Context) (*SecretList, error) {
	var list SecretList
	if err := c.GetJSON(ctx, "/api/v1/secrets", &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// PodDisruptionBudgetList matches policy/v1 PDBs (all namespaces).
type PodDisruptionBudgetList struct {
	Items []struct {
		Meta   ObjectMeta `json:"metadata"`
		Status struct {
			DisruptionsAllowed int `json:"disruptionsAllowed"`
			DesiredHealthy     int `json:"desiredHealthy"`
		} `json:"status"`
	} `json:"items"`
}

// ResourceQuotaList matches core/v1 ResourceQuotas (all namespaces).
type ResourceQuotaList struct {
	Items []struct {
		Meta ObjectMeta `json:"metadata"`
	} `json:"items"`
}

// DeploymentList matches apps/v1 Deployments (all namespaces).
type DeploymentList struct {
	Items []struct {
		Meta ObjectMeta `json:"metadata"`
	} `json:"items"`
}

func (c *Client) PodDisruptionBudgetsAll(ctx context.Context) (*PodDisruptionBudgetList, error) {
	var l PodDisruptionBudgetList
	if err := c.GetJSON(ctx, "/apis/policy/v1/poddisruptionbudgets", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (c *Client) ResourceQuotasAll(ctx context.Context) (*ResourceQuotaList, error) {
	var l ResourceQuotaList
	if err := c.GetJSON(ctx, "/api/v1/resourcequotas", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// DeploymentsIn fetches deployments of one namespace.
func (c *Client) DeploymentsIn(ctx context.Context, ns string) (*DeploymentList, error) {
	var l DeploymentList
	if err := c.GetJSON(ctx, "/apis/apps/v1/namespaces/"+ns+"/deployments", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// DaemonSetList matches apps/v1 DaemonSets (all namespaces).
type DaemonSetList struct {
	Items []struct {
		Meta   ObjectMeta `json:"metadata"`
		Status struct {
			DesiredNumberScheduled int `json:"desiredNumberScheduled"`
			CurrentNumberScheduled int `json:"currentNumberScheduled"`
			NumberAvailable        int `json:"numberAvailable"`
		} `json:"status"`
	} `json:"items"`
}

func (c *Client) DaemonSetsAll(ctx context.Context) (*DaemonSetList, error) {
	var l DaemonSetList
	if err := c.GetJSON(ctx, "/apis/apps/v1/daemonsets", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Service is the service subset used by the checks.
type Service struct {
	Meta ObjectMeta `json:"metadata"`
	Spec struct {
		Type     string            `json:"type"`
		Selector map[string]string `json:"selector,omitempty"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip,omitempty"`
				Hostname string `json:"hostname,omitempty"`
			} `json:"ingress,omitempty"`
		} `json:"loadBalancer,omitempty"`
	} `json:"status"`
}

// ServiceList matches a core/v1 Service list (all namespaces).
type ServiceList struct {
	Items []Service `json:"items"`
}

// EndpointSubset counts ready and not-ready addresses of one endpoint slice.
type EndpointSubset struct {
	Addresses         []struct{} `json:"addresses,omitempty"`
	NotReadyAddresses []struct{} `json:"notReadyAddresses,omitempty"`
}

// Endpoints is the endpoints subset used by the checks.
type Endpoints struct {
	Meta    ObjectMeta       `json:"metadata"`
	Subsets []EndpointSubset `json:"subsets,omitempty"`
}

// EndpointsList matches a core/v1 Endpoints list (all namespaces).
type EndpointsList struct {
	Items []Endpoints `json:"items"`
}

// ServicesAll lists all Services across all namespaces.
func (c *Client) ServicesAll(ctx context.Context) (*ServiceList, error) {
	var l ServiceList
	if err := c.GetJSON(ctx, "/api/v1/services", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// EndpointsAll lists all Endpoints across all namespaces.
func (c *Client) EndpointsAll(ctx context.Context) (*EndpointsList, error) {
	var l EndpointsList
	if err := c.GetJSON(ctx, "/api/v1/endpoints", &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// resolvePath returns the kubeconfig path: explicit argument first,
// then $KUBECONFIG, then the default ~/.kube/config.
func resolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		// $KUBECONFIG may be a colon-separated list - first entry wins,
		// merged configs are out of scope for this client.
		return strings.Split(env, ":")[0], nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}
