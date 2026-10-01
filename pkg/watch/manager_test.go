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

package watch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

// noopLogger discards all output; the manager tests only assert on state.
func noopLogger() logr.Logger { return logr.Discard() }

// NewWatchManager keeps the identifier this suite used before the Manager moved
// into pkg/watch. NewManager is the real constructor; this is a thin test-only
// alias without the dynamiccontroller metrics wiring (these tests do not assert
// on metrics).
func NewWatchManager(client metadata.Interface, resync time.Duration, onEvent EventHandler, log logr.Logger) *Manager {
	return NewManager(client, resync, onEvent, log)
}

func newTestWatchManager(t *testing.T) *Manager {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleMetadataClient(scheme)
	return NewWatchManager(client, 1*time.Hour, func(Event) {}, noopLogger())
}

func TestReleaseWatch_StopsUnowned(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "test")
	assert.Equal(t, 1, wm.ActiveWatchCount())

	wm.ReleaseWatch(gvr, "test")
	assert.Equal(t, 0, wm.ActiveWatchCount())

	// Second release should not panic and count stays 0.
	wm.ReleaseWatch(gvr, "test")
	assert.Equal(t, 0, wm.ActiveWatchCount())
}

func TestReleaseWatch_ThenRetainWatch_CreatesFresh(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "test")
	inf1 := wm.GetInformer(gvr)
	assert.NotNil(t, inf1)

	wm.ReleaseWatch(gvr, "test")
	assert.Nil(t, wm.GetInformer(gvr))

	wm.EnsureWatch(gvr, "test")
	inf2 := wm.GetInformer(gvr)
	assert.NotNil(t, inf2)

	// Must be a new informer instance, not the old one.
	assert.NotSame(t, inf1, inf2, "expected fresh informer after ReleaseWatch + EnsureWatch")
}

func TestReleaseWatch_RetainedByOtherOwner(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "parent")
	assert.NotNil(t, wm.GetInformer(gvr))

	// Releasing an unrelated owner should not stop an owned watch.
	wm.ReleaseWatch(gvr, "other")
	assert.NotNil(t, wm.GetInformer(gvr), "owned informer should stay running")

	// Releasing the actual owner should stop it.
	wm.ReleaseWatch(gvr, "parent")
	assert.Nil(t, wm.GetInformer(gvr), "watch should stop after the owner releases it")
}

func TestRetainWatch_MultipleOwners(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "parent")
	wm.EnsureWatch(gvr, "coordinator")
	assert.Equal(t, 1, wm.ActiveWatchCount())

	// Release one owner — watch should stay.
	wm.ReleaseWatch(gvr, "parent")
	assert.NotNil(t, wm.GetInformer(gvr), "informer should stay running with one owner remaining")

	// Release second owner — now it stops automatically.
	wm.ReleaseWatch(gvr, "coordinator")
	assert.Nil(t, wm.GetInformer(gvr), "informer should stop after all owners release")
}

func TestDeleteFunc_Tombstone(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	var received []Event
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(runtime.NewScheme()),
		1*time.Hour,
		func(e Event) { received = append(received, e) },
		noopLogger(),
	)

	// Create a gvrWatch and get its event handler.
	w := wm.newWatch(gvr)

	// Simulate a tombstone (DeletedFinalStateUnknown wrapping a PartialObjectMetadata).
	obj := &v1.PartialObjectMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-deploy",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
	}
	tombstone := cache.DeletedFinalStateUnknown{
		Key: "default/my-deploy",
		Obj: obj,
	}

	handler := w.eventHandlerFuncs(func(e Event) { received = append(received, e) })
	handler.OnDelete(tombstone)

	assert.Equal(t, 1, len(received), "tombstone should be unwrapped and produce an event")
	assert.Equal(t, EventDelete, received[0].Type)
	assert.Equal(t, "my-deploy", received[0].Name)
	assert.Equal(t, "default", received[0].Namespace)
	assert.Equal(t, map[string]string{"app": "test"}, received[0].Labels)
}

