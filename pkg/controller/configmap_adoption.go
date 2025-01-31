package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/authzed/controller-idioms/adopt"
	"github.com/authzed/controller-idioms/handler"
	"github.com/authzed/controller-idioms/typed"

	"github.com/authzed/spicedb-operator/pkg/metadata"
)

// EventConfigMapAdoptedBySpiceDBCluster is emitted when a ConfigMap is adopted by the SpiceDB operator
const EventConfigMapAdoptedBySpiceDBCluster = "ConfigMapAdoptedBySpiceDB"

// SchemaKey is the key for the SpiceDB policy schema definition in the ConfigMap
const SchemaKey = "schema"

// Example ConfigMap structure:
//
// apiVersion: v1
// kind: ConfigMap
// metadata:
//   name: spicedb-config
// data:
//   schema: |
//     definition user {}
//
//     definition document {
//         relation writer: user
//         relation reader: user
//
//         permission write = writer
//         permission read = reader + writer
//     }
//
//     definition folder {
//         relation parent: folder
//         relation owner: user
//
//         permission view = owner + owner from parent->view
//     }

// NewConfigMapAdoptionHandler creates a new handler for adopting SpiceDB configuration ConfigMaps.
// This handler manages ConfigMaps that contain SpiceDB configuration, including but not limited to:
// - Policy schemas
// - SpiceDB configuration options
// - Additional configuration that may be added in future
//
// When a SpiceDBCluster references a ConfigMap, this handler:
// 1. Adopts the ConfigMap by adding owner references and labels
// 2. Validates the configuration content
// 3. Makes the ConfigMap available to other handlers via context
func NewSchemaConfigMapAdoptionHandler(
	recorder record.EventRecorder,
	getFromCache func(ctx context.Context) (*corev1.ConfigMap, error),
	missingFunc func(ctx context.Context, err error),
	configMapIndexer *typed.Indexer[*corev1.ConfigMap],
	configMapApplyFunc adopt.ApplyFunc[*corev1.ConfigMap, *applycorev1.ConfigMapApplyConfiguration],
	existsFunc func(ctx context.Context, name types.NamespacedName) error,
	next handler.Handler,
) handler.Handler {
	return handler.NewHandler(&adopt.AdoptionHandler[*corev1.ConfigMap, *applycorev1.ConfigMapApplyConfiguration]{
		OperationsContext:      QueueOps,
		ControllerFieldManager: metadata.FieldManager,
		AdopteeCtx:             CtxSchemaConfigMapNN,
		OwnerCtx:               CtxClusterNN,
		AdoptedCtx:             CtxSchemaConfigMap,
		ObjectAdoptedFunc: func(ctx context.Context, configMap *corev1.ConfigMap) {
			recorder.Eventf(configMap, corev1.EventTypeNormal, EventConfigMapAdoptedBySpiceDBCluster,
				"ConfigMap was referenced as the configuration source for SpiceDBCluster %s; it has been labelled to mark it as part of the configuration for that controller.",
				CtxClusterNN.MustValue(ctx).String())
		},
		ObjectMissingFunc: missingFunc,
		GetFromCache:      getFromCache,
		Indexer:           configMapIndexer,
		IndexName:         metadata.OwningClusterIndex,
		Labels:            map[string]string{metadata.OperatorManagedLabelKey: metadata.OperatorManagedLabelValue},
		NewPatch: func(nn types.NamespacedName) *applycorev1.ConfigMapApplyConfiguration {
			return applycorev1.ConfigMap(nn.Name, nn.Namespace)
		},
		OwnerAnnotationPrefix: metadata.OwnerAnnotationKeyPrefix,
		OwnerAnnotationKeyFunc: func(owner types.NamespacedName) string {
			return metadata.OwnerAnnotationKeyPrefix + owner.Name
		},
		OwnerFieldManagerFunc: func(owner types.NamespacedName) string {
			return "spicedbcluster-owner-" + owner.Namespace + "-" + owner.Name
		},
		ApplyFunc:  configMapApplyFunc,
		ExistsFunc: existsFunc,
		Next:       next,
	}, "adoptConfigMap")
}
