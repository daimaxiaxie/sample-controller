package main

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	informers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"sample-controller/pkg/apis/samplecontroller/v1alpha1"
	clientset "sample-controller/pkg/generated/clientset/versioned"
	fooscheme "sample-controller/pkg/generated/clientset/versioned/scheme"
	fooinformers "sample-controller/pkg/generated/informers/externalversions/samplecontroller/v1alpha1"
	foolisters "sample-controller/pkg/generated/listers/samplecontroller/v1alpha1"
)

const (
	controllerAgentName    = "sample-controller"
	SuccessSynced          = "Synced"
	ErrResourceExists      = "ErrResourcesExists"
	MessageResourceExists  = "Resource %q already exists and is not managed by Foo"
	MessageResourcesSynced = "Foo synced successfully"
)

var logger = zap.NewExample().Sugar()

type Controller struct {
	kubeClient kubernetes.Interface
	fooClient  clientset.Interface

	deploymentsLister listers.DeploymentLister
	deploymentsSynced cache.InformerSynced
	foosLister        foolisters.FooLister
	foosSynced        cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
	recorder  record.EventRecorder
}

func NewController(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	fooClient clientset.Interface,
	deploymentInformer informers.DeploymentInformer,
	fooInformer fooinformers.FooInformer) *Controller {
	utilruntime.Must(fooscheme.AddToScheme(scheme.Scheme))

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
		Component: controllerAgentName,
	})

	controller := &Controller{
		kubeClient:        kubeClient,
		fooClient:         fooClient,
		deploymentsLister: deploymentInformer.Lister(),
		deploymentsSynced: deploymentInformer.Informer().HasSynced,
		foosLister:        fooInformer.Lister(),
		foosSynced:        fooInformer.Informer().HasSynced,
		workqueue: workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{Name: "Foos"}),
		recorder: recorder,
	}

	_, _ = fooInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueue(newObj)
		},
		DeleteFunc: controller.enqueue,
	})

	_, _ = deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(oldObj, newObj interface{}) {
			newDeploy := newObj.(*appsv1.Deployment)
			oldDeploy := oldObj.(*appsv1.Deployment)
			if oldDeploy.ResourceVersion == newDeploy.ResourceVersion {
				return
			}
			controller.handleObject(newDeploy)
		},
		DeleteFunc: controller.handleObject,
	})

	return controller
}

func (c *Controller) run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShuttingDown()

	logger.Info("Starting Foo controller")
	logger.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(ctx.Done(), c.deploymentsSynced, c.foosSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	logger.Infof("Starting %d workers", workers)
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.worker, time.Second)
	}

	logger.Info("Started workers")
	<-ctx.Done()
	logger.Info("Shutting down workers")

	return nil
}

func (c *Controller) worker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {

	}
}

func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.syncHandler(ctx, key); err != nil {
			c.workqueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err)
		}
		c.workqueue.Forget(obj)
		logger.Infof("Successfully synced name %s", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", err))
		return nil
	}

	foo, err := c.foosLister.Foos(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("foo '%s' in work queue no longer exists", err))
			//when delete. deployment auto delete because ownerReferences(Cascading deletion)
			return nil
		}
		return err
	}

	deploymentName := foo.Spec.DeploymentName
	if deploymentName == "" {
		utilruntime.HandleError(fmt.Errorf("%s: deployment name must be specified", key))
		return nil
	}

	deployment, err := c.deploymentsLister.Deployments(foo.Namespace).Get(deploymentName)
	if errors.IsNotFound(err) {
		deployment, err = c.kubeClient.AppsV1().Deployments(foo.Namespace).Create(context.TODO(), newDeployment(foo), metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}

	if !metav1.IsControlledBy(deployment, foo) {
		msg := fmt.Sprintf(MessageResourceExists, deployment.Name)
		c.recorder.Event(foo, corev1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	if foo.Spec.Replicas != nil && *foo.Spec.Replicas != *deployment.Spec.Replicas {
		logger.Infof("Update deployment resource, currentReplicas %d  desiredReplicas %d", *foo.Spec.Replicas, *deployment.Spec.Replicas)
		deployment, err = c.kubeClient.AppsV1().Deployments(foo.Namespace).Update(context.TODO(), newDeployment(foo), metav1.UpdateOptions{})
	}

	if err != nil {
		return err
	}

	err = c.update(foo, deployment)
	if err != nil {
		return err
	}

	c.recorder.Event(foo, corev1.EventTypeNormal, SuccessSynced, MessageResourcesSynced)
	return nil
}

func (c *Controller) update(foo *v1alpha1.Foo, deployment *appsv1.Deployment) error {
	fooCp := foo.DeepCopy()
	fooCp.Status.AvailableReplicas = deployment.Status.AvailableReplicas
	_, err := c.fooClient.SamplecontrollerV1alpha1().Foos(foo.Namespace).UpdateStatus(context.TODO(), fooCp, metav1.UpdateOptions{})
	return err
}

func (c *Controller) enqueue(obj interface{}) {
	if key, err := cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	} else {
		c.workqueue.Add(key)
	}
}

func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		logger.Warnf("Recovered deleted object, resourceName %s", object.GetName())
	}

	logger.Infof("Processing object, object %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		if ownerRef.Kind != "Foo" {
			return
		}

		foo, err := c.foosLister.Foos(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			logger.Infof("Ignore orphaned object, object %s owner %s", object.GetName(), ownerRef.Name)
			return
		}

		c.enqueue(foo)
		return
	}
}

func newDeployment(foo *v1alpha1.Foo) *appsv1.Deployment {
	labels := map[string]string{
		"app":        "nginx",
		"controller": foo.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foo.Spec.DeploymentName,
			Namespace: foo.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(foo, v1alpha1.SchemeGroupVersion.WithKind("Foo")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: foo.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}
}