func TestEnsureWatch_Idempotent(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "test")
	inf1 := wm.GetInformer(gvr)
	assert.NotNil(t, inf1)
	assert.Equal(t, 1, wm.ActiveWatchCount())

	// Second call is a no-op; same informer, same count.
	wm.EnsureWatch(gvr, "test")
	inf2 := wm.GetInformer(gvr)
	assert.Same(t, inf1, inf2)
	assert.Equal(t, 1, wm.ActiveWatchCount())
}

func TestShutdown(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr1 := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvr2 := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}

	wm.EnsureWatch(gvr1, "test")
	wm.EnsureWatch(gvr2, "test")
	assert.Equal(t, 2, wm.ActiveWatchCount())

	wm.Shutdown()
	assert.Equal(t, 0, wm.ActiveWatchCount())
	assert.Nil(t, wm.GetInformer(gvr1))
	assert.Nil(t, wm.GetInformer(gvr2))
}

func TestAddFunc(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	var received []Event
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(runtime.NewScheme()),
		1*time.Hour,
		func(e Event) { received = append(received, e) },
		noopLogger(),
	)

	w := wm.newWatch(gvr)
	handler := w.eventHandlerFuncs(func(e Event) { received = append(received, e) })

	obj := &v1.PartialObjectMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
		},
	}
	handler.OnAdd(obj, false)

	assert.Equal(t, 1, len(received))
	assert.Equal(t, EventAdd, received[0].Type)
	assert.Equal(t, "my-pod", received[0].Name)
	assert.Equal(t, "default", received[0].Namespace)
	assert.Equal(t, map[string]string{"app": "web"}, received[0].Labels)
}

func TestEventHandlerFuncs_NonMetaObject(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	var received []Event
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(runtime.NewScheme()),
		1*time.Hour,
		func(e Event) {},
		noopLogger(),
	)

	w := wm.newWatch(gvr)
	handler := w.eventHandlerFuncs(func(e Event) { received = append(received, e) })

	// Pass a non-meta object (plain string) — toEvent should return nil and
	// no event should be emitted.
	handler.OnAdd("not-a-meta-object", false)
	assert.Equal(t, 0, len(received))

	handler.OnUpdate("bad-old", "bad-new")
	assert.Equal(t, 0, len(received))

	handler.OnDelete("bad-obj")
	assert.Equal(t, 0, len(received))
}

func TestDeleteFunc_DirectObject(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	var received []Event
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(runtime.NewScheme()),
		1*time.Hour,
		func(e Event) {},
		noopLogger(),
	)

	w := wm.newWatch(gvr)
	handler := w.eventHandlerFuncs(func(e Event) { received = append(received, e) })

	// Direct delete (no tombstone wrapper).
	obj := &v1.PartialObjectMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name:      "direct-del",
			Namespace: "ns",
		},
	}
	handler.OnDelete(obj)

	assert.Equal(t, 1, len(received))
	assert.Equal(t, EventDelete, received[0].Type)
	assert.Equal(t, "direct-del", received[0].Name)
}

func TestNewWatch_WatchErrorHandler(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	failClient := fake.NewSimpleMetadataClient(scheme)
	failClient.PrependWatchReactor("*", func(action clienttesting.Action) (bool, watch.Interface, error) {
		// Return a valid watcher that immediately stops, which triggers
		// the watch error handler on the next retry.
		w := watch.NewFake()
		w.Stop()
		return true, w, nil
	})
	failClient.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated list error")
	})

	wm := NewWatchManager(failClient, 1*time.Hour, func(e Event) {}, noopLogger())
	wm.SyncTimeout = 500 * time.Millisecond
	wm.EnsureWatch(gvr, "test")

	// Give the informer goroutine time to hit the error handler.
	time.Sleep(200 * time.Millisecond)
}

