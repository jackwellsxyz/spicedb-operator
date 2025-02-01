package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/authzed/controller-idioms/queue/fake"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/controller-idioms/handler"
	"github.com/authzed/spicedb-operator/pkg/apis/authzed/v1alpha1"
	"github.com/authzed/spicedb-operator/pkg/config"
	"github.com/stretchr/testify/require"
)

type mockSchemaClient struct {
	writeSchemaErr error
}

func (m *mockSchemaClient) WriteSchema(context.Context, *v1.WriteSchemaRequest) (*v1.WriteSchemaResponse, error) {
	if m.writeSchemaErr != nil {
		return nil, m.writeSchemaErr
	}
	return &v1.WriteSchemaResponse{}, nil
}

func TestSchemaApplyHandler(t *testing.T) {
	tests := []struct {
		name               string
		schema             string
		existingSchemaHash string
		writeSchemaErr     error
		patchStatusErr     error
		expectRequeueErr   bool
		expectNext         bool
	}{
		{
			name:       "empty schema skips processing",
			schema:     "",
			expectNext: true,
		},
		{
			name:               "matching hash skips update",
			schema:             "definition user {}",
			existingSchemaHash: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
			expectNext:         true,
		},
		{
			name:             "schema write error requeues",
			schema:           "definition user {}",
			writeSchemaErr:   fmt.Errorf("connection failed"),
			expectRequeueErr: true,
		},
		{
			name:             "patch status error requeues",
			schema:           "definition user {}",
			patchStatusErr:   fmt.Errorf("patch failed"),
			expectRequeueErr: true,
		},
		{
			name:       "successful schema update",
			schema:     "definition user {}",
			expectNext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			requeueErr := false

			handler := &SchemaApplyHandler{
				patchStatus: func(ctx context.Context, patch *v1alpha1.SpiceDBCluster) error {
					return tt.patchStatusErr
				},
				next: handler.NewHandlerFromFunc(func(context.Context) {
					nextCalled = true
				}, "test"),
			}

			ctx := context.Background()
			ctx = CtxConfig.WithValue(ctx, &config.Config{
				SpiceConfig: config.SpiceConfig{
					Schema: tt.schema,
				},
			})
			ctx = CtxCluster.WithValue(ctx, &v1alpha1.SpiceDBCluster{
				Status: v1alpha1.ClusterStatus{
					SchemaHash: tt.existingSchemaHash,
				},
			})
			ctrls := &fake.FakeInterface{}
			ctx = QueueOps.WithValue(ctx, ctrls)

			//queue := newKeyRecordingQueue(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
			//
			//ctx = QueueOps.WithValue(ctx, queue)

			handler.Handle(ctx)

			require.Equal(t, tt.expectNext, nextCalled, "next handler called")
			require.Equal(t, tt.expectRequeueErr, requeueErr, "requeue error")
		})
	}
}
