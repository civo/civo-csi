package hook

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPreStop(t *testing.T) {
	type args struct {
		ctx  context.Context
		opts []Option
	}
	type test struct {
		name    string
		args    args
		wantErr bool
	}

	tests := []test{
		{
			name: "Returns nil when node exists but is not drained",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						node := &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "node-01",
							},
							Spec: v1.NodeSpec{},
						}
						client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, node, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns nil when drained node exists but no volume attachments are found",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						node := &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "node-01",
							},
							Spec: v1.NodeSpec{
								Taints: []v1.Taint{
									{
										Key: v1.TaintNodeUnschedulable,
									},
								},
							},
						}
						client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, node, nil
						})

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns nil when node is not found (possibly being deleted), and no volume attachments are found",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, nil, k8serrors.NewNotFound(schema.GroupResource{
								Group:    "",
								Resource: "nodes",
							}, "node-01")
						})

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns error when node cannot be retrieved due to an internal error",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, nil, errors.New("error")
						})
						return client
					}()),
				},
			},
			wantErr: true,
		},
		{
			name: "Returns error when volume attachments cannot be listed due to an internal error",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						node := &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "node-01",
							},
							Spec: v1.NodeSpec{
								Taints: []v1.Taint{
									{
										Key: v1.TaintNodeUnschedulable,
									},
								},
							},
						}
						client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, node, nil
						})
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, nil, errors.New("error")
						})
						return client
					}()),
				},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h, err := NewHook(test.args.opts...)
			if err != nil {
				tt.Fatal(err)
			}

			err = h.PreStop(test.args.ctx)
			if test.wantErr {
				assert.NotNil(tt, err)
			} else {
				assert.Nil(tt, err)
			}
		})
	}
}

func TestIsNodeDrained(t *testing.T) {
	type test struct {
		name string
		node *v1.Node
		want bool
	}

	tests := []test{
		{
			name: "Returns true when node is drained due to TaintNodeUnreachable",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					Taints: []v1.Taint{
						{
							Key: v1.TaintNodeUnschedulable,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Returns false when node is not drained because of TaintNodeNotReady",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					Taints: []v1.Taint{
						{
							Key: v1.TaintNodeNotReady,
						},
					},
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			got := isNodeDrained(test.node)

			assert.Equal(tt, test.want, got)
		})
	}
}

func TestWaitForVolumeAttachmentsCleanup(t *testing.T) {
	type args struct {
		ctx  context.Context
		opts []Option
	}
	type test struct {
		name    string
		args    args
		wantErr bool
	}

	tests := []test{
		{
			name: "Returns nil when volume attachment exists for a different node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-02",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns nil when the context is canceled",
			args: args{
				ctx: func() context.Context {
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					return ctx
				}(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-01",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns error when listing volume attachments fails in checkVolumeAttachmentsExist method",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, nil, errors.New("error")
						})
						return client
					}()),
				},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h, err := NewHook(test.args.opts...)
			if err != nil {
				tt.Fatal(err)
			}

			err = h.(*hook).waitForVolumeAttachmentsCleanup(test.args.ctx)
			if test.wantErr {
				assert.NotNil(tt, err)
			} else {
				assert.Nil(tt, err)
			}
		})
	}
}

func TestVolumeAttachmentEventHandler(t *testing.T) {
	type args struct {
		ctx  context.Context
		obj  interface{}
		opts []Option
	}
	type test struct {
		name    string
		args    args
		wantErr bool
	}

	tests := []test{
		{
			name: "Returns nil when volume attachment exists for a different node",
			args: args{
				ctx: context.Background(),
				obj: &storagev1.VolumeAttachment{
					Spec: storagev1.VolumeAttachmentSpec{
						NodeName: "node-02",
					},
				},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-02",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns nil when input object is VolumeAttachment but resource is not found (already deleted)",
			args: args{
				ctx: context.Background(),
				obj: &storagev1.VolumeAttachment{
					Spec: storagev1.VolumeAttachmentSpec{
						NodeName: "node-01",
					},
				},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						return client
					}()),
				},
			},
		},
		{
			name: "Returns error when volume attachment exists for the specified node",
			args: args{
				ctx: context.Background(),
				obj: &storagev1.VolumeAttachment{
					Spec: storagev1.VolumeAttachmentSpec{
						NodeName: "node-01",
					},
				},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-01",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
			wantErr: true,
		},
		{
			name: "Returns error when invalid object is provided",
			args: args{
				ctx: context.Background(),
				obj: "invalid-object",
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						return client
					}()),
				},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h, err := NewHook(test.args.opts...)
			if err != nil {
				tt.Fatal(err)
			}

			err = h.(*hook).volumeAttachmentEventHandler(test.args.ctx, test.args.obj)
			if test.wantErr {
				assert.NotNil(tt, err)
			} else {
				assert.Nil(tt, err)
			}
		})
	}
}

func TestCheckVolumeAttachmentsExist(t *testing.T) {
	type args struct {
		ctx  context.Context
		opts []Option
	}
	type test struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}

	tests := []test{
		{
			name: "Returns false when no volume attachments exist for the specified node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						return client
					}()),
				},
			},
		},
		{
			name: "Returns false when volume attachment exists for a different node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-02",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
		},
		{
			name: "Returns false and error when listing volume attachments fails",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, nil, errors.New("error")
						})
						return client
					}()),
				},
			},
			wantErr: true,
		},
		{
			name: "Returns true and error when volume attachment exists for the specified node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(func() kubernetes.Interface {
						client := fake.NewSimpleClientset()

						list := &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume-attachment-01",
									},
									Spec: storagev1.VolumeAttachmentSpec{
										NodeName: "node-01",
									},
								},
							},
						}
						client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							return true, list, nil
						})
						return client
					}()),
				},
			},
			want:    true,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			h, err := NewHook(test.args.opts...)
			if err != nil {
				tt.Fatal(err)
			}

			got, err := h.(*hook).checkVolumeAttachmentsExist(test.args.ctx)
			if test.wantErr {
				assert.NotNil(tt, err)
			} else {
				assert.Nil(tt, err)
			}
			assert.Equal(tt, test.want, got)
		})
	}
}