func TestNewWatch_AddEventHandlerError(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)

	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(scheme),
		1*time.Hour,
		func(e Event) {},
		noopLogger(),
	)

	// Override createInformer to return an informer that's already stopped,
	// which causes AddEventHandler to return an error.
	wm.createInformer = func(gvr schema.GroupVersionResource) cache.SharedIndexInformer {
		// Create a real informer via the metadata client, start and stop it.
		inf := wm.defaultCreateInformer(gvr)
		stopCh := make(chan struct{})
		go inf.Run(stopCh)
		time.Sleep(50 * time.Millisecond)
		close(stopCh)
		// Wait for it to fully stop.
		time.Sleep(100 * time.Millisecond)
		return inf
	}

	// newWatch should log the error but not panic.
	w := wm.newWatch(gvr)
	assert.NotNil(t, w)
}

func TestUpdateFunc_OldLabels(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	var received []Event
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(runtime.NewScheme()),
		1*time.Hour,
		func(e Event) { received = append(received, e) },
		noopLogger(),
	)

	w := wm.newWatch(gvr)

	oldObj := &v1.PartialObjectMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-cm",
			Namespace: "default",
			Labels:    map[string]string{"team": "alpha"},
		},
	}
	newObj := &v1.PartialObjectMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-cm",
			Namespace: "default",
			Labels:    map[string]string{"team": "beta"},
		},
	}

	handler := w.eventHandlerFuncs(func(e Event) { received = append(received, e) })
	handler.OnUpdate(oldObj, newObj)

	assert.Equal(t, 1, len(received))
	assert.Equal(t, EventUpdate, received[0].Type)
	assert.Equal(t, map[string]string{"team": "beta"}, received[0].Labels)
	assert.Equal(t, map[string]string{"team": "alpha"}, received[0].OldLabels)
}

func TestEnsureWatch_NeverSyncs_RetainsInformer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	client := fake.NewSimpleMetadataClient(scheme)
	// Fail all list calls so the informer cannot sync.
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated list error")
	})

	var synced atomic.Int32
	wm := NewWatchManager(client, 1*time.Hour, func(e Event) {
		if e.Type == EventSynced {
			synced.Add(1)
		}
	}, noopLogger())
	wm.SyncTimeout = 200 * time.Millisecond

	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	// EnsureWatch never blocks on the initial list.
	start := time.Now()
	wm.EnsureWatch(gvr, "test")
	assert.Less(t, time.Since(start), wm.SyncTimeout, "EnsureWatch must return without waiting for sync")
	assert.Equal(t, 1, wm.ActiveWatchCount())

	// Only an explicit WaitForSync observes the timeout, and it leaves the
	// informer and the owner's retention in place: the reflector keeps
	// retrying in the background instead of the caller rebuilding it.
	err := wm.WaitForSync(context.Background(), gvr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache sync timeout")
	assert.Equal(t, 1, wm.ActiveWatchCount(), "a watch that cannot sync is retained, not torn down")
	assert.NotNil(t, wm.GetInformer(gvr))
	assert.Equal(t, int32(0), synced.Load(), "no EventSynced while the list keeps failing")

	// The owner is still retained; releasing it is what stops the informer.
	wm.ReleaseWatch(gvr, "test")
	assert.Equal(t, 0, wm.ActiveWatchCount())
}

