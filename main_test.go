package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EdgeCDN-X/argocd-locations-plugin-generator/output"
	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
)

func newFakeK8sClient(t *testing.T) dynamic.Interface {
	t.Helper()

	scheme := kruntime.NewScheme()
	if err := clientsetscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

	location := &infrastructurev1alpha1.Location{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrastructurev1alpha1.SchemeGroupVersion.String(),
			Kind:       "Location",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fra1-c1",
			Namespace: "argocd",
		},
		Spec: infrastructurev1alpha1.LocationSpec{
			NodeGroups: []infrastructurev1alpha1.NodeGroupSpec{
				{
					Name:   "ssd",
					Flavor: "cache",
					CacheConfig: infrastructurev1alpha1.CacheConfigSpec{
						Path:     "/var/cache/ssd",
						KeysZone: "100m",
						Inactive: "10080m",
						MaxSize:  "4096m",
					},
					NodeSelector: map[string]string{"region": "fra1"},
				},
			},
		},
	}

	obj, err := kruntime.DefaultUnstructuredConverter.ToUnstructured(location)
	if err != nil {
		t.Fatalf("failed to convert location to unstructured: %v", err)
	}

	return dynamicfake.NewSimpleDynamicClient(scheme, &unstructured.Unstructured{Object: obj})
}

func newEmptyFakeK8sClient(t *testing.T) dynamic.Interface {
	t.Helper()

	scheme := kruntime.NewScheme()
	if err := clientsetscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

	return dynamicfake.NewSimpleDynamicClient(scheme)
}

func TestGetEnvOrDefault(t *testing.T) {
	const key = "TEST_ENV_OR_DEFAULT"
	_ = os.Unsetenv(key)

	if got := getEnvOrDefault(key, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}

	if err := os.Setenv(key, "from-env"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	if got := getEnvOrDefault(key, "fallback"); got != "from-env" {
		t.Fatalf("expected env value, got %q", got)
	}
}

func TestGetEnvBoolOrDefault(t *testing.T) {
	const key = "TEST_ENV_BOOL_OR_DEFAULT"

	t.Run("uses default when unset", func(t *testing.T) {
		_ = os.Unsetenv(key)
		if got := getEnvBoolOrDefault(key, true); got != true {
			t.Fatalf("expected default true, got %v", got)
		}
	})

	t.Run("parses valid bool", func(t *testing.T) {
		if err := os.Setenv(key, "false"); err != nil {
			t.Fatalf("failed to set env: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		if got := getEnvBoolOrDefault(key, true); got != false {
			t.Fatalf("expected parsed false, got %v", got)
		}
	})

	t.Run("uses default on invalid bool", func(t *testing.T) {
		if err := os.Setenv(key, "not-a-bool"); err != nil {
			t.Fatalf("failed to set env: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		if got := getEnvBoolOrDefault(key, true); got != true {
			t.Fatalf("expected default true for invalid bool, got %v", got)
		}
	})
}

func TestNewMux(t *testing.T) {
	mux := newMux("secret-token", false, nil)

	t.Run("rejects non-post method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/getparams.execute", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1"}}}`))
		req.Header.Set("Authorization", "Bearer wrong")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("rejects missing input parameters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("returns error when kubernetes client is unavailable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1"}}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}

		if !strings.Contains(rr.Body.String(), "kubernetes apiserver unavailable") {
			t.Fatalf("expected apiserver unavailable error, got %q", rr.Body.String())
		}
	})

	t.Run("returns error when location lookup fails", func(t *testing.T) {
		fakeClient := newEmptyFakeK8sClient(t)
		fakeMux := newMux("secret-token", false, fakeClient)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1"}}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		fakeMux.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}

		if !strings.Contains(rr.Body.String(), "kubernetes apiserver unavailable") {
			t.Fatalf("expected apiserver unavailable error, got %q", rr.Body.String())
		}
	})

	t.Run("reads location from mocked kube apiserver", func(t *testing.T) {
		fakeClient := newFakeK8sClient(t)
		fakeMux := newMux("secret-token", false, fakeClient)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1"}}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		fakeMux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}

		var got output.PluginOutput[[]DeploymentPayload]
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(got.Parameters) != 1 {
			t.Fatalf("expected 1 parameter, got %d", len(got.Parameters))
		}

		param := got.Parameters[0]
		if param.CacheName != "ssd" {
			t.Fatalf("expected cacheName ssd, got %q", param.CacheName)
		}
		if param.Flavor != "cache" {
			t.Fatalf("expected flavor cache, got %q", param.Flavor)
		}
		if param.Path != "/var/cache/ssd" {
			t.Fatalf("expected path /var/cache/ssd, got %q", param.Path)
		}
		if param.KeysZone != "100m" {
			t.Fatalf("expected keysZone 100m, got %q", param.KeysZone)
		}
		if param.Inactive != "10080m" {
			t.Fatalf("expected inactive 10080m, got %q", param.Inactive)
		}
		if param.MaxSize != "4096m" {
			t.Fatalf("expected maxSize 4096m, got %q", param.MaxSize)
		}
		if gotRegion := param.NodeSelector["region"]; gotRegion != "fra1" {
			t.Fatalf("expected nodeSelector.region fra1, got %q", gotRegion)
		}
	})

	t.Run("returns ingress classes when resource is IngressClass", func(t *testing.T) {
		fakeClient := newFakeK8sClient(t)
		fakeMux := newMux("secret-token", false, fakeClient)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1","resource":"IngressClass"}}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		fakeMux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}

		var got output.PluginOutput[[]IngressClassPayload]
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(got.Parameters) != 1 {
			t.Fatalf("expected 1 ingress class, got %d", len(got.Parameters))
		}

		if got.Parameters[0].IngressClassName != "ssd" {
			t.Fatalf("expected ingressClassName ssd, got %q", got.Parameters[0].IngressClassName)
		}
	})

	t.Run("returns bad request for unsupported resource type", func(t *testing.T) {
		fakeClient := newFakeK8sClient(t)
		fakeMux := newMux("secret-token", false, fakeClient)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/getparams.execute", strings.NewReader(`{"input":{"parameters":{"namespace":"argocd","name":"fra1-c1","resource":"Service"}}}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()

		fakeMux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}

		if !strings.Contains(rr.Body.String(), "unsupported resource type") {
			t.Fatalf("expected unsupported resource type error, got %q", rr.Body.String())
		}
	})
}
