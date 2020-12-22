// Copyright (c) 2020, Oracle and/or its affiliates.
//
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/

package main

import (
	"flag"
	"os"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/mysql/ndb-operator/pkg/config"
	"github.com/mysql/ndb-operator/pkg/controllers"
	clientset "github.com/mysql/ndb-operator/pkg/generated/clientset/versioned"
	informers "github.com/mysql/ndb-operator/pkg/generated/informers/externalversions"
	"github.com/mysql/ndb-operator/pkg/signals"
	"github.com/mysql/ndb-operator/pkg/version"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {

	klog.InitFlags(nil)
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	klog.Infof("Starting ndb-operator build version %s build time %s",
		version.GetBuildVersion(),
		version.GetBuildTime())

	klog.Info("Checking environment")
	var k8Host, k8Port string
	if k8Host = os.Getenv("KUBERNETES_SERVICE_HOST"); k8Host != "" && len(k8Host) > 0 {
		klog.Infof("Kubernetes host: %s", k8Host)
	}
	if k8Port = os.Getenv("KUBERNETES_SERVICE_PORT"); k8Port != "" && len(k8Port) > 0 {
		klog.Infof("Kubernetes port: %s", k8Port)
	}

	runInCluster := true
	if k8Host == "" || k8Port == "" {
		if kubeconfig == "" {
			klog.Fatalf("No kubeconfig or environment given. Exit.")
			return
		}
		runInCluster = false
		klog.Infof("Running ndb-operator out of kubernetes cluster")
	} else {
		klog.Infof("Running ndb-operator inside kubernetes cluster")
	}

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	ndbClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building ndb clientset: %s", err.Error())
	}

	k8If := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	ndbOpIf := informers.NewSharedInformerFactory(ndbClient, time.Second*30)

	ctx := controllers.NewControllerContext(kubeClient, ndbClient, runInCluster)

	controller := controllers.NewController(
		ctx,
		k8If.Apps().V1().StatefulSets(),
		k8If.Apps().V1().Deployments(),
		k8If.Core().V1().Services(),
		k8If.Core().V1().Pods(),
		k8If.Core().V1().ConfigMaps(),
		ndbOpIf.Mysql().V1alpha1().Ndbs())

	// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	k8If.Start(stopCh)
	ndbOpIf.Start(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&config.ScriptsDir, "scripts_dir", config.DefaultScriptsDir,
		"The location of scripts to be deployed by the operator in the pods. Only required if out-of-cluster.")
}