func TestEnsureWatch_RecoversInPlace_NoRebuild(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	client := fake.NewSimpleMetadataClient(scheme)

	// Lists fail until failList is cleared (e.g. RBAC granted later).
	var failList atomic.Bool
	failList.Store(true)
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if failList.Load() {
			return true, nil, fmt.Errorf("simulated list error")
		}
		return false, nil, nil
	})

	var synced atomic.Int32
	wm := NewWatchManager(client, 1*time.Hour, func(e Event) {
		if e.Type == EventSynced {
			synced.Add(1)
		}
	}, noopLogger())
	wm.SyncTimeout = 200 * time.Millisecond

	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "test")
	first := wm.GetInformer(gvr)
	require.NotNil(t, first)
	require.Error(t, wm.WaitForSync(context.Background(), gvr))
	assert.Equal(t, 1, wm.ActiveWatchCount(), "informer retained across the failed sync")

	// Clear the fault: the SAME informer's reflector converges on its own.
	failList.Store(false)
	wm.SyncTimeout = 10 * time.Second
	require.NoError(t, wm.WaitForSync(context.Background(), gvr))
	assert.Same(t, first, wm.GetInformer(gvr), "recovery must not rebuild the informer")
	assert.Equal(t, 1, wm.ActiveWatchCount())
	assert.Eventually(t, func() bool { return synced.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"exactly one EventSynced once the initial list completes")
	wm.Shutdown()
}

func TestEnsureWatch_SyncSuccess(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	wm.SyncTimeout = 5 * time.Second

	wm.EnsureWatch(gvr, "test")
	assert.NoError(t, wm.WaitForSync(context.Background(), gvr))
	assert.Equal(t, 1, wm.ActiveWatchCount())
	wm.Shutdown()
}

func TestWaitForSync_NoWatch(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	err := wm.WaitForSync(context.Background(), gvr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no watch for")
}

func TestWaitForSync_ContextCanceled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	client := fake.NewSimpleMetadataClient(scheme)
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated list error")
	})
	wm := NewWatchManager(client, 1*time.Hour, func(Event) {}, noopLogger())
	wm.SyncTimeout = 10 * time.Second
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	wm.EnsureWatch(gvr, "test")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := wm.WaitForSync(ctx, gvr)
	// A caller cancellation surfaces as the context error, not as a timeout.
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, wm.ActiveWatchCount())
	wm.Shutdown()
}

func TestEventSynced_EmittedOncePerInformerStart(t *testing.T) {
	var events []Event
	var mu sync.Mutex
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(func() *runtime.Scheme {
			s := runtime.NewScheme()
			_ = v1.AddMetaToScheme(s)
			return s
		}()),
		1*time.Hour,
		func(e Event) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
		noopLogger(),
	)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	// Several owners, one informer: one EventSynced.
	wm.EnsureWatch(gvr, "a")
	wm.EnsureWatch(gvr, "b")
	wm.EnsureWatch(gvr, "a")
	require.NoError(t, wm.WaitForSync(context.Background(), gvr))

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, e := range events {
			if e.Type == EventSynced {
				n++
				assert.Equal(t, gvr, e.GVR)
				assert.Empty(t, e.Name)
			}
		}
		return n == 1
	}, 2*time.Second, 10*time.Millisecond)
	wm.Shutdown()
}

func TestEnsureWatch_ConcurrentCalls(t *testing.T) {
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	// Launch multiple concurrent EnsureWatch calls.
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wm.EnsureWatch(gvr, "test")
		}()
	}
	wg.Wait()

	// Only one informer should exist.
	assert.Equal(t, 1, wm.ActiveWatchCount())
	wm.Shutdown()
}

func TestConcurrentRetainWatch_ReleaseWatch(t *testing.T) {
	wm := newTestWatchManager(t)
	wm.SyncTimeout = 500 * time.Millisecond
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	// Retain and release concurrently.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			wm.EnsureWatch(gvr, "a")
			wm.ReleaseWatch(gvr, "a")
		}
	}()

	// Concurrent EnsureWatch calls.
	for range 20 {
		wm.EnsureWatch(gvr, "b")
		wm.ReleaseWatch(gvr, "b")
	}
	<-done

	// Should not panic. Final state: 0 watches (all owners released).
	assert.Equal(t, 0, wm.ActiveWatchCount())
}

