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
	"strconv"
	"strings"

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

type ParameterTypes struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
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

// createKubernetesClient creates a Kubernetes dynamic client
func createKubernetesClient() (dynamic.Interface, error) {
	scheme := kruntime.NewScheme()
	clientsetscheme.AddToScheme(scheme)
	infrastructurev1alpha1.AddToScheme(scheme)

	var config *rest.Config
	var err error

	// Try in-cluster config first (when running in a pod)
	if config, err = rest.InClusterConfig(); err != nil {
		// Fall back to kubeconfig file
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		if config, err = clientcmd.BuildConfigFromFlags("", kubeconfig); err != nil {
			return nil, fmt.Errorf("failed to create Kubernetes config: %v", err)
		}
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return client, nil
}

// getLocation reads a single Location CRD from Kubernetes
func getLocation(client dynamic.Interface, namespace string, name string, location *infrastructurev1alpha1.Location) error {
	ctx := context.Background()
	unstructuredObj, err := client.Resource(infrastructurev1alpha1.GroupVersion.WithResource("locations")).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
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

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/getparams.execute", func(w http.ResponseWriter, r *http.Request) {
		// Check method
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != *token {
			if *verbose {
				log.Printf("Authorization failed for request to %s", r.URL.Path)
			}
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if *verbose {
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

		// Get Locations from Kubernetes if client is available
		location := &infrastructurev1alpha1.Location{}
		if k8sClient != nil {
			var err error
			err = getLocation(k8sClient, inputParams.Namespace, inputParams.Name, location)
			if err != nil {
				log.Printf("Warning: Failed to get Location: %v", err)
			} else if *verbose {
				log.Printf("Successfully retrieved Location: %s", location.Name)
			}
		}

		cacheConfigSpecs := []infrastructurev1alpha1.CacheConfigSpec{}

		for _, ng := range location.Spec.NodeGroups {
			cacheConfigSpecs = append(cacheConfigSpecs, ng.CacheConfig)
		}

		output := map[string]interface{}{
			"output": map[string]interface{}{
				"parameters": cacheConfigSpecs,
			},
		}

		if *verbose {
			log.Printf("Response output: %+v", output)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(output)
	})

	serverAddr := fmt.Sprintf(":%s", *port)
	log.Printf("Server running on %s", serverAddr)
	if *verbose {
		log.Printf("API endpoint available at /api/v1/getparams.execute")
	}
	log.Fatal(http.ListenAndServe(serverAddr, mux))
}
