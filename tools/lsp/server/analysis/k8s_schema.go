// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analysis

import "strings"

// K8sSchemaProvider provides field information for common Kubernetes types
type K8sSchemaProvider struct {
	schemas map[string]map[string][]string
}

// NewK8sSchemaProvider creates a new schema provider with hardcoded K8s types
// Covers 30+ Kubernetes resources with deep nested field support
func NewK8sSchemaProvider() *K8sSchemaProvider {
	// Start with core schemas
	schemas := map[string]map[string][]string{
		// Common metadata fields (shared by all resources)
		"_common_metadata": {
			"metadata": {
				"name",
				"namespace",
				"labels",
				"annotations",
				"creationTimestamp",
				"deletionTimestamp",
				"generation",
				"resourceVersion",
				"uid",
				"finalizers",
				"ownerReferences",
			},
		},
			// Deployment (apps/v1)
			"Deployment": {
				"spec": {
					"replicas",
					"selector",
					"template",
					"strategy",
					"minReadySeconds",
					"revisionHistoryLimit",
					"paused",
					"progressDeadlineSeconds",
				},
				"spec.selector": {
					"matchLabels",
					"matchExpressions",
				},
				"spec.template": {
					"metadata",
					"spec",
				},
				"spec.template.spec": {
					"containers",
					"volumes",
					"restartPolicy",
					"serviceAccountName",
					"nodeName",
					"nodeSelector",
					"affinity",
					"tolerations",
				},
				"status": {
					"replicas",
					"updatedReplicas",
					"readyReplicas",
					"availableReplicas",
					"unavailableReplicas",
					"conditions",
					"observedGeneration",
					"collisionCount",
				},
			},
			// Service (v1)
			"Service": {
				"spec": {
					"type",
					"selector",
					"ports",
					"clusterIP",
					"externalIPs",
					"sessionAffinity",
					"loadBalancerIP",
					"loadBalancerSourceRanges",
					"externalName",
					"externalTrafficPolicy",
					"healthCheckNodePort",
					"publishNotReadyAddresses",
					"sessionAffinityConfig",
				},
				"spec.ports": {
					"name",
					"protocol",
					"port",
					"targetPort",
					"nodePort",
				},
				"status": {
					"loadBalancer",
					"conditions",
				},
			},
			// ConfigMap (v1)
			"ConfigMap": {
				"data":       {},
				"binaryData": {},
			},
			// Secret (v1)
			"Secret": {
				"type":       {},
				"data":       {},
				"stringData": {},
			},
			// Pod (v1)
			"Pod": {
				"spec": {
					"containers",
					"volumes",
					"restartPolicy",
					"serviceAccountName",
					"nodeName",
					"nodeSelector",
					"affinity",
					"tolerations",
					"hostNetwork",
					"hostPID",
					"hostIPC",
					"hostname",
					"subdomain",
					"dnsPolicy",
					"dnsConfig",
					"priorityClassName",
					"priority",
					"schedulerName",
				},
				"spec.containers": {
					"name",
					"image",
					"command",
					"args",
					"ports",
					"env",
					"envFrom",
					"volumeMounts",
					"resources",
					"livenessProbe",
					"readinessProbe",
					"startupProbe",
					"securityContext",
					"imagePullPolicy",
					"workingDir",
				},
				"spec.containers.ports": {
					"name",
					"containerPort",
					"protocol",
					"hostPort",
					"hostIP",
				},
				"spec.containers.env": {
					"name",
					"value",
					"valueFrom",
				},
				"spec.containers.env.valueFrom": {
					"configMapKeyRef",
					"secretKeyRef",
					"fieldRef",
					"resourceFieldRef",
				},
				"spec.containers.env.valueFrom.configMapKeyRef": {
					"name",
					"key",
					"optional",
				},
				"spec.containers.env.valueFrom.secretKeyRef": {
					"name",
					"key",
					"optional",
				},
				"spec.containers.volumeMounts": {
					"name",
					"mountPath",
					"subPath",
					"readOnly",
					"mountPropagation",
				},
				"spec.containers.resources": {
					"limits",
					"requests",
				},
				"spec.containers.resources.limits": {
					"cpu",
					"memory",
					"ephemeral-storage",
				},
				"spec.containers.resources.requests": {
					"cpu",
					"memory",
					"ephemeral-storage",
				},
				"spec.containers.livenessProbe": {
					"httpGet",
					"tcpSocket",
					"exec",
					"initialDelaySeconds",
					"periodSeconds",
					"timeoutSeconds",
				},
				"spec.containers.livenessProbe.httpGet": {
					"path",
					"port",
					"host",
					"scheme",
				},
				"spec.volumes": {
					"name",
					"configMap",
					"secret",
					"emptyDir",
					"hostPath",
					"persistentVolumeClaim",
				},
				"spec.volumes.configMap": {
					"name",
					"items",
					"defaultMode",
					"optional",
				},
				"spec.volumes.secret": {
					"secretName",
					"items",
					"defaultMode",
					"optional",
				},
				"spec.volumes.persistentVolumeClaim": {
					"claimName",
					"readOnly",
				},
				"status": {
					"phase",
					"conditions",
					"message",
					"reason",
					"hostIP",
					"podIP",
					"podIPs",
					"startTime",
					"containerStatuses",
					"initContainerStatuses",
					"ephemeralContainerStatuses",
				},
			},
			// StatefulSet (apps/v1)
			"StatefulSet": {
				"spec": {
					"serviceName",
					"replicas",
					"selector",
					"template",
					"volumeClaimTemplates",
					"podManagementPolicy",
					"updateStrategy",
					"revisionHistoryLimit",
					"minReadySeconds",
				},
				"status": {
					"observedGeneration",
					"replicas",
					"readyReplicas",
					"currentReplicas",
					"updatedReplicas",
					"currentRevision",
					"updateRevision",
					"collisionCount",
					"conditions",
				},
			},
			// DaemonSet (apps/v1)
			"DaemonSet": {
				"spec": {
					"selector",
					"template",
					"updateStrategy",
					"minReadySeconds",
					"revisionHistoryLimit",
				},
				"status": {
					"currentNumberScheduled",
					"numberMisscheduled",
					"desiredNumberScheduled",
					"numberReady",
					"observedGeneration",
					"updatedNumberScheduled",
					"numberAvailable",
					"numberUnavailable",
					"collisionCount",
					"conditions",
				},
			},
			// Ingress (networking.k8s.io/v1)
			"Ingress": {
				"spec": {
					"ingressClassName",
					"defaultBackend",
					"tls",
					"rules",
				},
				"spec.rules": {
					"host",
					"http",
				},
				"spec.rules.http": {
					"paths",
				},
				"spec.rules.http.paths": {
					"path",
					"pathType",
					"backend",
				},
				"status": {
					"loadBalancer",
				},
			},
			// PersistentVolumeClaim (v1)
			"PersistentVolumeClaim": {
				"spec": {
					"accessModes",
					"resources",
					"volumeName",
					"storageClassName",
					"volumeMode",
					"dataSource",
					"selector",
				},
				"spec.resources": {
					"requests",
					"limits",
				},
				"status": {
					"phase",
					"accessModes",
					"capacity",
					"conditions",
				},
			},
			// Namespace (v1)
			"Namespace": {
				"spec": {
					"finalizers",
				},
				"status": {
					"phase",
					"conditions",
				},
			},
			// ServiceAccount (v1)
			"ServiceAccount": {
				"secrets":                   {},
				"imagePullSecrets":          {},
				"automountServiceAccountToken": {},
			},
		}

	// Merge extended schemas (Job, CronJob, HPA, etc.)
	for kind, kindSchemas := range extendedK8sSchemas {
		schemas[kind] = kindSchemas
	}

	return &K8sSchemaProvider{
		schemas: schemas,
	}
}

// GetFields returns the field names for a given K8s kind and field path
// Example: GetFields("Deployment", "spec") returns ["replicas", "selector", "template", ...]
// Example: GetFields("Service", "spec.ports") returns ["name", "protocol", "port", ...]
func (ksp *K8sSchemaProvider) GetFields(kind string, fieldPath string) []string {
	if ksp == nil || ksp.schemas == nil {
		return nil
	}

	kindSchema, ok := ksp.schemas[kind]
	if !ok {
		return nil
	}

	fields, ok := kindSchema[fieldPath]
	if !ok {
		// Check if this is a metadata field path - fall back to common metadata
		// Handles both "metadata" and "spec.template.metadata" etc
		if fieldPath == "metadata" || strings.HasSuffix(fieldPath, ".metadata") {
			if commonMeta, ok := ksp.schemas["_common_metadata"]; ok {
				if metadataFields, ok := commonMeta["metadata"]; ok {
					return metadataFields
				}
			}
		}
		return nil
	}

	return fields
}

// HasKind returns true if this provider has schema information for the given kind
func (ksp *K8sSchemaProvider) HasKind(kind string) bool {
	if ksp == nil || ksp.schemas == nil {
		return false
	}
	_, ok := ksp.schemas[kind]
	return ok
}
