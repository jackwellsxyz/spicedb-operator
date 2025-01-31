package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/authzed/controller-idioms/handler"
	"github.com/authzed/controller-idioms/queue/fake"
	"github.com/authzed/controller-idioms/typed"

	"github.com/authzed/spicedb-operator/pkg/metadata"
)

type applyCall struct {
	input  *applycorev1.ConfigMapApplyConfiguration
	result *corev1.ConfigMap
	err    error
	called bool
}

func TestSchemaConfigMapAdopterHandler(t *testing.T) {
	testConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "test",

			Labels: map[string]string{
				metadata.OperatorManagedLabelKey: metadata.OperatorManagedLabelValue,
			},
			Annotations: map[string]string{
				metadata.OwnerAnnotationKeyPrefix + "test": "owned",
			},
		},
		Data: map[string]string{
			"schema.yaml": "Valid test SpiceDB schema",
		},
	}

	tests := []struct {
		name                     string
		configMapName            string
		cluster                  types.NamespacedName
		configMapInCache         *corev1.ConfigMap
		cacheErr                 error
		configMapExistsErr       error
		configMapsInIndex        []*corev1.ConfigMap
		applyCalls               []*applyCall
		expectEvents             []string
		expectNext               bool
		expectRequeueErr         error
		expectRequeueAPIErr      error
		expectRequeue            bool
		expectObjectMissingErr   error
		expectCtxSchemaConfigMap *corev1.ConfigMap
	}{
		{
			name: "no configmap",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			configMapName: "",
			applyCalls:    []*applyCall{},
			expectNext:    true,
		},
		{
			name:          "configmap does not exist",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			cacheErr:               apierrors.NewNotFound(corev1.Resource("configmaps"), "config"),
			configMapExistsErr:     apierrors.NewNotFound(corev1.Resource("configmaps"), "config"),
			configMapsInIndex:      []*corev1.ConfigMap{},
			applyCalls:             []*applyCall{},
			expectObjectMissingErr: apierrors.NewNotFound(corev1.Resource("configmaps"), "config"),
		},
		{
			name:          "configmap needs adopting",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			cacheErr:          apierrors.NewNotFound(corev1.Resource("configmaps"), "config"),
			configMapsInIndex: []*corev1.ConfigMap{},
			applyCalls: []*applyCall{
				{
					input: applycorev1.ConfigMap("config", "test").
						WithLabels(map[string]string{
							metadata.OperatorManagedLabelKey: metadata.OperatorManagedLabelValue,
						}),
					result: &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "config",
							Namespace: "test",
							Labels:    nil,
						},
					},
				},
				{
					input: applycorev1.ConfigMap("config", "test").
						WithAnnotations(map[string]string{
							metadata.OwnerAnnotationKeyPrefix + "test": "owned",
						}),
					result: testConfigMap,
				},
			},
			expectEvents:             []string{"Normal ConfigMapAdoptedBySpiceDB ConfigMap was referenced as the configuration source for SpiceDBCluster test/test; it has been labelled to mark it as part of the configuration for that controller."},
			expectCtxSchemaConfigMap: testConfigMap,
			expectNext:               true,
		},
		{
			name:          "configmap already adopted",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			configMapInCache:         testConfigMap,
			configMapsInIndex:        []*corev1.ConfigMap{testConfigMap},
			applyCalls:               []*applyCall{},
			expectNext:               true,
			expectCtxSchemaConfigMap: testConfigMap,
		},
		{
			name:          "error during adoption",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			configMapInCache: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: "test",
				},
			},
			applyCalls: []*applyCall{
				{
					input: applycorev1.ConfigMap("config", "test").
						WithLabels(map[string]string{}).
						WithAnnotations(map[string]string{
							metadata.OwnerAnnotationKeyPrefix + "test": "owned",
						}),
					result: &corev1.ConfigMap{},
					err:    fmt.Errorf("apply error"),
				},
			},
			expectRequeueAPIErr: fmt.Errorf("apply error"),
		},
		{
			name:          "valid schema stored successfully",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			configMapInCache: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: "test",
				},
				Data: map[string]string{
					"schema": "definition user {}",
				},
			},
			applyCalls: []*applyCall{
				{
					input: applycorev1.ConfigMap("config", "test").
						WithAnnotations(map[string]string{
							metadata.OwnerAnnotationKeyPrefix + "test": "owned",
						}),
					result: testConfigMap,
				},
			},
			expectEvents:             []string{"Normal ConfigMapAdoptedBySpiceDB ConfigMap was referenced as the configuration source for SpiceDBCluster test/test; it has been labelled to mark it as part of the configuration for that controller."},
			expectCtxSchemaConfigMap: testConfigMap,
			expectNext:               true,
		},
		{
			name:          "configmap updated with new schema",
			configMapName: "config",
			cluster: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
			configMapInCache: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: "test",
					Labels:    nil,
				},
				Data: map[string]string{
					"schema": "definition user {} // updated",
				},
			},
			configMapsInIndex:        []*corev1.ConfigMap{testConfigMap}, // already adopted
			applyCalls:               []*applyCall{},                     // shouldn't need to re-adopt
			expectNext:               true,
			expectCtxSchemaConfigMap: testConfigMap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrls := &fake.FakeInterface{}
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{metadata.OwningClusterIndex: metadata.GetClusterKeyFromMeta})
			IndexAddUnstructured(t, indexer, tt.configMapsInIndex)

			recorder := record.NewFakeRecorder(1)
			nextCalled := false
			applyCallIndex := 0

			h := NewSchemaConfigMapAdoptionHandler(
				recorder,
				func(_ context.Context) (*corev1.ConfigMap, error) {
					return tt.configMapInCache, tt.cacheErr
				},
				func(_ context.Context, err error) {
					require.Equal(t, tt.expectObjectMissingErr, err)
				},
				typed.NewIndexer[*corev1.ConfigMap](indexer),
				func(_ context.Context, cm *applycorev1.ConfigMapApplyConfiguration, _ metav1.ApplyOptions) (*corev1.ConfigMap, error) {
					defer func() { applyCallIndex++ }()
					call := tt.applyCalls[applyCallIndex]
					call.called = true
					require.Equal(t, call.input, cm, "error on call %d", applyCallIndex)
					return call.result, call.err
				},
				func(_ context.Context, _ types.NamespacedName) error {
					return tt.configMapExistsErr
				},
				handler.NewHandlerFromFunc(func(ctx context.Context) {
					nextCalled = true
					require.Equal(t, tt.expectCtxSchemaConfigMap, CtxSchemaConfigMap.Value(ctx))
				}, "testnext"),
			)

			ctx := CtxClusterNN.WithValue(context.Background(), tt.cluster)
			ctx = CtxSchemaConfigMapNN.WithValue(ctx, types.NamespacedName{Namespace: "test", Name: tt.configMapName})
			ctx = QueueOps.WithValue(ctx, ctrls)
			h.Handle(ctx)
			for _, call := range tt.applyCalls {
				require.True(t, call.called)
			}
			require.Equal(t, len(tt.applyCalls), applyCallIndex, "not all expected apply calls were made")
			ExpectEvents(t, recorder, tt.expectEvents)
			require.Equal(t, tt.expectNext, nextCalled)
			if tt.expectRequeueErr != nil {
				require.Equal(t, 1, ctrls.RequeueErrCallCount())
				require.Equal(t, tt.expectRequeueErr, ctrls.RequeueErrArgsForCall(0))
			}
			if tt.expectRequeueAPIErr != nil {
				require.Equal(t, 1, ctrls.RequeueAPIErrCallCount())
				require.Equal(t, tt.expectRequeueAPIErr, ctrls.RequeueAPIErrArgsForCall(0))
			}
			require.Equal(t, tt.expectRequeue, ctrls.RequeueCallCount() == 1)
		})
	}
}
