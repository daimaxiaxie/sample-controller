package main

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/diff"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"sample-controller/pkg/apis/samplecontroller/v1alpha1"
	foofake "sample-controller/pkg/generated/clientset/versioned/fake"
	fooinformers "sample-controller/pkg/generated/informers/externalversions"
)

var (
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	fooclient  *foofake.Clientset
	kubeclient *k8sfake.Clientset

	foosLister        []*v1alpha1.Foo
	deploymentsLister []*appsv1.Deployment

	kubeActions []k8stesting.Action
	fooActions  []k8stesting.Action

	kubeObjects []runtime.Object
	fooObjects  []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{
		t:           t,
		kubeObjects: []runtime.Object{},
		fooObjects:  []runtime.Object{},
	}
	return f
}

func newFoo(name string, replicas *int32) *v1alpha1.Foo {
	return &v1alpha1.Foo{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.FooSpec{
			Replicas:       replicas,
			DeploymentName: fmt.Sprintf("%s-deployment", name),
		},
	}
}

func (f *fixture) newController(ctx context.Context) (*Controller, fooinformers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.fooclient = foofake.NewSimpleClientset(f.fooObjects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeObjects...)

	fooInformerFactory := fooinformers.NewSharedInformerFactory(f.fooclient, noResyncPeriodFunc())
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())

	c := NewController(ctx, f.kubeclient, f.fooclient, kubeInformerFactory.Apps().V1().Deployments(), fooInformerFactory.Samplecontroller().V1alpha1().Foos())
	c.foosSynced = alwaysReady
	c.deploymentsSynced = alwaysReady
	c.recorder = &record.FakeRecorder{}

	for _, f := range f.foosLister {
		_ = fooInformerFactory.Samplecontroller().V1alpha1().Foos().Informer().GetIndexer().Add(f)
	}
	for _, d := range f.deploymentsLister {
		_ = kubeInformerFactory.Apps().V1().Deployments().Informer().GetIndexer().Add(d)
	}

	return c, fooInformerFactory, kubeInformerFactory
}

func (f *fixture) run(ctx context.Context, name string, expectErr bool) {
	f.runController(ctx, name, true, expectErr)
}

func (f *fixture) runController(ctx context.Context, name string, startInformers bool, expectErr bool) {
	c, fooInformerFactory, kubeInformerFactory := f.newController(ctx)
	if startInformers {
		fooInformerFactory.Start(ctx.Done())
		kubeInformerFactory.Start(ctx.Done())
	}

	err := c.syncHandler(ctx, name)
	if !expectErr && err != nil {
		f.t.Errorf("error syncing foo: %v", err)
	} else if expectErr && err == nil {
		f.t.Errorf("expected error syncing foo, got nil")
	}

	fooActions := filterInformerActions(f.fooclient.Actions())
	for i, action := range fooActions {
		if len(f.fooActions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(fooActions)-len(f.fooActions), fooActions[i:])
			break
		}
		expectAction := f.fooActions[i]
		checkAction(expectAction, action, f.t)
	}

	if len(f.fooActions) > len(fooActions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.fooActions)-len(fooActions), f.fooActions[len(fooActions):])
	}

	kubeActions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range kubeActions {
		if len(f.fooActions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(kubeActions)-len(f.kubeActions), kubeActions[i:])
			break
		}
		expectAction := f.kubeActions[i]
		checkAction(expectAction, action, f.t)
	}

	if len(f.kubeActions) > len(kubeActions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.kubeActions)-len(kubeActions), f.kubeActions[len(kubeActions):])
	}
}

