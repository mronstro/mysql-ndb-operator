// Copyright (c) 2020, Oracle and/or its affiliates.
//
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/

package resources

import (
	"fmt"
	"os"
	"strings"

	"github.com/ocklin/ndb-operator/pkg/apis/ndbcontroller/v1alpha1"
	"github.com/ocklin/ndb-operator/pkg/constants"
	"github.com/ocklin/ndb-operator/pkg/version"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
)

const mgmdVolumeName = "mgmdvolume"
const mgmdName = "mgmd"
const mgmdImage = "mysql-cluster"

const ndbdName = "ndbd"

const ndbAgentName = "ndb-agent"
const ndbAgentImage = "ndb-agent"
const ndbAgentVersion = "1.0.0"

const ndbVersion = "1.0.0"

type StatefulSetInterface interface {
	NewStatefulSet(cluster *v1alpha1.Ndb) *apps.StatefulSet
	GetName() string
}

type baseStatefulSet struct {
	typeName    string
	clusterName string
}

func NewMgmdStatefulSet(cluster *v1alpha1.Ndb) *baseStatefulSet {
	return &baseStatefulSet{typeName: "mgmd", clusterName: cluster.Name}
}

func NewNdbdStatefulSet(cluster *v1alpha1.Ndb) *baseStatefulSet {
	return &baseStatefulSet{typeName: "ndbd", clusterName: cluster.Name}
}

func volumeMounts(cluster *v1alpha1.Ndb) []v1.VolumeMount {
	var mounts []v1.VolumeMount

	mounts = append(mounts, v1.VolumeMount{
		Name:      mgmdVolumeName,
		MountPath: constants.DataDir,
	})

	// A user may explicitly define a config file for ndb via config map
	if cluster.Spec.Config != nil {
		mounts = append(mounts, v1.VolumeMount{
			Name:      "config-volume",
			MountPath: constants.DataDir + "/config",
		})
	}

	return mounts
}

func agentContainer(ndb *v1alpha1.Ndb, ndbAgentImage string) v1.Container {

	agentVersion := version.GetBuildVersion()

	if version := os.Getenv("NDB_AGENT_VERSION"); version != "" {
		agentVersion = version
	}

	image := fmt.Sprintf("%s:%s", ndbAgentImage, agentVersion)
	klog.Infof("Creating agent container from image %s", image)

	return v1.Container{
		Name:  ndbAgentName,
		Image: image,
		Ports: []v1.ContainerPort{
			{
				ContainerPort: 8080,
			},
		},
		// agent requires access to ndbd and mgmd volumes
		VolumeMounts: volumeMounts(ndb),
		Env:          []v1.EnvVar{},
		LivenessProbe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/live",
					Port: intstr.FromInt(8080),
				},
			},
		},
		ReadinessProbe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(8080),
				},
			},
		},
	}
}

func (bss *baseStatefulSet) getMgmdHostname(ndb *v1alpha1.Ndb) string {
	dnsZone := fmt.Sprintf("%s.svc.cluster.local", ndb.Namespace)

	mgmHostnames := ""
	for i := 0; i < (int)(*ndb.Spec.Mgmd.NodeCount); i++ {
		if i > 0 {
			mgmHostnames += ","
		}
		mgmHostnames += fmt.Sprintf("%s-%d.%s.%s", bss.clusterName+"-mgmd", i, bss.clusterName, dnsZone)
	}

	return mgmHostnames
}

/*
	Creates comma seperated list of all FQ hostnames of data nodes
*/
func (bss *baseStatefulSet) getNdbdHostnames(ndb *v1alpha1.Ndb) string {

	dnsZone := fmt.Sprintf("%s.svc.cluster.local", ndb.Namespace)

	ndbHostnames := ""
	for i := 0; i < (int)(*ndb.Spec.Ndbd.NodeCount); i++ {
		if i > 0 {
			ndbHostnames += ","
		}
		ndbHostnames += fmt.Sprintf("%s-%d.%s.%s", bss.clusterName+"-ndbd", i, bss.clusterName, dnsZone)
	}
	return ndbHostnames
}

