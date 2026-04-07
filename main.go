// main.go
// REST API for /api/v1/getparams.execute with Argo token auth and specific input/output handling

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/EdgeCDN-X/argocd-locations-plugin-generator/output"
	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type RequestPayload struct {
	Input struct {
		Parameters ParameterTypes `json:"parameters"`
	} `json:"input"`
}

type DeploymentPayload struct {
	CacheName    string            `json:"cacheName"`
	Flavor       string            `json:"flavor"`
	Path         string            `json:"path"`
	KeysZone     string            `json:"keysZone"`
	Inactive     string            `json:"inactive"`
	MaxSize      string            `json:"maxSize"`
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

type IngressClassPayload struct {
	IngressClassName string `json:"ingressClassName"`
}

type ProbePayload struct {
	NodeGroupName string `json:"nodegroupName"`
	Flavor        string `json:"flavor,omitempty"`
	NodeName      string `json:"nodeName"`
	Address       string `json:"address"`
}

type ParameterTypes struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Resource  string `json:"resource"`
}

func buildDeploymentPayloads(location *infrastructurev1alpha1.Location) []DeploymentPayload {
	responseParams := make([]DeploymentPayload, 0, len(location.Spec.NodeGroups))

	for _, ng := range location.Spec.NodeGroups {
		responseParams = append(responseParams, DeploymentPayload{
			CacheName:    ng.Name,
			Flavor:       ng.Flavor,
			Path:         ng.CacheConfig.Path,
			KeysZone:     ng.CacheConfig.KeysZone,
			Inactive:     ng.CacheConfig.Inactive,
			MaxSize:      ng.CacheConfig.MaxSize,
			NodeSelector: ng.NodeSelector,
		})
	}

	return responseParams
}

func buildIngressClassPayloads(location *infrastructurev1alpha1.Location) []IngressClassPayload {
	responseParams := []IngressClassPayload{}

	for _, ng := range location.Spec.NodeGroups {
		if slices.ContainsFunc(responseParams, func(icp IngressClassPayload) bool { return icp.IngressClassName == ng.Name }) {
			continue
		}
		responseParams = append(responseParams, IngressClassPayload{
			IngressClassName: ng.Name,
		})
	}

	return responseParams
}

func buildProbePayloads(location *infrastructurev1alpha1.Location) []ProbePayload {
	responseParams := []ProbePayload{}

	for _, ng := range location.Spec.NodeGroups {
		for _, node := range ng.Nodes {
			if node.Ipv4 != "" {
				responseParams = append(responseParams, ProbePayload{
					NodeGroupName: ng.Name,
					Flavor:        ng.Flavor,
					NodeName:      node.Name,
					Address:       node.Ipv4 + "/healthz",
				})
			}

			if node.Ipv6 != "" {
				responseParams = append(responseParams, ProbePayload{
					NodeGroupName: ng.Name,
					Flavor:        ng.Flavor,
					NodeName:      node.Name,
					Address:       node.Ipv6 + "/healthz",
				})
			}
		}
	}

	return responseParams
}

// getEnvOrDefault returns the value of an environment variable or a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBoolOrDefault returns the boolean value of an environment variable or a default value
func getEnvBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func loadKubernetesConfig(loadInCluster func() (*rest.Config, error), loadFromKubeconfig func() (*rest.Config, error)) (*rest.Config, error) {
	if os.Getenv(clientcmd.RecommendedConfigPathEnvVar) != "" {
		config, err := loadFromKubeconfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create Kubernetes config from %s: %w", clientcmd.RecommendedConfigPathEnvVar, err)
		}
		return config, nil
	}

	config, err := loadInCluster()
	if err == nil {
		return config, nil
	}

	config, err = loadFromKubeconfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	return config, nil
}

// createKubernetesClient creates a Kubernetes dynamic client
func createKubernetesClient() (dynamic.Interface, error) {
	scheme := kruntime.NewScheme()
	clientsetscheme.AddToScheme(scheme)
	infrastructurev1alpha1.AddToScheme(scheme)

	config, err := loadKubernetesConfig(rest.InClusterConfig, func() (*rest.Config, error) {
		if os.Getenv(clientcmd.RecommendedConfigPathEnvVar) != "" {
			return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				clientcmd.NewDefaultClientConfigLoadingRules(),
				&clientcmd.ConfigOverrides{},
			).ClientConfig()
		}

		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	})
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return client, nil
}

