// Package kube provides a minimal Kubernetes API client built directly on
// net/http. It deliberately avoids client-go: the checks only need plain
// GET requests against a handful of endpoints, and skipping client-go keeps
// the binary small and the dependency tree short.
package kube

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// kubeconfig mirrors only the fields we actually need from a standard
// kubeconfig file. Unknown fields are ignored by the YAML decoder.
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
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string `yaml:"token"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
			Username              string `yaml:"username"`
			Password              string `yaml:"password"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// resolvePath returns the kubeconfig path to use, following the usual
// precedence: explicit flag > $KUBECONFIG > ~/.kube/config.
func resolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		// Only the first entry of a colon-separated list is supported;
		// merged kubeconfigs are out of scope for this tool.
		first := filepath.SplitList(env)[0]
		if first != "" {
			return first, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// load parses the kubeconfig file and returns the decoded structure.
func load(path string) (*kubeconfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &kc, nil
}

// selectClusterAndUser resolves the current context into its cluster and
// user entries. Returns an error if the context or its references are missing.
func (kc *kubeconfig) selectClusterAndUser() (clusterName, userName string, err error) {
	name := kc.CurrentContext
	if name == "" {
		return "", "", errors.New("kubeconfig has no current-context set")
	}
	for _, c := range kc.Contexts {
		if c.Name == name {
			return c.Context.Cluster, c.Context.User, nil
		}
	}
	return "", "", fmt.Errorf("current context %q not found in kubeconfig", name)
}

// decodeBase64 decodes a kubeconfig data field, tolerating empty input.
func decodeBase64(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// buildTLSConfig assembles the TLS configuration from the cluster CA
// (certificate-authority-data) - matching how kubectl talks to the API.
func (kc *kubeconfig) buildTLSConfig(clusterName string) (*tls.Config, error) {
	for _, c := range kc.Clusters {
		if c.Name != clusterName {
			continue
		}
		tlsCfg := &tls.Config{
			InsecureSkipVerify: c.Cluster.InsecureSkipTLSVerify,
		}
		caPEM, err := decodeBase64(c.Cluster.CertificateAuthorityData)
		if err != nil {
			return nil, fmt.Errorf("decoding certificate-authority-data: %w", err)
		}
		if len(caPEM) > 0 {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				return nil, errors.New("certificate-authority-data contains no valid PEM certificates")
			}
			tlsCfg.RootCAs = pool
		}
		return tlsCfg, nil
	}
	return nil, fmt.Errorf("cluster %q not found in kubeconfig", clusterName)
}
