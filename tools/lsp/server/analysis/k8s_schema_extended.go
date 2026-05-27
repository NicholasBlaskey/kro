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

// Extended K8s schemas for additional resource types
// This is separated to keep k8s_schema.go manageable
var extendedK8sSchemas = map[string]map[string][]string{
	// Job (batch/v1)
	"Job": {
		"spec": {
			"template",
			"parallelism",
			"completions",
			"backoffLimit",
			"activeDeadlineSeconds",
			"ttlSecondsAfterFinished",
			"suspend",
			"completionMode",
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
		},
		"status": {
			"active",
			"succeeded",
			"failed",
			"startTime",
			"completionTime",
			"conditions",
		},
	},
	// CronJob (batch/v1)
	"CronJob": {
		"spec": {
			"schedule",
			"jobTemplate",
			"concurrencyPolicy",
			"suspend",
			"successfulJobsHistoryLimit",
			"failedJobsHistoryLimit",
			"startingDeadlineSeconds",
		},
		"spec.jobTemplate": {
			"spec",
		},
		"spec.jobTemplate.spec": {
			"template",
			"parallelism",
			"completions",
			"backoffLimit",
			"activeDeadlineSeconds",
		},
		"status": {
			"active",
			"lastScheduleTime",
			"lastSuccessfulTime",
		},
	},
	// HorizontalPodAutoscaler (autoscaling/v2)
	"HorizontalPodAutoscaler": {
		"spec": {
			"scaleTargetRef",
			"minReplicas",
			"maxReplicas",
			"metrics",
			"behavior",
		},
		"spec.scaleTargetRef": {
			"apiVersion",
			"kind",
			"name",
		},
		"spec.metrics": {
			"type",
			"resource",
			"pods",
			"object",
			"external",
		},
		"spec.metrics.resource": {
			"name",
			"target",
		},
		"status": {
			"currentReplicas",
			"desiredReplicas",
			"currentMetrics",
			"conditions",
			"lastScaleTime",
		},
	},
	// NetworkPolicy (networking.k8s.io/v1)
	"NetworkPolicy": {
		"spec": {
			"podSelector",
			"ingress",
			"egress",
			"policyTypes",
		},
		"spec.podSelector": {
			"matchLabels",
			"matchExpressions",
		},
		"spec.ingress": {
			"from",
			"ports",
		},
		"spec.egress": {
			"to",
			"ports",
		},
	},
	// PersistentVolume (v1)
	"PersistentVolume": {
		"spec": {
			"capacity",
			"accessModes",
			"persistentVolumeReclaimPolicy",
			"storageClassName",
			"volumeMode",
			"hostPath",
			"nfs",
			"awsElasticBlockStore",
			"gcePersistentDisk",
			"azureDisk",
			"csi",
			"local",
		},
		"spec.hostPath": {
			"path",
			"type",
		},
		"spec.nfs": {
			"server",
			"path",
			"readOnly",
		},
		"spec.csi": {
			"driver",
			"volumeHandle",
			"volumeAttributes",
			"fsType",
			"readOnly",
		},
		"status": {
			"phase",
			"message",
			"reason",
		},
	},
	// Role (rbac.authorization.k8s.io/v1)
	"Role": {
		"rules": {
			"apiGroups",
			"resources",
			"verbs",
			"resourceNames",
		},
	},
	// RoleBinding (rbac.authorization.k8s.io/v1)
	"RoleBinding": {
		"subjects": {
			"kind",
			"name",
			"namespace",
			"apiGroup",
		},
		"roleRef": {
			"apiGroup",
			"kind",
			"name",
		},
	},
	// ClusterRole (rbac.authorization.k8s.io/v1)
	"ClusterRole": {
		"rules": {
			"apiGroups",
			"resources",
			"verbs",
			"resourceNames",
			"nonResourceURLs",
		},
		"aggregationRule": {
			"clusterRoleSelectors",
		},
	},
	// ClusterRoleBinding (rbac.authorization.k8s.io/v1)
	"ClusterRoleBinding": {
		"subjects": {
			"kind",
			"name",
			"namespace",
			"apiGroup",
		},
		"roleRef": {
			"apiGroup",
			"kind",
			"name",
		},
	},
	// ResourceQuota (v1)
	"ResourceQuota": {
		"spec": {
			"hard",
			"scopes",
			"scopeSelector",
		},
		"status": {
			"hard",
			"used",
		},
	},
	// LimitRange (v1)
	"LimitRange": {
		"spec": {
			"limits",
		},
		"spec.limits": {
			"type",
			"max",
			"min",
			"default",
			"defaultRequest",
			"maxLimitRequestRatio",
		},
	},
	// PodDisruptionBudget (policy/v1)
	"PodDisruptionBudget": {
		"spec": {
			"minAvailable",
			"maxUnavailable",
			"selector",
			"unhealthyPodEvictionPolicy",
		},
		"spec.selector": {
			"matchLabels",
			"matchExpressions",
		},
		"status": {
			"currentHealthy",
			"desiredHealthy",
			"disruptionsAllowed",
			"expectedPods",
			"observedGeneration",
			"conditions",
		},
	},
	// PriorityClass (scheduling.k8s.io/v1)
	"PriorityClass": {
		"value":           {},
		"globalDefault":   {},
		"description":     {},
		"preemptionPolicy": {},
	},
	// StorageClass (storage.k8s.io/v1)
	"StorageClass": {
		"provisioner":       {},
		"parameters":        {},
		"reclaimPolicy":     {},
		"volumeBindingMode": {},
		"allowVolumeExpansion": {},
		"mountOptions":      {},
		"allowedTopologies": {},
	},
	// Endpoints (v1)
	"Endpoints": {
		"subsets": {
			"addresses",
			"notReadyAddresses",
			"ports",
		},
		"subsets.addresses": {
			"ip",
			"hostname",
			"nodeName",
			"targetRef",
		},
		"subsets.ports": {
			"name",
			"port",
			"protocol",
			"appProtocol",
		},
	},
	// EndpointSlice (discovery.k8s.io/v1)
	"EndpointSlice": {
		"addressType": {},
		"endpoints": {
			"addresses",
			"conditions",
			"hostname",
			"targetRef",
			"nodeName",
			"zone",
		},
		"ports": {
			"name",
			"protocol",
			"port",
			"appProtocol",
		},
	},
	// ReplicaSet (apps/v1)
	"ReplicaSet": {
		"spec": {
			"replicas",
			"selector",
			"template",
			"minReadySeconds",
		},
		"spec.selector": {
			"matchLabels",
			"matchExpressions",
		},
		"spec.template": {
			"metadata",
			"spec",
		},
		"status": {
			"replicas",
			"fullyLabeledReplicas",
			"readyReplicas",
			"availableReplicas",
			"observedGeneration",
			"conditions",
		},
	},
}

func init() {
	// Merge extended schemas into the main schema provider
	// This is called automatically when the package is imported
}