// getLocation reads a single Location CRD from Kubernetes
func getLocation(client dynamic.Interface, namespace string, name string, location *infrastructurev1alpha1.Location) error {
	ctx := context.Background()
	unstructuredObj, err := client.Resource(infrastructurev1alpha1.SchemeGroupVersion.WithResource("locations")).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	temp, err := json.Marshal(unstructuredObj.Object)
	if err != nil {
		return err
	}
	err = json.Unmarshal(temp, &location)
	return err
}

// newMux builds the API routes and returns an HTTP mux.
func newMux(token string, verbose bool, k8sClient dynamic.Interface) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/getparams.execute", func(w http.ResponseWriter, r *http.Request) {
		// Check method
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != token {
			if verbose {
				log.Printf("Authorization failed for request to %s", r.URL.Path)
			}
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if verbose {
			log.Printf("Authorized request to %s", r.URL.Path)
		}

		// Read request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		reqData := &RequestPayload{}
		if err := json.Unmarshal(bodyBytes, reqData); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		inputParams := reqData.Input.Parameters
		if inputParams == (ParameterTypes{}) {
			http.Error(w, "missing input.parameters", http.StatusBadRequest)
			return
		}

		log.Printf("Received request for Location: namespace=%s, name=%s", inputParams.Namespace, inputParams.Name)

		if k8sClient == nil {
			http.Error(w, "kubernetes apiserver unavailable", http.StatusServiceUnavailable)
			return
		}

		// Get Location from Kubernetes
		location := &infrastructurev1alpha1.Location{}
		locationErr := getLocation(k8sClient, inputParams.Namespace, inputParams.Name, location)
		if locationErr != nil {
			log.Printf("Warning: Failed to get Location: %v", locationErr)
			http.Error(w, "kubernetes apiserver unavailable", http.StatusServiceUnavailable)
			return
		} else if verbose {
			log.Printf("Successfully retrieved Location: %s", location.Name)
		}

		if reqData.Input.Parameters.Resource == "" {

			responseParams := buildDeploymentPayloads(location)

			o := &output.PluginOutput[[]DeploymentPayload]{
				Parameters: responseParams,
			}

			output := o.BuildPluginOutput()

			if verbose {
				log.Printf("Response output: %+v", output)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(output)
			return
		}

		if reqData.Input.Parameters.Resource == "IngressClass" {

			responseParams := buildIngressClassPayloads(location)

			o := &output.PluginOutput[[]IngressClassPayload]{
				Parameters: responseParams,
			}

			output := o.BuildPluginOutput()

			if verbose {
				log.Printf("Response output: %+v", output)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(output)
			return
		}

		if reqData.Input.Parameters.Resource == "Probe" {

			responseParams := buildProbePayloads(location)

			o := &output.PluginOutput[[]ProbePayload]{
				Parameters: responseParams,
			}

			output := o.BuildPluginOutput()

			if verbose {
				log.Printf("Response output: %+v", output)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(output)
			return
		}

		http.Error(w, "unsupported resource type", http.StatusBadRequest)
		return
	})

	return mux
}

func main() {
	// Define command-line flags with defaults from environment variables
	var (
		port    = flag.String("port", getEnvOrDefault("PORT", "8080"), "Port to run the server on (env: PORT)")
		token   = flag.String("token", getEnvOrDefault("TOKEN", "randtoken"), "Argo token (env: TOKEN)")
		verbose = flag.Bool("verbose", getEnvBoolOrDefault("VERBOSE", false), "Enable verbose logging (env: VERBOSE)")
	)
	flag.Parse()

	if *verbose {
		log.Println("Verbose logging enabled")
		log.Printf("Server configuration: port=%s, token=%s", *port, *token)
		log.Printf("Environment variables: PORT=%s, TOKEN=%s, VERBOSE=%s", os.Getenv("PORT"), os.Getenv("TOKEN"), os.Getenv("VERBOSE"))
	}

	// Create Kubernetes client
	k8sClient, err := createKubernetesClient()
	if err != nil {
		log.Printf("Warning: Failed to create Kubernetes client: %v", err)
		log.Println("Locations CRD data will not be available")
	} else if *verbose {
		log.Println("Kubernetes client created successfully")
	}

	mux := newMux(*token, *verbose, k8sClient)

	serverAddr := fmt.Sprintf(":%s", *port)
	log.Printf("Server running on %s", serverAddr)
	if *verbose {
		log.Printf("API endpoint available at /api/v1/getparams.execute")
	}
	log.Fatal(http.ListenAndServe(serverAddr, mux))
}