func (f *fixture) expectCreateDeploymentAction(deploy *appsv1.Deployment) {
	f.kubeActions = append(f.kubeActions,
		k8stesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments"}, deploy.Namespace, deploy))
}

func (f *fixture) expectUpdateDeploymentAction(deploy *appsv1.Deployment) {
	f.kubeActions = append(f.kubeActions,
		k8stesting.NewUpdateAction(schema.GroupVersionResource{Resource: "deployments"}, deploy.Namespace, deploy))
}

func (f *fixture) expectUpdateFooStatusAction(foo *v1alpha1.Foo) {
	f.fooActions = append(f.fooActions,
		k8stesting.NewUpdateSubresourceAction(schema.GroupVersionResource{Resource: "foos"}, "status", foo.Namespace, foo))
}

func filterInformerActions(actions []k8stesting.Action) []k8stesting.Action {
	var ret []k8stesting.Action
	for _, action := range actions {
		if len(action.GetNamespace()) == 0 && (action.Matches("list", "foos") || action.Matches("watch", "foos") || action.Matches("list", "deployments") || action.Matches("watch", "deployments")) {
			continue
		}
		ret = append(ret, action)
	}
	return ret
}

func checkAction(expected, actual k8stesting.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) && actual.GetSubresource() == expected.GetSubresource()) {
		t.Errorf("Expected\\n\\t%#v\\ngot\\n\\t%#v", expected, actual)
		return
	}

	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("Action has wrong type. Expected: %t. Got: %t", expected, actual)
		return
	}

	switch a := actual.(type) {
	case k8stesting.CreateActionImpl:
		e, _ := expected.(k8stesting.CreateActionImpl)
		expectedObj := e.GetObject()
		obj := a.GetObject()

		if !reflect.DeepEqual(obj, expectedObj) {
			t.Errorf("Action %s %s has wrong object\\nDiff:\\n %s", a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintSideBySide(expectedObj, obj))
		}
	case k8stesting.UpdateActionImpl:
		e, _ := expected.(k8stesting.UpdateActionImpl)
		expectedObj := e.GetObject()
		obj := a.GetObject()

		if !reflect.DeepEqual(obj, expectedObj) {
			t.Errorf("Action %s %s has wrong object\\nDiff:\\n %s", a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintSideBySide(expectedObj, obj))
		}
	case k8stesting.PatchActionImpl:
		e, _ := expected.(k8stesting.PatchActionImpl)
		expectedPatch := e.GetPatch()
		patch := a.GetPatch()

		if !reflect.DeepEqual(expectedPatch, patch) {
			t.Errorf("Action %s %s has wrong object\\nDiff:\\n %s", a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintSideBySide(expectedPatch, patch))
		}
	default:
		t.Errorf("Uncaptured Action %s %s, you should explicitly add a case to capture it", a.GetVerb(), a.GetResource().Resource)
	}
}

func getKey(foo *v1alpha1.Foo, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(foo)
	if err != nil {
		t.Errorf("Unexpected error getting key for foo %v: %v", foo.Name, err)
		return ""
	}
	return key
}

func TestDoNothing(t *testing.T) {
	f := newFixture(t)
	foo := newFoo("test", int32Ptr(1))
	ctx := context.Background()

	deploy := newDeployment(foo)

	f.foosLister = append(f.foosLister, foo)
	f.fooObjects = append(f.fooObjects, foo)
	f.deploymentsLister = append(f.deploymentsLister, deploy)
	f.kubeObjects = append(f.kubeObjects, deploy)

	f.expectUpdateFooStatusAction(foo)

	f.run(ctx, getKey(foo, t), false)

}

func TestCreateDeployment(t *testing.T) {
	f := newFixture(t)
	foo := newFoo("test", int32Ptr(1))
	ctx := context.Background()

	f.foosLister = append(f.foosLister, foo)
	f.fooObjects = append(f.fooObjects, foo)

	deploy := newDeployment(foo)
	f.expectCreateDeploymentAction(deploy)
	f.expectUpdateFooStatusAction(foo)

	f.run(ctx, getKey(foo, t), false)
}

func TestUpdateDeployment(t *testing.T) {
	f := newFixture(t)
	foo := newFoo("test", int32Ptr(1))
	ctx := context.Background()

	oldDeploy := newDeployment(foo)
	foo.Spec.Replicas = int32Ptr(2)
	newDeploy := newDeployment(foo)

	f.foosLister = append(f.foosLister, foo)
	f.fooObjects = append(f.fooObjects, foo)
	f.deploymentsLister = append(f.deploymentsLister, oldDeploy)
	f.kubeObjects = append(f.kubeObjects, oldDeploy)

	f.expectUpdateFooStatusAction(foo)
	f.expectUpdateDeploymentAction(newDeploy)

	f.run(ctx, getKey(foo, t), false)
}

func TestNotControlledByUs(t *testing.T) {
	f := newFixture(t)
	foo := newFoo("test", int32Ptr(1))
	ctx := context.Background()

	deploy := newDeployment(foo)
	deploy.ObjectMeta.OwnerReferences = []metav1.OwnerReference{}

	f.foosLister = append(f.foosLister, foo)
	f.fooObjects = append(f.fooObjects, foo)
	f.deploymentsLister = append(f.deploymentsLister, deploy)
	f.kubeObjects = append(f.kubeObjects, deploy)

	f.run(ctx, getKey(foo, t), true)
}

func int32Ptr(i int32) *int32 {
	return &i
}
