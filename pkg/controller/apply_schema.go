package controller

import (
	"context"
	"fmt"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/controller-idioms/handler"
	"github.com/authzed/controller-idioms/hash"
	"github.com/authzed/spicedb-operator/pkg/apis/authzed/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SchemaApplyHandler struct {
	patchStatus func(ctx context.Context, patch *v1alpha1.SpiceDBCluster) error
	next        handler.ContextHandler
}

func (s *SchemaApplyHandler) Handle(ctx context.Context) {
	fmt.Println("SchemaApplyHandler.Handle")
	cfg := CtxConfig.Value(ctx)
	fmt.Println("cfg.SpiceConfig.Schema:", cfg.SpiceConfig.Schema)
	if cfg.SpiceConfig.Schema == "" {
		fmt.Println("Schema is empty, returning")
		s.next.Handle(ctx)
		return
	}

	//if !s.shouldUpdateSchema(ctx) {
	//	s.next.Handle(ctx)
	//	return
	//}
	//
	cluster := CtxCluster.MustValue(ctx)

	client, err := s.createGRPCClient(ctx)
	if err != nil {
		fmt.Println("failed to create gRPC client: %w", err)
		s.next.Handle(ctx)
		return
	}
	defer client.Close()

	schemaClient := v1.NewSchemaServiceClient(client)
	_, err = schemaClient.WriteSchema(ctx, &v1.WriteSchemaRequest{
		Schema: cfg.SpiceConfig.Schema,
	})

	if err != nil {
		fmt.Println("WriteSchemaRequest error is not nil", err)
		st, ok := status.FromError(err)
		if !ok {
			fmt.Println("failed to write schema: %w", err)
			s.next.Handle(ctx)
			return
		}
		fmt.Println("Code is: ", st.Code())
		if st.Code() == codes.FailedPrecondition || st.Code() == codes.InvalidArgument {
			fmt.Println("Code is FailedPrecodition ya know")
			s.next.Handle(ctx)
			return
		} else {
			QueueOps.RequeueErr(ctx, err)
			s.next.Handle(ctx)
			return
		}

		//switch st.Code() {
		//// Non-transient, bad schema formation codes
		//case codes.FailedPrecondition, codes.InvalidArgument:
		//	fmt.Println("schema validation failed: %w", err)
		//	return
		//default:
		//	fmt.Println("requeueing err")
		//	QueueOps.RequeueErr(ctx, err)
		//	return
		//}
	}

	fmt.Println("Made it this far")
	err = s.updateSchemaHash(ctx, cluster)
	if err != nil {
		fmt.Println("failed to update schema hash: %w", err)
	}

	s.next.Handle(ctx)
}

func (s *SchemaApplyHandler) shouldUpdateSchema(ctx context.Context) bool {
	cluster := CtxCluster.MustValue(ctx)
	cfg := CtxConfig.Value(ctx)

	hasher := hash.NewObjectHash()
	schemaHash := hasher.Hash(cfg.SpiceConfig.Schema)

	return cluster.Status.SchemaHash != schemaHash
}

//
//func (s *SchemaApplyHandler) applySchema(ctx context.Context) error {
//	cluster := CtxCluster.MustValue(ctx)
//
//	client, err := s.createGRPCClient(ctx)
//	if err != nil {
//		return fmt.Errorf("failed to create gRPC client: %w", err)
//	}
//	defer client.Close()
//
//	cfg := CtxConfig.Value(ctx)
//	schemaClient := v1.NewSchemaServiceClient(client)
//	_, err = schemaClient.WriteSchema(ctx, &v1.WriteSchemaRequest{
//		Schema: cfg.SpiceConfig.Schema,
//	})
//
//	if err != nil {
//		st, ok := status.FromError(err)
//		if !ok {
//			return fmt.Errorf("failed to write schema: %w", err)
//		}
//
//		switch st.Code() {
//		case codes.FailedPrecondition, codes.InvalidArgument:
//			return NewPermanentError(fmt.Errorf("schema validation failed: %w", err))
//		default:
//			return fmt.Errorf("failed to write schema: %w", err)
//		}
//	}
//
//	return s.updateSchemaHash(ctx, cluster)
//}

func (s *SchemaApplyHandler) createGRPCClient(ctx context.Context) (*grpc.ClientConn, error) {
	cfg := CtxConfig.Value(ctx)
	svc := cfg.Service()
	endpoint := fmt.Sprintf("%s.%s.svc:50051", *svc.Name, *svc.Namespace)

	return grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&presharedKeyCredentials{key: cfg.SpiceConfig.PresharedKey}),
	)
}

//func (s *SchemaApplyHandler) writeSchema(ctx context.Context, client *grpc.ClientConn) error {
//	cfg := CtxConfig.Value(ctx)
//	schemaClient := v1.NewSchemaServiceClient(client)
//
//	_, err := schemaClient.WriteSchema(ctx, &v1.WriteSchemaRequest{
//		Schema: cfg.SpiceConfig.Schema,
//	})
//	if err != nil {
//		st, ok := status.FromError(err)
//		if !ok {
//			return fmt.Errorf("failed to write schema: %w", err)
//		}
//
//		switch st.Code() {
//		case codes.FailedPrecondition, codes.InvalidArgument:
//			return NewPermanentError(fmt.Errorf("schema validation failed: %w", err))
//		default:
//			return fmt.Errorf("failed to write schema: %w", err)
//		}
//	}
//	return nil
//}

func (s *SchemaApplyHandler) updateSchemaHash(ctx context.Context, cluster *v1alpha1.SpiceDBCluster) error {
	cfg := CtxConfig.Value(ctx)
	hasher := hash.NewObjectHash()
	schemaHash := hasher.Hash(cfg.SpiceConfig.Schema)

	status := &v1alpha1.SpiceDBCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.SpiceDBClusterKind,
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Status: *cluster.Status.DeepCopy(),
	}
	status.Status.SchemaHash = schemaHash

	if err := s.patchStatus(ctx, status); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	return nil
}
