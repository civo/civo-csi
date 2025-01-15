package hook

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPreStop(t *testing.T) {
	type args struct {
		ctx  context.Context
		opts []Option
	}
	type test struct {
		name       string
		args       args
		beforeFunc func(*hook)
		wantErr    bool
	}

	tests := []test{
		{
			name: "Returns nil when node exists but is not drained",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

				node := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-01",
					},
					Spec: v1.NodeSpec{},
				}
				client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, node, nil
				})
			},
		},
		{
			name: "Returns nil when drained node exists but no volume attachments are found",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
			},
		},
		{
			name: "Returns nil when node is not found (possibly being deleted), and no volume attachments are found",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
			},
		},
		{
			name: "Returns error when node cannot be retrieved due to an internal error",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)
				client.Fake.PrependReactor("get", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("error")
				})
			},
			wantErr: true,
		},
		{
			name: "Returns error when volume attachments cannot be listed due to an internal error",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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

			ctx, cancel := context.WithCancel(test.args.ctx)
			defer cancel()

			hook := h.(*hook)
			if test.beforeFunc != nil {
				test.beforeFunc(hook)
			}

			err = h.PreStop(ctx)
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
		name       string
		args       args
		beforeFunc func(*hook)
		wantErr    bool
	}

	tests := []test{
		{
			name: "Returns nil when volume attachment exists for a different node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
			},
		},
		func() test {
			ctx := context.Background()
			volumeattachment := &storagev1.VolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-volume-attachment-01",
				},
				Spec: storagev1.VolumeAttachmentSpec{
					NodeName: "node-01",
				},
				Status: storagev1.VolumeAttachmentStatus{
					Attached: true,
				},
			}
			var volumeAttachmentDeleted int64

			return test{
				name: "Returns nil after volume attachment is cleaned up",
				args: args{
					ctx: ctx,
					opts: []Option{
						WithNodeName("node-01"),
						WithKubernetesClient(fake.NewSimpleClientset()),
					},
				},
				beforeFunc: func(h *hook) {
					client := h.client.(*fake.Clientset)

					client.StorageV1().VolumeAttachments().Create(ctx, volumeattachment, metav1.CreateOptions{})

					client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
						if atomic.LoadInt64(&volumeAttachmentDeleted) == 1 {
							return true, &storagev1.VolumeAttachmentList{}, nil
						}
						return true, &storagev1.VolumeAttachmentList{
							Items: []storagev1.VolumeAttachment{*volumeattachment},
						}, nil
					})

					// To trigger the event after registering it to the informer.
					go func() {
						time.Sleep(time.Second)

						newObj := volumeattachment.DeepCopy()
						newObj.Status.Attached = false
						h.client.StorageV1().VolumeAttachments().Update(ctx, newObj, metav1.UpdateOptions{})
						time.Sleep(100 * time.Millisecond)

						// Since calling delete triggers the event handler, we change the value of volumeAttachmentDeleted before that. This allows us to dynamically modify the value.
						atomic.StoreInt64(&volumeAttachmentDeleted, 1)
						h.client.StorageV1().VolumeAttachments().Delete(ctx, newObj.GetName(), metav1.DeleteOptions{})
						time.Sleep(100 * time.Millisecond)
					}()
				},
			}
		}(),

		func() test {
			ctx := context.Background()
			volumeattachments := &storagev1.VolumeAttachmentList{
				Items: []storagev1.VolumeAttachment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-volume-attachment-01",
						},
						Spec: storagev1.VolumeAttachmentSpec{
							NodeName: "node-01",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-volume-attachment-02",
						},
						Spec: storagev1.VolumeAttachmentSpec{
							NodeName: "node-01",
						},
					},
				},
			}
			var volumeAttachmentDeleted int64

			return test{
				name: "Returns nil after successful deletion of multiple volume attachments",
				args: args{
					ctx: ctx,
					opts: []Option{
						WithNodeName("node-01"),
						WithKubernetesClient(fake.NewSimpleClientset()),
					},
				},
				beforeFunc: func(h *hook) {
					client := h.client.(*fake.Clientset)
					for _, item := range volumeattachments.Items {
						client.StorageV1().VolumeAttachments().Create(ctx, &item, metav1.CreateOptions{})
					}

					client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
						if atomic.LoadInt64(&volumeAttachmentDeleted) == 1 {
							return true, &storagev1.VolumeAttachmentList{}, nil
						}
						return true, volumeattachments, nil
					})

					// To trigger the event after registering it to the informer.
					go func() {
						time.Sleep(time.Second)

						client.StorageV1().VolumeAttachments().Delete(ctx, "test-volume-attachment-01", metav1.DeleteOptions{})
						time.Sleep(100 * time.Millisecond)

						// Since calling delete triggers the event handler, we change the value of volumeAttachmentDeleted before that. This allows us to dynamically modify the value.
						atomic.StoreInt64(&volumeAttachmentDeleted, 1)
						client.StorageV1().VolumeAttachments().Delete(ctx, "test-volume-attachment-02", metav1.DeleteOptions{})
						time.Sleep(100 * time.Millisecond)
					}()
				},
			}
		}(),
		{
			name: "Returns error when listing volume attachments fails in checkVolumeAttachmentsExist method",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)
				client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("error")
				})
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

			ctx, cancel := context.WithCancel(test.args.ctx)
			defer cancel()

			hook := h.(*hook)
			if test.beforeFunc != nil {
				test.beforeFunc(hook)
			}

			err = hook.waitForVolumeAttachmentsCleanup(ctx)
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
		ctx         context.Context
		obj         interface{}
		stopEventFn func()
		opts        []Option
	}
	type test struct {
		name       string
		args       args
		beforeFunc func(*hook)
		wantErr    bool
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
				stopEventFn: func() {},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
				stopEventFn: func() {},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
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
				stopEventFn: func() {},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
			},
			wantErr: true,
		},
		{
			name: "Returns error when invalid object is provided",
			args: args{
				ctx:         context.Background(),
				obj:         "invalid-object",
				stopEventFn: func() {},
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
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

			ctx, cancel := context.WithCancel(test.args.ctx)
			defer cancel()

			hook := h.(*hook)
			if test.beforeFunc != nil {
				test.beforeFunc(hook)
			}

			err = hook.volumeAttachmentEventHandler(ctx, test.args.obj, test.args.stopEventFn)
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
		name       string
		args       args
		beforeFunc func(*hook)
		want       bool
		wantErr    bool
	}

	tests := []test{
		{
			name: "Returns false when no volume attachments exist for the specified node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
		},
		{
			name: "Returns false when volume attachment exists for a different node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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
			},
		},
		{
			name: "Returns false and error when listing volume attachments fails",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

				client.Fake.PrependReactor("list", "volumeattachments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("error")
				})
			},
			wantErr: true,
		},
		{
			name: "Returns true and error when volume attachment exists for the specified node",
			args: args{
				ctx: context.Background(),
				opts: []Option{
					WithNodeName("node-01"),
					WithKubernetesClient(fake.NewSimpleClientset()),
				},
			},
			beforeFunc: func(h *hook) {
				client := h.client.(*fake.Clientset)

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

			ctx, cancel := context.WithCancel(test.args.ctx)
			defer cancel()

			hook := h.(*hook)
			if test.beforeFunc != nil {
				test.beforeFunc(hook)
			}

			got, err := hook.checkVolumeAttachmentsExist(ctx)
			if test.wantErr {
				assert.NotNil(tt, err)
			} else {
				assert.Nil(tt, err)
			}
			assert.Equal(tt, test.want, got)
		})
	}
}