// Builds the Ndb operator container for a mgmd.
func (bss *baseStatefulSet) mgmdContainer(ndb *v1alpha1.Ndb) v1.Container {

	runWithEntrypoint := false
	cmd := ""
	environment := []v1.EnvVar{}

	imageName := fmt.Sprintf("%s:%s", mgmdImage, ndbVersion)

	if runWithEntrypoint {
		args := []string{
			"ndb_mgmd",
		}
		cmdArgs := strings.Join(args, " ")
		cmd = fmt.Sprintf(`/entrypoint.sh %s`, cmdArgs)

		mgmdHostname := bss.getMgmdHostname(ndb)
		ndbdHostnames := bss.getNdbdHostnames(ndb)

		environment = []v1.EnvVar{
			{
				Name:  "NDB_REPLICAS",
				Value: fmt.Sprintf("%d", *ndb.Spec.Ndbd.NoOfReplicas),
			},
			{
				Name:  "NDB_MGMD_HOSTS",
				Value: mgmdHostname,
			},
			{
				Name:  "NDB_NDBD_HOSTS",
				Value: ndbdHostnames,
			},
		}
		klog.Infof("Creating mgmd container from image %s with hostnames mgmd: %s, ndbd: %s",
			imageName, mgmdHostname, ndbdHostnames)

	} else {
		args := []string{
			"-f", "/var/lib/ndb/config/config.ini",
			"--configdir=/var/lib/ndb",
			"--initial",
			"--nodaemon",
			"-v",
		}
		cmdArgs := strings.Join(args, " ")
		cmd = fmt.Sprintf(`/usr/sbin/ndb_mgmd %s`, cmdArgs)
		klog.Infof("Creating mgmd container from image %s", imageName)
	}

	return v1.Container{
		Name:  mgmdName,
		Image: imageName,
		Ports: []v1.ContainerPort{
			{
				ContainerPort: 1186,
			},
		},
		VolumeMounts:    volumeMounts(ndb),
		Command:         []string{"/bin/bash", "-ecx", cmd},
		ImagePullPolicy: v1.PullNever,
		Env:             environment,
	}
}

// Builds the Ndb operator container for a mgmd.
func (bss *baseStatefulSet) ndbmtdContainer(ndb *v1alpha1.Ndb) v1.Container {

	args := []string{
		"ndbmtd",
	}

	entryPointArgs := strings.Join(args, " ")
	cmd := fmt.Sprintf(`/entrypoint.sh %s`, entryPointArgs)

	mgmdHostname := bss.getMgmdHostname(ndb)
	imageName := fmt.Sprintf("%s:%s", mgmdImage, ndbVersion)
	klog.Infof("Creating ndbmtd container from image %s for hostnames %s",
		imageName, mgmdHostname)

	return v1.Container{
		Name:  ndbdName,
		Image: imageName,
		Ports: []v1.ContainerPort{
			{
				ContainerPort: 1186,
			},
		},
		VolumeMounts:    volumeMounts(ndb),
		Command:         []string{"/bin/bash", "-ecx", cmd},
		ImagePullPolicy: v1.PullNever,
		Env: []v1.EnvVar{
			{
				Name:  "NDB_REPLICAS",
				Value: fmt.Sprintf("%d", *ndb.Spec.Ndbd.NoOfReplicas),
			},
			{
				Name:  "NDB_MGMD_HOSTS",
				Value: mgmdHostname,
			},
		},
	}
}

func (bss *baseStatefulSet) GetName() string {
	return bss.clusterName + "-" + bss.typeName
}

// NewForCluster creates a new StatefulSet for the given Cluster.
func (bss *baseStatefulSet) NewStatefulSet(ndb *v1alpha1.Ndb) *apps.StatefulSet {

	// If a PV isn't specified just use a EmptyDir volume
	var podVolumes = []v1.Volume{}
	podVolumes = append(podVolumes,
		v1.Volume{
			Name: mgmdVolumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{
					Medium: "",
				},
			},
		},
	)
	// add the configmap generated with config.ini
	podVolumes = append(podVolumes, v1.Volume{
		Name: "config-volume",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: ndb.GetConfigMapName(),
				},
			},
		},
	})
	//}

	containers := []v1.Container{}
	serviceaccount := ""
	var podLabels map[string]string
	replicas := func(i int32) *int32 { return &i }((0))

	if bss.typeName == "mgmd" {
		containers = []v1.Container{
			//bss.mgmdContainer(cluster),
			agentContainer(ndb, ndbAgentImage),
		}
		serviceaccount = "ndb-agent"
		replicas = ndb.Spec.Mgmd.NodeCount
		podLabels = ndb.GetManagementNodeLabels()

	} else {
		containers = []v1.Container{
			//bss.ndbmtdContainer(cluster),
			agentContainer(ndb, ndbAgentImage),
		}
		serviceaccount = "ndb-agent"
		replicas = ndb.Spec.Ndbd.NodeCount
		podLabels = ndb.GetDataNodeLabels()
	}

	podspec := v1.PodSpec{
		Containers: containers,
		Volumes:    podVolumes,
	}
	if serviceaccount != "" {
		podspec.ServiceAccountName = "ndb-agent"
	}

	ss := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   bss.GetName(),
			Labels: podLabels, // must match templates
			// could have a owner reference here
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ndb, schema.GroupVersionKind{
					Group:   v1.SchemeGroupVersion.Group,
					Version: v1.SchemeGroupVersion.Version,
					Kind:    "Ndb",
				}),
			},
		},
		Spec: apps.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Replicas: replicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        bss.GetName(),
					Labels:      podLabels,
					Annotations: map[string]string{},
				},
				Spec: podspec,
			},
			ServiceName: ndb.GetServiceName(),
		},
	}
	return ss
}