func TestEnsureWatch_RaceCondition_ReleaseBeforeInformerCreated(t *testing.T) {
	// Regression test: EnsureWatch must hold the lock through both owner
	// registration and informer creation. Without this, a concurrent
	// ReleaseWatch between AddOwner and informer creation could remove the
	// owner, leaving a leaked watch with zero owners.
	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	client := fake.NewSimpleMetadataClient(scheme)

	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm := NewWatchManager(client, 1*time.Hour, func(Event) {}, noopLogger())

	// Use a slow createInformer so the lock is held longer, giving
	// ReleaseWatch time to block on the mutex.
	wm.createInformer = func(gvr schema.GroupVersionResource) cache.SharedIndexInformer {
		time.Sleep(50 * time.Millisecond)
		return wm.defaultCreateInformer(gvr)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		wm.EnsureWatch(gvr, "owner-a")
	}()

	// Give EnsureWatch time to acquire the lock, then release while it holds
	// the lock for informer creation. ReleaseWatch blocks until the watch
	// exists, then removes the owner and stops the informer.
	time.Sleep(10 * time.Millisecond)
	wm.ReleaseWatch(gvr, "owner-a")
	<-done

	// The key invariant: no leaked watch with zero owners.
	assert.Equal(t, 0, wm.ActiveWatchCount(), "watch should be stopped after sole owner released")
}

func TestEnsureWatch_AtomicOwnerAndWatch(t *testing.T) {
	// Verify that after EnsureWatch returns successfully, both the owner
	// and the watch exist — i.e., they were created atomically.
	wm := newTestWatchManager(t)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "owner-a")

	// Both owner and watch should exist.
	assert.Equal(t, 1, wm.ActiveWatchCount())
	assert.NotNil(t, wm.GetInformer(gvr))

	// ReleaseWatch should be able to clean up properly.
	wm.ReleaseWatch(gvr, "owner-a")
	assert.Equal(t, 0, wm.ActiveWatchCount())
	assert.Nil(t, wm.GetInformer(gvr))
}

func TestWaitForSync_Timeout_KeepsOwner(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddMetaToScheme(scheme)
	client := fake.NewSimpleMetadataClient(scheme)
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated list error")
	})

	wm := NewWatchManager(client, 1*time.Hour, func(Event) {}, noopLogger())
	wm.SyncTimeout = 200 * time.Millisecond

	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm.EnsureWatch(gvr, "owner-a")
	err := wm.WaitForSync(context.Background(), gvr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache sync timeout")

	// A timeout is purely informational: the owner's retention and the
	// informer are untouched. It is the caller's decision to release.
	assert.Equal(t, 1, wm.ActiveWatchCount())
	wm.ReleaseWatch(gvr, "owner-a")
	assert.Equal(t, 0, wm.ActiveWatchCount())
}

func TestSyncTimeout_DefaultValue(t *testing.T) {
	wm := newTestWatchManager(t)
	assert.Equal(t, defaultSyncTimeout, wm.syncTimeout())

	wm.SyncTimeout = 5 * time.Second
	assert.Equal(t, 5*time.Second, wm.syncTimeout())
}

func TestReleaseWatch_HandlerRemoved(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	var eventCount atomic.Int32
	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(func() *runtime.Scheme {
			s := runtime.NewScheme()
			_ = v1.AddMetaToScheme(s)
			return s
		}()),
		1*time.Hour,
		func(e Event) { eventCount.Add(1) },
		noopLogger(),
	)

	wm.EnsureWatch(gvr, "test")
	assert.NotNil(t, wm.GetInformer(gvr))

	// Release stops the watch and removes the handler.
	wm.ReleaseWatch(gvr, "test")
	assert.Nil(t, wm.GetInformer(gvr))
}

func TestShutdown_HandlerRemoved(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	wm := NewWatchManager(
		fake.NewSimpleMetadataClient(func() *runtime.Scheme {
			s := runtime.NewScheme()
			_ = v1.AddMetaToScheme(s)
			return s
		}()),
		1*time.Hour,
		func(e Event) {},
		noopLogger(),
	)

	wm.EnsureWatch(gvr, "test")
	wm.Shutdown()
	assert.Equal(t, 0, wm.ActiveWatchCount())
}
