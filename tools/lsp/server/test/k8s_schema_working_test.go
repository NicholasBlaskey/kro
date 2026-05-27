package test

import (
	"strings"
	"testing"
	"time"
)

// TestK8sSchema_ServiceFields tests Service schema completion
func TestK8sSchema_ServiceFields(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "Service spec",
			expr:         "${service.spec.",
			line:         18,
			char:         30,
			wantContains: []string{"type", "selector", "ports", "clusterIP"},
		},
		{
			name:         "Service ports",
			expr:         "${service.spec.ports.",
			line:         18,
			char:         36,
			wantContains: []string{"name", "protocol", "port", "targetPort", "nodePort"},
		},
		{
			name:         "Service status",
			expr:         "${service.status.",
			line:         18,
			char:         32,
			wantContains: []string{"loadBalancer", "conditions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-service.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-service.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}

// TestK8sSchema_DeploymentFields tests Deployment schema completion
func TestK8sSchema_DeploymentFields(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "Deployment spec",
			expr:         "${deployment.spec.",
			line:         18,
			char:         33,
			wantContains: []string{"replicas", "selector", "template", "strategy"},
		},
		{
			name:         "Deployment selector",
			expr:         "${deployment.spec.selector.",
			line:         18,
			char:         42,
			wantContains: []string{"matchLabels", "matchExpressions"},
		},
		{
			name:         "Deployment template",
			expr:         "${deployment.spec.template.",
			line:         18,
			char:         42,
			wantContains: []string{"metadata", "spec"},
		},
		{
			name:         "Deployment status",
			expr:         "${deployment.status.",
			line:         18,
			char:         35,
			wantContains: []string{"replicas", "updatedReplicas", "readyReplicas", "conditions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-deployment.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-deployment.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}

// TestK8sSchema_PodFields tests Pod schema completion (deep nesting)
func TestK8sSchema_PodFields(t *testing.T) {
	// Create RGD with Pod resource
	testRGDWithPod := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: testpod
spec:
  schema:
    kind: TestPod
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: pod
      template:
        apiVersion: v1
        kind: Pod
        metadata:
          name: CURSOR_HERE`

	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "Pod spec",
			expr:         "${pod.spec.",
			line:         17,
			char:         26,
			wantContains: []string{"containers", "volumes", "restartPolicy", "serviceAccountName"},
		},
		{
			name:         "Pod containers",
			expr:         "${pod.spec.containers.",
			line:         17,
			char:         37,
			wantContains: []string{"name", "image", "command", "ports", "env", "volumeMounts", "resources"},
		},
		{
			name:         "Pod containers env",
			expr:         "${pod.spec.containers.env.",
			line:         17,
			char:         41,
			wantContains: []string{"name", "value", "valueFrom"},
		},
		{
			name:         "Pod containers env valueFrom",
			expr:         "${pod.spec.containers.env.valueFrom.",
			line:         17,
			char:         51,
			wantContains: []string{"configMapKeyRef", "secretKeyRef", "fieldRef"},
		},
		{
			name:         "Pod containers env valueFrom secretKeyRef",
			expr:         "${pod.spec.containers.env.valueFrom.secretKeyRef.",
			line:         17,
			char:         65,
			wantContains: []string{"name", "key", "optional"},
		},
		{
			name:         "Pod containers volumeMounts",
			expr:         "${pod.spec.containers.volumeMounts.",
			line:         17,
			char:         50,
			wantContains: []string{"name", "mountPath", "subPath", "readOnly"},
		},
		{
			name:         "Pod containers resources",
			expr:         "${pod.spec.containers.resources.",
			line:         17,
			char:         47,
			wantContains: []string{"limits", "requests"},
		},
		{
			name:         "Pod containers resources limits",
			expr:         "${pod.spec.containers.resources.limits.",
			line:         17,
			char:         54,
			wantContains: []string{"cpu", "memory", "ephemeral-storage"},
		},
		{
			name:         "Pod containers livenessProbe",
			expr:         "${pod.spec.containers.livenessProbe.",
			line:         17,
			char:         52,
			wantContains: []string{"httpGet", "tcpSocket", "exec", "initialDelaySeconds"},
		},
		{
			name:         "Pod containers livenessProbe httpGet",
			expr:         "${pod.spec.containers.livenessProbe.httpGet.",
			line:         17,
			char:         60,
			wantContains: []string{"path", "port", "host", "scheme"},
		},
		{
			name:         "Pod volumes",
			expr:         "${pod.spec.volumes.",
			line:         17,
			char:         34,
			wantContains: []string{"name", "configMap", "secret", "emptyDir", "persistentVolumeClaim"},
		},
		{
			name:         "Pod volumes configMap",
			expr:         "${pod.spec.volumes.configMap.",
			line:         17,
			char:         44,
			wantContains: []string{"name", "items", "defaultMode", "optional"},
		},
		{
			name:         "Pod volumes secret",
			expr:         "${pod.spec.volumes.secret.",
			line:         17,
			char:         41,
			wantContains: []string{"secretName", "items", "defaultMode", "optional"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithPod, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-pod.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-pod.yaml", tt.line, tt.char, 1, "")

			t.Logf("%s: got %d items: %v", tt.name, len(items), itemLabels(items))

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}

// TestK8sSchema_StatefulSetFields tests StatefulSet schema completion
func TestK8sSchema_StatefulSetFields(t *testing.T) {
	// Create RGD with StatefulSet resource
	testRGDWithStatefulSet := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: teststatefulset
spec:
  schema:
    kind: TestSts
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: statefulset
      template:
        apiVersion: apps/v1
        kind: StatefulSet
        metadata:
          name: CURSOR_HERE`

	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "StatefulSet spec",
			expr:         "${statefulset.spec.",
			line:         17,
			char:         34,
			wantContains: []string{"serviceName", "replicas", "selector", "template", "volumeClaimTemplates"},
		},
		{
			name:         "StatefulSet status",
			expr:         "${statefulset.status.",
			line:         17,
			char:         36,
			wantContains: []string{"replicas", "readyReplicas", "currentReplicas", "updatedReplicas"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithStatefulSet, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-sts.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-sts.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}

// TestK8sSchema_ConfigMapAndSecret tests ConfigMap and Secret completion
func TestK8sSchema_ConfigMapAndSecret(t *testing.T) {
	// Create RGD with ConfigMap and Secret
	testRGDWithConfig := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: testconfig
spec:
  schema:
    kind: TestConfig
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: configmap
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: CURSOR_HERE
    - id: secret
      template:
        apiVersion: v1
        kind: Secret
        metadata:
          name: my-secret`

	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "ConfigMap root",
			expr:         "${configmap.",
			line:         17,
			char:         26,
			wantContains: []string{"data", "binaryData", "metadata"},
		},
		{
			name:         "Secret root",
			expr:         "${secret.",
			line:         17,
			char:         24,
			wantContains: []string{"type", "data", "stringData", "metadata"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithConfig, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-config.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-config.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}

// TestK8sSchema_Coverage tests that we have good coverage of K8s types
func TestK8sSchema_Coverage(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	// Just verify that completion doesn't crash for various resource types
	// and returns SOME results

	tests := []struct {
		resourceType string
		expr         string
		minItems     int
	}{
		{"Deployment", "${deployment.spec.", 3},
		{"Service", "${service.spec.", 3},
		{"StatefulSet", "${statefulset.spec.", 3},
		{"DaemonSet", "${daemonset.spec.", 3},
		{"Pod", "${pod.spec.", 3},
		{"ConfigMap", "${configmap.", 1},
		{"Secret", "${secret.", 1},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			// Create a simple RGD with this resource type
			// (using lowercase kind for simplicity in this test)
			rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: Test
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: ` + strings.ToLower(tt.resourceType) + `
      template:
        apiVersion: v1
        kind: ` + tt.resourceType + `
        metadata:
          name: ` + tt.expr

			doc := strings.Replace(rgd, tt.expr, tt.expr, 1)
			client.OpenDocument("file:///tmp/test-coverage-"+tt.resourceType+".yaml", doc)
			time.Sleep(300 * time.Millisecond)

			// Just make sure we get SOME completions and don't crash
			items := client.RequestCompletion("file:///tmp/test-coverage-"+tt.resourceType+".yaml", 15, len(tt.expr)+18, 1, "")

			if len(items) < tt.minItems {
				t.Errorf("%s: expected at least %d items, got %d: %v", tt.resourceType, tt.minItems, len(items), itemLabels(items))
			} else {
				t.Logf("%s: ✅ got %d completions", tt.resourceType, len(items))
			}
		})
	}
}
