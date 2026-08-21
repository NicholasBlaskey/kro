// Copyright 2026 The Kubernetes Authors.
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

package core_test

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
)

// This suite exercises the kro `time` CEL library (KREP-025) end-to-end through
// the real controller: the impure time.now(evaluateAfter) drives a self-scheduled
// requeue that flips an includeWhen with no external event, and the pure
// withTime/addDays helpers compute date boundaries.
var _ = Describe("Time CEL library", func() {
	var namespace string

	BeforeEach(func(ctx SpecContext) {
		namespace = fmt.Sprintf("test-%s", rand.String(5))
		Expect(env.Client.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(env.Client.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
	})

	It("creates a gated resource once the current time passes a deadline, driven by the evaluateAfter requeue", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-time-flip",
			generator.WithSchema(
				"TimeFlip", "v1alpha1",
				map[string]interface{}{
					"name":       "string",
					"activateAt": "string", // RFC3339 instant, set by the test
				},
				nil,
			),
			// Always created: proves the instance reconciles independent of the gate.
			generator.WithResource("always", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-always",
					"namespace": "${schema.metadata.namespace}",
				},
				"data": map[string]interface{}{"key": "always"},
			}, nil, nil),
			// Gated: only created once now >= activateAt. time.now(evaluateAfter)
			// schedules the reconcile at activateAt so the flip needs no external
			// event. The data fields exercise the pure withTime/addDays helpers.
			generator.WithResource("gated", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-gated",
					"namespace": "${schema.metadata.namespace}",
				},
				"data": map[string]interface{}{
					"activatedAt":     "${schema.spec.activateAt}",
					"nineUTC":         `${string(time.now(null).withTime({hours: 9}, "Etc/UTC"))}`,
					"tomorrowNineUTC": `${string(time.now(null).withTime({hours: 9}, "Etc/UTC").addDays(1, "Etc/UTC"))}`,
				},
			}, nil, []string{
				`${time.now(timestamp(schema.spec.activateAt)) >= timestamp(schema.spec.activateAt)}`,
			}),
		)

		Expect(env.Client.Create(ctx, rgd)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		})

		// RGD must compile (this proves the builder accepts time.* references).
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			g.Expect(env.Client.Get(ctx, types.NamespacedName{Name: rgd.Name}, createdRGD)).To(Succeed())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 30*time.Second, 250*time.Millisecond).WithContext(ctx).Should(Succeed())

		name := "flip-" + rand.String(4)
		activateAt := time.Now().UTC().Add(8 * time.Second)
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "TimeFlip",
				"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
				"spec": map[string]interface{}{
					"name":       name,
					"activateAt": activateAt.Format(time.RFC3339),
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { _ = env.Client.Delete(ctx, instance) })

		// "always" appears promptly.
		Eventually(func(g Gomega, ctx SpecContext) {
			g.Expect(env.Client.Get(ctx, types.NamespacedName{Name: name + "-always", Namespace: namespace}, &corev1.ConfigMap{})).To(Succeed())
		}, 20*time.Second, 250*time.Millisecond).WithContext(ctx).Should(Succeed())

		gatedKey := types.NamespacedName{Name: name + "-gated", Namespace: namespace}

		// Before the deadline the gate is false, so the resource must NOT exist.
		Consistently(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, gatedKey, &corev1.ConfigMap{})
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "gated must not exist before activateAt")
		}, 4*time.Second, 250*time.Millisecond).WithContext(ctx).Should(Succeed())

		// After the deadline, the scheduled requeue (from evaluateAfter) flips the
		// gate to true and the resource appears — with no external event.
		gated := &corev1.ConfigMap{}
		Eventually(func(g Gomega, ctx SpecContext) {
			g.Expect(env.Client.Get(ctx, gatedKey, gated)).To(Succeed())
		}, 30*time.Second, 500*time.Millisecond).WithContext(ctx).Should(Succeed())

		// Validate the pure helpers computed through the full pipeline.
		Expect(gated.Data["activatedAt"]).To(Equal(activateAt.Format(time.RFC3339)))

		nine, err := time.Parse(time.RFC3339, gated.Data["nineUTC"])
		Expect(err).NotTo(HaveOccurred())
		Expect(nine.UTC().Hour()).To(Equal(9))
		Expect(nine.UTC().Minute()).To(Equal(0))
		Expect(nine.UTC().Second()).To(Equal(0))
		Expect(nine.UTC().Format("2006-01-02")).To(Equal(time.Now().UTC().Format("2006-01-02")), "nineUTC should be today at 09:00Z")

		tomorrowNine, err := time.Parse(time.RFC3339, gated.Data["tomorrowNineUTC"])
		Expect(err).NotTo(HaveOccurred())
		Expect(tomorrowNine.Sub(nine)).To(Equal(24*time.Hour), "addDays(1) should advance exactly one day in UTC")
	})

	It("re-reconciles on its own schedule via time.now(evaluateAfter), with no external event", func(ctx SpecContext) {
		// Same mechanism as the minute-flipper example, sped up: each reconcile
		// stamps the current time and asks kro to reconcile ~1s later. Nothing
		// else touches the instance, so repeated updates prove the evaluateAfter
		// requeue loop drives reconciles by itself.
		rgd := generator.NewResourceGraphDefinition("test-time-beacon",
			generator.WithSchema(
				"TimeBeacon", "v1alpha1",
				map[string]interface{}{"name": "string"},
				nil,
			),
			generator.WithResource("beacon", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-beacon",
					"namespace": "${schema.metadata.namespace}",
				},
				"data": map[string]interface{}{
					"observedAt": `${string(time.now(time.now(null) + duration("1s")))}`,
				},
			}, nil, nil),
		)
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { Expect(env.Client.Delete(ctx, rgd)).To(Succeed()) })

		Eventually(func(g Gomega, ctx SpecContext) {
			created := &krov1alpha1.ResourceGraphDefinition{}
			g.Expect(env.Client.Get(ctx, types.NamespacedName{Name: rgd.Name}, created)).To(Succeed())
			g.Expect(created.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 30*time.Second, 250*time.Millisecond).WithContext(ctx).Should(Succeed())

		name := "beacon-" + rand.String(4)
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "TimeBeacon",
				"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
				"spec":       map[string]interface{}{"name": name},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { _ = env.Client.Delete(ctx, instance) })

		// Collect distinct observedAt values over ~10s. If the evaluateAfter
		// requeue works, kro reconciles repeatedly on its own and observedAt
		// advances several times without anyone touching the instance.
		beaconKey := types.NamespacedName{Name: name + "-beacon", Namespace: namespace}
		seen := map[string]struct{}{}
		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			g.Expect(env.Client.Get(ctx, beaconKey, cm)).To(Succeed())
			if v := cm.Data["observedAt"]; v != "" {
				seen[v] = struct{}{}
			}
			g.Expect(len(seen)).To(BeNumerically(">=", 3),
				"expected >=3 self-scheduled reconciles, saw %d distinct observedAt values", len(seen))
		}, 20*time.Second, 500*time.Millisecond).WithContext(ctx).Should(Succeed())
	})

	It("bumps a pod-template annotation once per interval (ticker/rollout pattern)", func(ctx SpecContext) {
		// KREP-025 example 2: roll a Deployment every N minutes (memory-leak
		// mitigation). A pod-template annotation carries a monotonic bucket
		// int(time.now(null))/interval; changing it re-rolls the pods. The nested
		// time.now(evaluateAfter) re-reconciles each interval. Uses a 2s interval
		// so the test is quick.
		rgd := generator.NewResourceGraphDefinition("test-time-ticker",
			generator.WithSchema(
				"TimeTicker", "v1alpha1",
				map[string]interface{}{"name": "string"},
				nil,
			),
			generator.WithResource("deployment", map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.metadata.namespace}",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{"app": "${schema.spec.name}"},
					},
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{"app": "${schema.spec.name}"},
							"annotations": map[string]interface{}{
								// monotonic 2s bucket; re-reconcile every 2s.
								"kro.run/restart-bucket": `${string(int(time.now(time.now(null) + duration("2s"))) / 2)}`,
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{"name": "app", "image": "nginx"},
							},
						},
					},
				},
			}, nil, nil),
		)
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { Expect(env.Client.Delete(ctx, rgd)).To(Succeed()) })

		Eventually(func(g Gomega, ctx SpecContext) {
			created := &krov1alpha1.ResourceGraphDefinition{}
			g.Expect(env.Client.Get(ctx, types.NamespacedName{Name: rgd.Name}, created)).To(Succeed())
			g.Expect(created.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 30*time.Second, 250*time.Millisecond).WithContext(ctx).Should(Succeed())

		name := "ticker-" + rand.String(4)
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "TimeTicker",
				"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
				"spec":       map[string]interface{}{"name": name},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { _ = env.Client.Delete(ctx, instance) })

		// The bucket annotation must advance monotonically as kro re-reconciles
		// on its own schedule — this is what re-rolls the pods each interval.
		depKey := types.NamespacedName{Name: name, Namespace: namespace}
		seen := map[int]struct{}{}
		last := -1
		Eventually(func(g Gomega, ctx SpecContext) {
			dep := &appsv1.Deployment{}
			g.Expect(env.Client.Get(ctx, depKey, dep)).To(Succeed())
			v := dep.Spec.Template.Annotations["kro.run/restart-bucket"]
			g.Expect(v).ToNot(BeEmpty())
			n, err := strconv.Atoi(v)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(n).To(BeNumerically(">=", last), "restart-bucket must be monotonic (was %d, now %d)", last, n)
			last = n
			seen[n] = struct{}{}
			g.Expect(len(seen)).To(BeNumerically(">=", 3),
				"expected the bucket to advance >=3 times, saw %d distinct values", len(seen))
		}, 20*time.Second, 500*time.Millisecond).WithContext(ctx).Should(Succeed())
	})
})
