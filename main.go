package main

import (
	"flag"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clientset "sample-controller/pkg/generated/clientset/versioned"
	fooinformers "sample-controller/pkg/generated/informers/externalversions"
	"sample-controller/pkg/signals"
)

var kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
var masterURL = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")

func main() {
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		return
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return
	}

	fooClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	fooInformerFactory := fooinformers.NewSharedInformerFactory(fooClient, time.Second*30)
	ctx := signals.SetupSignalHandler()
	controller := NewController(ctx, kubeClient, fooClient, kubeInformerFactory.Apps().V1().Deployments(), fooInformerFactory.Samplecontroller().V1alpha1().Foos())

	kubeInformerFactory.Start(ctx.Done())
	fooInformerFactory.Start(ctx.Done())

	if err := controller.run(ctx, 1); err != nil {
		logger.Infof("Error running controller %s", err)
	}
}
